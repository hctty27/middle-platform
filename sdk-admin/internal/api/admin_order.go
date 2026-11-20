package api

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"hitall.com/middle-platform/shared/logging"
)

const adminOrderColumns = "id,game_id,appid,env,game_order_id,COALESCE(platform_order_no,''),amount,payment_status,notify_status,notify_attempts,created_at,paid_at"

type adminOrderRow struct {
	ID string `json:"id"`
	GameID int64 `json:"gameId"`
	AppID string `json:"appid"`
	Env string `json:"env"`
	GameOrderID string `json:"gameOrderId"`
	PlatformOrderNo string `json:"platformOrderNo"`
	Amount int64 `json:"amount"`
	PaymentStatus int `json:"paymentStatus"`
	NotifyStatus int `json:"notifyStatus"`
	NotifyAttempts int `json:"notifyAttempts"`
	CreatedAt time.Time `json:"createdAt"`
	PaidAt *time.Time `json:"paidAt"`
}

type adminOrderDetail struct {
	adminOrderRow
	Attach string `json:"attach"`
	LastNotifyAt *time.Time `json:"lastNotifyAt"`
	NotifySuccessAt *time.Time `json:"notifySuccessAt"`
	NextNotifyAt *time.Time `json:"nextNotifyAt"`
	LastNotifyError string `json:"lastNotifyError"`
}

func (o *adminOrderRow) destinations() []any {
	return []any{&o.ID,&o.GameID,&o.AppID,&o.Env,&o.GameOrderID,&o.PlatformOrderNo,&o.Amount,&o.PaymentStatus,&o.NotifyStatus,&o.NotifyAttempts,&o.CreatedAt,&o.PaidAt}
}

type adminOrderStats struct {
	Registered int64 `json:"registered"`
	Paid int64 `json:"paid"`
	PaidAmount string `json:"paidAmount"`
	Pending int64 `json:"pending"`
	Failed int64 `json:"failed"`
	PaidNotified int64 `json:"paidNotified"`
}
type orderStatsEntry struct {
	value adminOrderStats
	expires time.Time
}
type adminOrders struct {
	db *sql.DB
	gate chan struct{}
	mu sync.Mutex
	stats map[string]orderStatsEntry
}

func registerAdminOrderRoutes(mux *http.ServeMux, db *sql.DB) {
	a := &adminOrders{db:db,gate:make(chan struct{},2),stats:make(map[string]orderStatsEntry)}
	mux.HandleFunc("GET /api/sdk/admin/orders",a.limit(a.list))
	mux.HandleFunc("GET /api/sdk/admin/orders/stats",a.limit(a.statistics))
	mux.HandleFunc("GET /api/sdk/admin/orders/{id}",a.limit(a.detail))
	mux.HandleFunc("POST /api/sdk/admin/orders/{id}/retry",a.limit(a.retry))
}

func orderAdminError(w http.ResponseWriter, status int, message string) {
	writeJSON(w,status,response{Code:status,Message:message})
}
func (a *adminOrders) limit(fn http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter,r *http.Request) {
		w.Header().Set("Cache-Control","no-store")
		select {
		case a.gate <- struct{}{}:
			defer func(){ <-a.gate }()
		default:
			orderAdminError(w,429,"订单管理请求繁忙，请稍后重试")
			return
		}
		ctx,cancel:=context.WithTimeout(r.Context(),3*time.Second)
		defer cancel()
		fn(w,r.WithContext(ctx))
	}
}

func orderScope(r *http.Request) (string, []any, bool) {
	q:=r.URL.Query()
	game,err:=strconv.ParseInt(q.Get("gameId"),10,64)
	if err!=nil || game<=0 {return "",nil,false}
	where:="game_id=?"
	args:=[]any{game}
	env:=q.Get("env")
	if env!="" {
		if env!="prod" && env!="test" && env!="review" {return "",nil,false}
		where+=" AND env=?"
		args=append(args,env)
	}
	if app:=q.Get("appid");app!="" {
		if len(app)>128 {return "",nil,false}
		where+=" AND appid=?"
		args=append(args,app)
	}
	return where,args,true
}

var orderAdminZone=time.FixedZone("UTC+8",8*3600)

func orderAdminDay(now time.Time) (time.Time,time.Time) {
	local:=now.In(orderAdminZone)
	start:=time.Date(local.Year(),local.Month(),local.Day(),0,0,0,0,orderAdminZone)
	return start.UTC(),start.AddDate(0,0,1).UTC()
}

