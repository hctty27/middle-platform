package store

import (
    "context"
    "database/sql"
    "errors"
    "fmt"
    "os"
    "path/filepath"
    "sync"
    "testing"
    "time"

    "github.com/hctty27/middle-platform/sdk-core/internal/model"
)

// TestConcurrentNotifyClaims 使用两个独立连接池验证领取唯一性、租约恢复和过期结果隔离。
func TestConcurrentNotifyClaims(t *testing.T) {
    dsn:=os.Getenv("TEST_SDK_CORE_DB_DSN")
    if dsn==""{t.Skip("TEST_SDK_CORE_DB_DSN not configured")}
    first,err:=sql.Open("mysql",dsn)
    if err!=nil{t.Fatal(err)}
    defer first.Close()
    second,err:=sql.Open("mysql",dsn)
    if err!=nil{t.Fatal(err)}
    defer second.Close()
    first.SetMaxOpenConns(16)
    second.SetMaxOpenConns(16)

    ctx,cancel:=context.WithTimeout(context.Background(),30*time.Second)
    defer cancel()
    if _,err=first.ExecContext(ctx,"DROP TABLE IF EXISTS game_order");err!=nil{t.Fatal(err)}
    defer first.ExecContext(context.Background(),"DROP TABLE IF EXISTS game_order")
    schema,err:=os.ReadFile(filepath.Join("..","..","..","config-center","db","sql","add_game_order.sql"))
    if err!=nil{t.Fatal(err)}
    if _,err=first.ExecContext(ctx,string(schema));err!=nil{t.Fatal(err)}

    stores:=[]*OrderStore{NewOrderStore(first),NewOrderStore(second)}
    const total=16
    for i:=0;i<total;i++{
        order:=model.Order{
            AppID:"app1001", GameID:1,Env:"prod",
            GameOrderID:fmt.Sprintf("concurrent-%d",i), Amount:600,
            GameNotifyURL:"https://game.example.com/notify",
        }
        if err=stores[0].Register(ctx,order);err!=nil{t.Fatal(err)}
        found,findErr:=stores[0].Find(ctx,order.AppID,order.GameOrderID)
        if findErr!=nil{t.Fatal(findErr)}
        if err=stores[0].ConfirmPayment(ctx,found.ID,fmt.Sprintf("platform-%d",i),time.Now().UTC());err!=nil{t.Fatal(err)}
    }

    // 多个 SDK Core 实例争抢同一批订单，每笔订单只能被领取一次。
    var mu sync.Mutex
    seen:=make(map[int64]int)
    errs:=make(chan error,8)
    finished:=make(chan struct{})
    var once sync.Once
    var wg sync.WaitGroup
    for i:=0;i<8;i++{
        wg.Add(1)
        go func(worker int) {
            defer wg.Done()
            s:=stores[worker%len(stores)]
            for {
                select{case <-ctx.Done():return;case <-finished:return;default:}
                item,claimErr:=s.ClaimDue(ctx)
                if claimErr!=nil{
                    select{case errs<-claimErr:default:}
                    return
                }
                if item==nil{time.Sleep(10*time.Millisecond);continue}
                mu.Lock()
                seen[item.ID]++
                if len(seen)==total{once.Do(func(){close(finished)})}
                mu.Unlock()
                if doneErr:=s.CompleteNotify(ctx,item.ID,item.NotifyAttempts,nil);doneErr!=nil{
                    select{case errs<-doneErr:default:}
                    return
                }
            }
        }(i)
    }
    select{
    case <-finished:
    case <-ctx.Done():t.Fatal("concurrent consumers timed out")
    }
    wg.Wait()
    close(errs)
    for claimErr:=range errs{t.Fatal(claimErr)}
    if len(seen)!=total{t.Fatalf("received %d/%d orders",len(seen),total)}
    for id,count:=range seen{
        if count!=1{t.Fatalf("order %d was claimed %d times",id,count)}
        order,lookupErr:=stores[0].ByID(ctx,id)
        if lookupErr!=nil||order.NotifyStatus!=1||order.NotifyAttempts!=1{
            t.Fatalf("order %d not completed once: %+v err=%v",id,order,lookupErr)
        }
    }
    if item,err:=stores[0].ClaimDue(ctx);err!=nil||item!=nil{
        t.Fatalf("completed orders became due: %+v %v",item,err)
    }

    // 模拟进程崩溃后租约过期；新租约的序号必须使旧任务结果失效。
    extra:=model.Order{AppID:"app1001",GameID:1,Env:"prod",
        GameOrderID:"expired-lease",Amount:600,GameNotifyURL:"https://game.example.com/notify"}
    if err=stores[0].Register(ctx,extra);err!=nil{t.Fatal(err)}
    item,err:=stores[0].Find(ctx,extra.AppID,extra.GameOrderID)
    if err!=nil{t.Fatal(err)}
    if err=stores[0].ConfirmPayment(ctx,item.ID,"platform-expired",time.Now().UTC());err!=nil{t.Fatal(err)}
    stale,err:=stores[0].ClaimDue(ctx)
    if err!=nil||stale==nil||stale.ID!=item.ID{t.Fatalf("first lease: %+v %v",stale,err)}
    if _,err=first.ExecContext(ctx, "UPDATE game_order SET next_notify_at=UTC_TIMESTAMP(3)-INTERVAL 1 SECOND WHERE id=?",item.ID);err!=nil{t.Fatal(err)}
    current,err:=stores[1].ClaimDue(ctx)
    if err!=nil||current==nil||current.ID!=item.ID||current.NotifyAttempts!=stale.NotifyAttempts+1{
        t.Fatalf("takeover lease: %+v err=%v",current,err)
    }
    // 新租约出现后，旧租约无论返回成功或失败均不能抢占状态。
    if err=stores[0].CompleteNotify(ctx,item.ID,stale.NotifyAttempts,nil);err!=nil{t.Fatal(err)}
    if err=stores[0].CompleteNotify(ctx,item.ID,stale.NotifyAttempts,errors.New("stale failure"));err!=nil{t.Fatal(err)}
    var status,attempts int
    if err=first.QueryRowContext(ctx,"SELECT notify_status,notify_attempts FROM game_order WHERE id=?",item.ID).Scan(&status,&attempts);err!=nil{t.Fatal(err)}
    if status!=0||attempts!=current.NotifyAttempts{
        t.Fatalf("stale failure modified current claim: status=%d attempts=%d",status,attempts)
    }
    if err=stores[1].CompleteNotify(ctx,item.ID,current.NotifyAttempts,nil);err!=nil{t.Fatal(err)}
    if err=stores[0].CompleteNotify(ctx,item.ID,stale.NotifyAttempts,errors.New("late failure"));err!=nil{t.Fatal(err)}
    last,err:=stores[0].ByID(ctx,item.ID)
    if err!=nil||last.NotifyStatus!=1{t.Fatalf("stale completion overwrote success: %+v %v",last,err)}
}
