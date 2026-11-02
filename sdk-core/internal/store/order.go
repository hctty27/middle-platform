package store

import (
    "context"
    "database/sql"
    "errors"
    "fmt"
    "strings"
    "time"

    "github.com/go-sql-driver/mysql"
    "github.com/hctty27/middle-platform/sdk-core/internal/model"
)

// OrderStore 在共享的 config_center 连接池中读写 game_order，不修改配置表。
type OrderStore struct { db *sql.DB }

// NewOrderStore 使用 SDK Core 已有的 sql.DB 连接池。
func NewOrderStore(db *sql.DB) *OrderStore { return &OrderStore{db: db} }

const orderColumns = `id, appid, game_id, env, game_order_id, amount, attach, game_notify_url,
    COALESCE(platform_order_no,''), paid_at, payment_status, notify_status, notify_attempts, next_notify_at`

func scanOrder(row interface{ Scan(...any) error }) (model.Order, error) {
    var o model.Order
    var paid, next sql.NullTime
    err := row.Scan(&o.ID, &o.AppID, &o.GameID, &o.Env, &o.GameOrderID, &o.Amount, &o.Attach,
        &o.GameNotifyURL, &o.PlatformOrderNo, &paid, &o.PaymentStatus, &o.NotifyStatus,
        &o.NotifyAttempts, &next)
    if paid.Valid { o.PaidAt = paid.Time }
    if next.Valid { o.NextNotifyAt = next.Time }
    return o, err
}

// Find 按 AppID 和游戏订单号查询已登记的唯一订单。
func (s *OrderStore) Find(ctx context.Context, appid, gameOrderID string) (model.Order, error) {
    o, err := scanOrder(s.db.QueryRowContext(ctx,
        "SELECT "+orderColumns+" FROM game_order WHERE appid=? AND game_order_id=?",
        appid, gameOrderID))
    if errors.Is(err, sql.ErrNoRows) { return o, model.ErrOrderNotFound }
    return o, err
}

// ByID 按数据库主键获取当前支付及通知状态。
func (s *OrderStore) ByID(ctx context.Context, id int64) (model.Order, error) {
    o, err := scanOrder(s.db.QueryRowContext(ctx,
        "SELECT "+orderColumns+" FROM game_order WHERE id=?", id))
    if errors.Is(err, sql.ErrNoRows) { return o, model.ErrOrderNotFound }
    return o, err
}

// Register 以数据库唯一索引实现跨实例幂等，重复登记不刷新通知地址。
func (s *OrderStore) Register(ctx context.Context, incoming model.Order) error {
    _, err := s.db.ExecContext(ctx, `
        INSERT INTO game_order(appid,game_id,env,game_order_id,amount,attach,game_notify_url)
        VALUES(?,?,?,?,?,?,?)`,
        incoming.AppID, incoming.GameID, incoming.Env, incoming.GameOrderID, incoming.Amount,
        incoming.Attach, incoming.GameNotifyURL)
    if err == nil { return nil }
    var mysqlErr *mysql.MySQLError
    if !errors.As(err, &mysqlErr) || mysqlErr.Number != 1062 {
        return fmt.Errorf("insert game order: %w", err)
    }
    old, lookupErr := s.Find(ctx, incoming.AppID, incoming.GameOrderID)
    if lookupErr != nil { return fmt.Errorf("check duplicate order: %w", lookupErr) }
    if old.GameID != incoming.GameID || old.Env != incoming.Env || old.Amount != incoming.Amount || old.Attach != incoming.Attach {
        return model.ErrOrderConflict
    }
    return nil
}

// ConfirmPayment 仅首次确认支付，平台订单号不能改绑；支付状态不会回退。
func (s *OrderStore) ConfirmPayment(ctx context.Context, id int64, platformOrderNo string, paidAt time.Time) error {
    tx, err := s.db.BeginTx(ctx, nil)
    if err != nil { return err }
    defer tx.Rollback()
    var old sql.NullString
    var paid uint8
    err = tx.QueryRowContext(ctx, "SELECT platform_order_no,payment_status FROM game_order WHERE id=? FOR UPDATE", id).Scan(&old, &paid)
    if errors.Is(err, sql.ErrNoRows) { return model.ErrOrderNotFound }
    if err != nil { return err }
    if paid == 1 {
        if !old.Valid || old.String != platformOrderNo { return model.ErrOrderConflict }
        return tx.Commit()
    }
    if old.Valid && old.String != platformOrderNo { return model.ErrOrderConflict }
    _, err = tx.ExecContext(ctx, `
        UPDATE game_order SET platform_order_no=?,payment_status=1,paid_at=?,
        next_notify_at=UTC_TIMESTAMP(3) WHERE id=? AND payment_status=0`,
        platformOrderNo, paidAt.UTC(), id)
    if err != nil {
        var myErr *mysql.MySQLError
        if errors.As(err, &myErr) && myErr.Number==1062 { return model.ErrOrderConflict }
        return err
    }
    return tx.Commit()
}