func orderListFilter(r *http.Request) (string,[]any,int,bool) {
	where,args,ok:=orderScope(r)
	if !ok {return "",nil,0,false}
	q:=r.URL.Query()
	for _,field:=range []struct{param,column string}{{"gameOrderId","game_order_id"},{"platformOrderNo","platform_order_no"}} {
		if v:=q.Get(field.param);v!="" {
			if len(v)>128 {return "",nil,0,false}
			where+=" AND "+field.column+"=?"
			args=append(args,v)
		}
	}
	for _,field:=range []struct{param,column string;max int}{{"paymentStatus","payment_status",1},{"notifyStatus","notify_status",2}} {
		if v:=q.Get(field.param);v!="" {
			n,err:=strconv.Atoi(v)
			if err!=nil || n<0 || n>field.max {return "",nil,0,false}
			where+=" AND "+field.column+"=?"
			args=append(args,n)
			if field.param=="notifyStatus" {where+=" AND payment_status=1"}
		}
	}
	// 日期筛选可选，不限制历史跨度；只校验时间格式及区间顺序。
	if q.Get("startTime")!="" || q.Get("endTime")!="" {
		start,e1:=time.Parse(time.RFC3339,q.Get("startTime"))
		end,e2:=time.Parse(time.RFC3339,q.Get("endTime"))
		if e1!=nil || e2!=nil || !end.After(start) {return "",nil,0,false}
		where+=" AND created_at>=? AND created_at<?"
		args=append(args,start.UTC(),end.UTC())
	}
	if c:=q.Get("cursor");c!="" {
		if len(c)>128 {return "",nil,0,false}
		raw,err:=base64.RawURLEncoding.DecodeString(c)
		if err!=nil {return "",nil,0,false}
		p:=strings.Split(string(raw),"|")
		if len(p)!=2 {return "",nil,0,false}
		t,err:=time.Parse(time.RFC3339,p[0])
		id,e2:=strconv.ParseUint(p[1],10,64)
		if err!=nil || e2!=nil || id==0 {return "",nil,0,false}
		where+=" AND (created_at<? OR (created_at=? AND id<?))"
		args=append(args,t.UTC(),t.UTC(),id)
	}
	size:=20
	if v:=q.Get("pageSize");v!="" {
		n,err:=strconv.Atoi(v)
		if err!=nil || n<1 || n>100 {return "",nil,0,false}
		size=n
	}
	return where,args,size,true
}

func orderAdminDBError(w http.ResponseWriter,r *http.Request,err error) {
	logging.FromContext(r.Context()).WarnContext(r.Context(),"订单管理数据库操作失败","error",err)
	orderAdminError(w,503,"订单管理服务暂时不可用")
}
func (a *adminOrders) list(w http.ResponseWriter,r *http.Request) {
	where,args,size,ok:=orderListFilter(r)
	if !ok {orderAdminError(w,400,"订单查询参数不合法");return}
	args=append(args,size+1)
	rows,err:=a.db.QueryContext(r.Context(),"SELECT "+adminOrderColumns+" FROM game_order WHERE "+where+" ORDER BY created_at DESC,id DESC LIMIT ?",args...)
	if err!=nil {orderAdminDBError(w,r,err);return}
	defer rows.Close()
	items:=make([]adminOrderRow,0,size+1)
	for rows.Next() {
		var o adminOrderRow
		if err=rows.Scan(o.destinations()...);err!=nil {orderAdminDBError(w,r,err);return}
		items=append(items,o)
	}
	if err=rows.Err();err!=nil {orderAdminDBError(w,r,err);return}
	cursor:=""
	if len(items)>size {
		items=items[:size]
		last:=items[size-1]
		cursor=base64.RawURLEncoding.EncodeToString([]byte(last.CreatedAt.UTC().Format(time.RFC3339)+"|"+last.ID))
	}
	writeJSON(w,200,response{Code:0,Message:"success",Data:map[string]any{"items":items,"nextCursor":cursor}})
}
func orderIdentity(r *http.Request) (int64,uint64,bool) {
	game,e1:=strconv.ParseInt(r.URL.Query().Get("gameId"),10,64)
	id,e2:=strconv.ParseUint(r.PathValue("id"),10,64)
	return game,id,e1==nil && e2==nil && game>0 && id>0
}
func (a *adminOrders) detail(w http.ResponseWriter,r *http.Request) {
	game,id,ok:=orderIdentity(r)
	if !ok {orderAdminError(w,400,"订单标识不合法");return}
	var o adminOrderDetail
	dests:=append(o.destinations(),&o.Attach,&o.LastNotifyAt,&o.NotifySuccessAt,&o.NextNotifyAt,&o.LastNotifyError)
	err:=a.db.QueryRowContext(r.Context(),"SELECT "+adminOrderColumns+",attach,last_notify_at,notify_success_at,next_notify_at,last_notify_error FROM game_order WHERE id=? AND game_id=?",id,game).Scan(dests...)
	if errors.Is(err,sql.ErrNoRows) {orderAdminError(w,404,"订单不存在");return}
	if err!=nil {orderAdminDBError(w,r,err);return}
	writeJSON(w,200,response{Code:0,Message:"success",Data:o})
}

