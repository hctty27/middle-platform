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

// OrderStore 仅访问独立订单库，配置中心的只读连接不参与订单事务。
type OrderStore struct { db *sql.DB }

func NewOrderStore(db *sql.DB) *OrderStore { return &OrderStore{db: db} }

const orderColumns = `id, appid, env, game_order_id, amount, attach, game_notify_url,
    COALESCE(platform_order_no,''), paid_at, payment_status, notify_status, notify_attempts, next_notify_at`

func scanOrder(row interface{ Scan(...any) error }) (model.Order, error) {
    var o model.Order
    var paid, next sql.NullTime
    err := row.Scan(&o.ID, &o.AppID, &o.Env, &o.GameOrderID, &o.Amount, &o.Attach,
        &o.GameNotifyURL, &o.PlatformOrderNo, &paid, &o.PaymentStatus, &o.NotifyStatus,
        &o.NotifyAttempts, &next)
    if paid.Valid { o.PaidAt = paid.Time }
    if next.Valid { o.NextNotifyAt = next.Time }
    return o, err
}

func (s *OrderStore) Find(ctx context.Context, appid, gameOrderID string) (model.Order, error) {
    o, err := scanOrder(s.db.QueryRowContext(ctx,
        "SELECT "+orderColumns+" FROM game_order WHERE appid=? AND game_order_id=?",
        appid, gameOrderID))
    if errors.Is(err, sql.ErrNoRows) { return o, model.ErrOrderNotFound }
    return o, err
}

func (s *OrderStore) ByID(ctx context.Context, id int64) (model.Order, error) {
    o, err := scanOrder(s.db.QueryRowContext(ctx,
        "SELECT "+orderColumns+" FROM game_order WHERE id=?", id))
    if errors.Is(err, sql.ErrNoRows) { return o, model.ErrOrderNotFound }
    return o, err
}

// Register 以数据库唯一索引实现跨实例幂等，重复登记不刷新通知地址。
func (s *OrderStore) Register(ctx context.Context, incoming model.Order) error {
    _, err := s.db.ExecContext(ctx, `
        INSERT INTO game_order(appid,env,game_order_id,amount,attach,game_notify_url)
        VALUES(?,?,?,?,?,?)`,
        incoming.AppID, incoming.Env, incoming.GameOrderID, incoming.Amount,
        incoming.Attach, incoming.GameNotifyURL)
    if err == nil { return nil }
    var mysqlErr *mysql.MySQLError
    if !errors.As(err, &mysqlErr) || mysqlErr.Number != 1062 {
        return fmt.Errorf("insert game order: %w", err)
    }
    old, lookupErr := s.Find(ctx, incoming.AppID, incoming.GameOrderID)
    if lookupErr != nil { return fmt.Errorf("check duplicate order: %w", lookupErr) }
    if old.Env != incoming.Env || old.Amount != incoming.Amount || old.Attach != incoming.Attach {
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

// BeginNotify 在网络发送前提交计数和下次执行时间，不持有数据库锁。
func (s *OrderStore) BeginNotify(ctx context.Context, id int64) (bool,error) {
    result,err:=s.db.ExecContext(ctx,`
        UPDATE game_order
        SET notify_attempts=notify_attempts+1,
            last_notify_at=UTC_TIMESTAMP(3),
            next_notify_at=DATE_ADD(UTC_TIMESTAMP(3), INTERVAL 60 SECOND)
        WHERE id=? AND payment_status=1 AND notify_status<>1`,id)
    if err!=nil { return false,err }
    n,err:=result.RowsAffected()
    return n==1,err
}

// CompleteNotify 单向提交 SUCCESS；后到的失败永远不能覆盖成功。
func (s *OrderStore) CompleteNotify(ctx context.Context,id int64, notifyErr error) error {
    if notifyErr==nil {
        _,err:=s.db.ExecContext(ctx,`
            UPDATE game_order SET notify_status=1,
                notify_success_at=COALESCE(notify_success_at,UTC_TIMESTAMP(3)),
                last_notify_error='',next_notify_at=NULL
            WHERE id=? AND notify_status<>1`,id)
        return err
    }
    summary:=strings.ReplaceAll(notifyErr.Error(),"
"," ")
    if len(summary)>256 { summary=summary[:256] }
    _,err:=s.db.ExecContext(ctx,`
        UPDATE game_order SET notify_status=2,last_notify_error=?,
        next_notify_at=DATE_ADD(UTC_TIMESTAMP(3),
            INTERVAL LEAST(3600,30*POW(2,LEAST(7,notify_attempts))) SECOND)
        WHERE id=? AND notify_status<>1`,summary,id)
    return err
}

// ClaimDue 利用 InnoDB 行锁领取到期任务，适合多实例任务队列。
func (s *OrderStore) ClaimDue(ctx context.Context) (*model.Order,error) {
    tx,err:=s.db.BeginTx(ctx,nil)
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
    if err=tx.Commit();err!=nil {return nil,err}
    o,err:=s.ByID(ctx,id)
    return &o,err
}

func (s *OrderStore) PingContext(ctx context.Context) error { return s.db.PingContext(ctx) }
