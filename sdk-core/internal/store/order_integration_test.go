package store

import (
    "context"
    "errors"
    "os"
    "path/filepath"
    "testing"
    "time"

    "database/sql"

    _ "github.com/go-sql-driver/mysql"
    "github.com/hctty27/middle-platform/sdk-core/internal/model"
)

// 使用 CI Runner 的隔离 MySQL，测试数据库唯一约束与条件更新。
func TestOrderStoreLifecycle(t *testing.T) {
    dsn:=os.Getenv("TEST_SDK_CORE_DB_DSN")
    if dsn=="" {t.Skip("TEST_SDK_CORE_DB_DSN not configured")}
    db,err:=sql.Open("mysql",dsn)
    if err!=nil{t.Fatal(err)}
    defer db.Close()
    ctx,cancel:=context.WithTimeout(context.Background(),15*time.Second)
    defer cancel()
    if err=db.PingContext(ctx);err!=nil{t.Fatal(err)}
    if _,err=db.ExecContext(ctx,"DROP TABLE IF EXISTS game_order");err!=nil{t.Fatal(err)}
    defer db.ExecContext(context.Background(),"DROP TABLE IF EXISTS game_order")
    schema,err:=os.ReadFile(filepath.Join("..","..","db","migration","V1__create_game_order.sql"))
    if err!=nil{t.Fatal(err)}
    if _,err=db.ExecContext(ctx,string(schema));err!=nil{t.Fatal(err)}
    repo:=NewOrderStore(db)
    source:=model.Order{AppID:"app1",GameID:1,Env:"test",GameOrderID:"g1",Amount:600,Attach:"",GameNotifyURL:"https://game.example.com/notify"}
    if err=repo.Register(ctx,source);err!=nil{t.Fatal(err)}
    if err=repo.Register(ctx,source);err!=nil{t.Fatalf("repeat should be idempotent: %v",err)}
    moved:=source
    moved.GameNotifyURL="https://game-new.example.com/notify"
    if err=repo.Register(ctx,moved);err!=nil{t.Fatalf("URL snapshot changed: %v",err)}
    mismatch:=source
    mismatch.Amount=100
    if err=repo.Register(ctx,mismatch);!errors.Is(err,model.ErrOrderConflict){t.Fatalf("expected conflict, got %v",err)}
    duplicateEnv:=source
    duplicateEnv.Env="review"
    if err=repo.Register(ctx,duplicateEnv);!errors.Is(err,model.ErrOrderConflict){t.Fatalf("cross-env key not enforced: %v",err)}
    found,err:=repo.Find(ctx,"app1","g1")
    if err!=nil{t.Fatal(err)}
    if found.GameID!=source.GameID||found.GameNotifyURL!=source.GameNotifyURL {t.Fatal("snapshot overwritten")}
    paidAt:=time.Now().UTC().Truncate(time.Second)
    if err=repo.ConfirmPayment(ctx,found.ID,"p1",paidAt);err!=nil{t.Fatal(err)}
    if err=repo.ConfirmPayment(ctx,found.ID,"p1",paidAt);err!=nil{t.Fatalf("duplicate payment: %v",err)}
    if err=repo.ConfirmPayment(ctx,found.ID,"p2",paidAt);!errors.Is(err,model.ErrOrderConflict){t.Fatalf("payment changed: %v",err)}
    other:=source
    other.GameOrderID="g2"
    if err=repo.Register(ctx,other);err!=nil{t.Fatal(err)}
    second,err:=repo.Find(ctx,"app1","g2")
    if err!=nil{t.Fatal(err)}
    if err=repo.ConfirmPayment(ctx,second.ID,"p1",paidAt);!errors.Is(err,model.ErrOrderConflict){t.Fatalf("platform unique constraint: %v",err)}
    claimed,err:=repo.ClaimDue(ctx)
    if err!=nil{t.Fatal(err)}
    if claimed==nil||claimed.ID!=found.ID {t.Fatal("missing due job")}
    again,err:=repo.ClaimDue(ctx)
    if err!=nil{t.Fatal(err)}
    if again!=nil {t.Fatal("job claimed twice")}
    if err=repo.CompleteNotify(ctx,found.ID,nil);err!=nil{t.Fatal(err)}
    if err=repo.CompleteNotify(ctx,found.ID,errors.New("late failure"));err!=nil{t.Fatal(err)}
    current,err:=repo.ByID(ctx,found.ID)
    if err!=nil{t.Fatal(err)}
    if current.PaymentStatus!=1||current.NotifyStatus!=1 {t.Fatal("terminal success overwritten")}
    allowed,err:=repo.BeginNotify(ctx,found.ID)
    if err!=nil{t.Fatal(err)}
    if allowed {t.Fatal("successful notification retried")}
}