func (a *adminOrders) statistics(w http.ResponseWriter,r *http.Request) {
	where,args,ok:=orderScope(r)
	if !ok {orderAdminError(w,400,"订单统计参数不合法");return}
	now:=time.Now()
	start,end:=orderAdminDay(now)
	key:=r.URL.Query().Encode()+"|"+start.Format(time.RFC3339)
	a.mu.Lock()
	entry,found:=a.stats[key]
	a.mu.Unlock()
	if found && now.Before(entry.expires) {writeJSON(w,200,response{Code:0,Message:"success",Data:entry.value});return}
	var s adminOrderStats
	createdArgs:=append(append([]any{},args...),start,end)
	err:=a.db.QueryRowContext(r.Context(),"SELECT COUNT(*) FROM game_order WHERE "+where+" AND created_at>=? AND created_at<?",createdArgs...).Scan(&s.Registered)
	if err!=nil {orderAdminDBError(w,r,err);return}
	err=a.db.QueryRowContext(r.Context(),"SELECT COUNT(*),COALESCE(SUM(amount),0),COALESCE(SUM(notify_status=1),0) FROM game_order WHERE "+where+" AND payment_status=1 AND paid_at>=? AND paid_at<?",createdArgs...).Scan(&s.Paid,&s.PaidAmount,&s.PaidNotified)
	if err!=nil {orderAdminDBError(w,r,err);return}
	err=a.db.QueryRowContext(r.Context(),"SELECT COALESCE(SUM(notify_status=0),0),COALESCE(SUM(notify_status=2),0) FROM game_order WHERE "+where+" AND payment_status=1 AND notify_status IN (0,2)",args...).Scan(&s.Pending,&s.Failed)
	if err!=nil {orderAdminDBError(w,r,err);return}
	a.mu.Lock()
	if len(a.stats)>=256 {clear(a.stats)}
	a.stats[key]=orderStatsEntry{value:s,expires:now.Add(time.Minute)}
	a.mu.Unlock()
	writeJSON(w,200,response{Code:0,Message:"success",Data:s})
}

// The guarded UPDATE serializes with ClaimDue and never shortens an active lease.
// A recent completed failure is also protected conservatively for 60 seconds.
func (a *adminOrders) retry(w http.ResponseWriter,r *http.Request) {
	game,id,ok:=orderIdentity(r)
	if !ok {orderAdminError(w,400,"订单标识不合法");return}
	result,err:=a.db.ExecContext(r.Context(),`UPDATE game_order SET next_notify_at=UTC_TIMESTAMP()
		WHERE id=? AND game_id=? AND payment_status=1 AND notify_status=2
		AND next_notify_at>UTC_TIMESTAMP()
		AND (last_notify_at IS NULL OR last_notify_at<=DATE_SUB(UTC_TIMESTAMP(),INTERVAL 60 SECOND))`,id,game)
	if err!=nil {orderAdminDBError(w,r,err);return}
	n,err:=result.RowsAffected()
	if err!=nil {orderAdminDBError(w,r,err);return}
	slog.Info("订单手动补偿请求","order_id",id,"game_id",game,"remote_addr",r.RemoteAddr,"scheduled",n==1)
	if n==0 {orderAdminError(w,409,"订单不存在、无需重试或处于任务租约保护期");return}
	writeJSON(w,200,response{Code:0,Message:"已安排发货补偿"})
}
