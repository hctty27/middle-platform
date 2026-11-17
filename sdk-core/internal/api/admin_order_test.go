package api

import (
	"database/sql"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

func TestAdminOrderFilter(t *testing.T) {
	now := time.Date(2026, 11, 17, 1, 0, 0, 0, time.UTC)
	cases := []struct {
		query string
		fragment string
		ok bool
	}{
		{"gameId=1&env=prod", "created_at>=?", true},
		{"gameId=1&gameOrderId=abc", "game_order_id=?", true},
		{"gameId=1&notifyStatus=2", "payment_status=1", true},
		{"gameId=1&pageSize=101", "", false},
		{"gameId=1&env=bad", "", false},
		{"gameId=1&cursor=bad", "", false},
		{"gameId=1&startTime=2026-01-01T00:00:00Z&endTime=2026-03-01T00:00:00Z", "", false},
		{"env=prod", "", false},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodGet, "/?"+tc.query, nil)
		where, args, size, ok := orderListFilter(req, now)
		if ok != tc.ok || (ok && (!strings.Contains(where, tc.fragment) || len(args)==0 || size!=20)) {
			t.Errorf("%s: where=%q args=%v size=%d ok=%v",tc.query,where,args,size,ok)
		}
		if ok && strings.Contains(tc.query,"gameOrderId") && strings.Contains(where,"created_at>=?") {
			t.Fatal("exact order number must allow history lookup")
		}
	}
	start,end:=orderAdminDay(now)
	if start.Format(time.RFC3339)!="2026-11-16T16:00:00Z" || end.Sub(start)!=24*time.Hour {
		t.Fatal("incorrect Beijing day boundary")
	}
	cursor:=base64.RawURLEncoding.EncodeToString([]byte("2026-11-17T00:00:00Z|123"))
	where,_,_,ok:=orderListFilter(httptest.NewRequest(http.MethodGet,"/?gameId=1&cursor="+cursor,nil),now)
	if !ok || !strings.Contains(where,"id<?") {t.Fatal("missing stable keyset cursor")}
}

func TestAdminOrderRouteRejectsInvalidScope(t *testing.T) {
	mux:=http.NewServeMux()
	registerAdminOrderRoutes(mux,nil)
	for _,p:=range []string{"/api/core/admin/orders","/api/core/admin/orders/stats","/api/core/admin/orders/123"} {
		rec:=httptest.NewRecorder()
		mux.ServeHTTP(rec,httptest.NewRequest(http.MethodGet,p,nil))
		if rec.Code!=400 {t.Fatalf("%s: %d",p,rec.Code)}
	}
}

// Only use the disposable CI DSN. A session-local temporary table shadows game_order
// without touching the persistent table used by the store package's tests.
func TestAdminOrderRetryMySQL(t *testing.T) {
	dsn:=os.Getenv("TEST_SDK_CORE_DB_DSN")
	if dsn=="" {t.Skip("TEST_SDK_CORE_DB_DSN not configured")}
	db,err:=sql.Open("mysql",dsn)
	if err!=nil {t.Fatal(err)}
	defer db.Close()
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	schema,err:=os.ReadFile(filepath.Join("..","..","..","db","incremental","V0001__add_game_order.sql"))
	if err!=nil {t.Fatal(err)}
	ddl:=strings.Replace(string(schema),"CREATE TABLE game_order","CREATE TEMPORARY TABLE game_order",1)
	if ddl==string(schema) {t.Fatal("cannot construct temporary order schema")}
	if _,err=db.Exec(ddl);err!=nil {t.Fatal(err)}
	insert:=`INSERT INTO game_order(id,appid,game_id,env,game_order_id,amount,attach,game_notify_url,payment_status,notify_status,notify_attempts,last_notify_at,next_notify_at)
	VALUES (1,'app',1,'test','retry',100,'','https://game.example/notify',1,2,3,DATE_SUB(UTC_TIMESTAMP(),INTERVAL 90 SECOND),DATE_ADD(UTC_TIMESTAMP(),INTERVAL 120 SECOND)),
	(2,'app',1,'test','leased',100,'','https://game.example/notify',1,2,3,UTC_TIMESTAMP(),DATE_ADD(UTC_TIMESTAMP(),INTERVAL 60 SECOND)),
	(3,'app',1,'test','unpaid',100,'','https://game.example/notify',0,2,0,NULL,DATE_ADD(UTC_TIMESTAMP(),INTERVAL 120 SECOND)),
	(4,'app',1,'test','success',100,'','https://game.example/notify',1,1,1,NULL,NULL)`
	if _,err=db.Exec(insert);err!=nil {t.Fatal(err)}
	mux:=http.NewServeMux()
	registerAdminOrderRoutes(mux,db)
	cases:=[]struct{path string;want int}{
		{"1/retry?gameId=2",409},{"2/retry?gameId=1",409},{"3/retry?gameId=1",409},
		{"4/retry?gameId=1",409},{"1/retry?gameId=1",200},{"1/retry?gameId=1",409},
	}
	for _,tc:=range cases {
		rec:=httptest.NewRecorder()
		mux.ServeHTTP(rec,httptest.NewRequest(http.MethodPost,"/api/core/admin/orders/"+tc.path,nil))
		if rec.Code!=tc.want {t.Fatalf("%s got %d: %s",tc.path,rec.Code,rec.Body.String())}
	}
	var attempts int
	var status int
	var due bool
	err=db.QueryRow("SELECT notify_attempts,notify_status,next_notify_at<=UTC_TIMESTAMP() FROM game_order WHERE id=1").Scan(&attempts,&status,&due)
	if err!=nil || attempts!=3 || status!=2 || !due {t.Fatalf("retry changed order state: %v %d %d %v",err,attempts,status,due)}
}