// CompleteNotify 使用领取时的尝试序号作为 fencing token，过期任务不得覆盖新结果。
// 成功为终态；失败只为当前持有租约的尝试安排下一次补偿。
func (s *OrderStore) CompleteNotify(ctx context.Context,id int64, attempt int, notifyErr error) error {
    if notifyErr==nil {
        _,err:=s.db.ExecContext(ctx,`
            UPDATE game_order SET notify_status=1,
                notify_success_at=COALESCE(notify_success_at,UTC_TIMESTAMP(3)),
                last_notify_error='',next_notify_at=NULL
            WHERE id=? AND payment_status=1 AND notify_status<>1
              AND notify_attempts=?`,id,attempt)
        return err
    }
    summary:=strings.ReplaceAll(notifyErr.Error(),"\n"," ")
    if len(summary)>256 { summary=summary[:256] }
    _,err:=s.db.ExecContext(ctx,`
        UPDATE game_order SET notify_status=2,last_notify_error=?,
        next_notify_at=DATE_ADD(UTC_TIMESTAMP(3),
            INTERVAL LEAST(3600,30*POW(2,LEAST(7,notify_attempts))) SECOND)
        WHERE id=? AND payment_status=1 AND notify_status<>1
          AND notify_attempts=?`,summary,id,attempt)
    return err
}

// ClaimDue 使用 READ COMMITTED + SKIP LOCKED 领取到期任务，减少高并发扫描的间隙锁冲突。
// InnoDB 仍可能返回 1213 死锁或 1205 锁等待超时；短暂重试，不允许重复提交任务。
func (s *OrderStore) ClaimDue(ctx context.Context) (*model.Order,error) {
    for retry:=0;retry<3;retry++ {
        order,err:=s.claimDueOnce(ctx)
        if err==nil||ctx.Err()!=nil{return order,err}
        var myErr *mysql.MySQLError
        if !errors.As(err,&myErr)||(myErr.Number!=1213&&myErr.Number!=1205){
            return nil,err
        }
        select {
        case <-ctx.Done():return nil,ctx.Err()
        case <-time.After(time.Duration(retry+1)*10*time.Millisecond):
        }
    }
    return s.claimDueOnce(ctx)
}

// claimDueOnce 在事务内写入 60 秒租约，并取得稳定的尝试序号。
// 只在提交事务后将订单交给网络通知，不在事务中调用游戏服务器。
func (s *OrderStore) claimDueOnce(ctx context.Context) (*model.Order,error) {
    tx,err:=s.db.BeginTx(ctx,&sql.TxOptions{Isolation:sql.LevelReadCommitted})
    if err!=nil {return nil,err}
    defer tx.Rollback()
    var id int64
    err=tx.QueryRowContext(ctx,`
        SELECT id FROM game_order
        WHERE payment_status=1 AND notify_status<>1
          AND next_notify_at<=UTC_TIMESTAMP(3)
        ORDER BY next_notify_at,id LIMIT 1 FOR UPDATE SKIP LOCKED`).Scan(&id)
    if errors.Is(err,sql.ErrNoRows) {return nil,nil}
    if err!=nil {return nil,err}
    _,err=tx.ExecContext(ctx,`
        UPDATE game_order SET notify_attempts=notify_attempts+1,
        last_notify_at=UTC_TIMESTAMP(3),
        next_notify_at=DATE_ADD(UTC_TIMESTAMP(3),INTERVAL 60 SECOND)
        WHERE id=?`,id)
    if err!=nil {return nil,err}
    // 在释放行锁前读取订单和尝试序号；防止租约过期后读取到其他 Worker 的新序号。
    o,err:=scanOrder(tx.QueryRowContext(ctx,"SELECT "+orderColumns+" FROM game_order WHERE id=?",id))
    if err!=nil{return nil,err}
    if err=tx.Commit();err!=nil{return nil,err}
    return &o,nil
}

// PingContext 检查共享连接池的 MySQL 可用性。
func (s *OrderStore) PingContext(ctx context.Context) error { return s.db.PingContext(ctx) }
