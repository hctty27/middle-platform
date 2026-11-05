package main

import (
    "context"
    "database/sql"
    "errors"
    "flag"
    "fmt"
    "log/slog"
    "net/http"
    "os"
    "os/signal"
    "sync/atomic"
    "syscall"
    "time"

    "github.com/go-sql-driver/mysql"
    "github.com/hctty27/middle-platform/sdk-core/internal/api"
    "github.com/hctty27/middle-platform/sdk-core/internal/mapping"
    "github.com/hctty27/middle-platform/sdk-core/internal/notify"
    "github.com/hctty27/middle-platform/sdk-core/internal/service"
    "github.com/hctty27/middle-platform/sdk-core/internal/settings"
    "github.com/hctty27/middle-platform/sdk-core/internal/store"
    "github.com/hctty27/middle-platform/shared/logging"
    "github.com/hctty27/middle-platform/shared/signature"
)

func main(){
    path:=flag.String("config","configs/config.yaml","YAML configuration file")
    flag.Parse()
    if err:=run(*path);err!=nil{slog.Error("SDK Core 启动或运行失败","error",err);os.Exit(1)}
}

func openDB(cfg settings.Database)(*sql.DB,error){
    parsed,err:=mysql.ParseDSN(cfg.DSN)
    if err!=nil{return nil,fmt.Errorf("数据库 DSN 不合法: %w",err)}
    parsed.ParseTime=true
    parsed.Loc=time.UTC
    parsed.Timeout=5*time.Second
    parsed.ReadTimeout=10*time.Second
    parsed.WriteTimeout=10*time.Second
    if parsed.Params==nil{parsed.Params=make(map[string]string)}
    parsed.Params["time_zone"]="'+00:00'"
    db,err:=sql.Open("mysql",parsed.FormatDSN())
    if err!=nil{return nil,err}
    db.SetMaxOpenConns(cfg.MaxOpenConns)
    db.SetMaxIdleConns(cfg.MaxIdleConns)
    lifetime,_:=time.ParseDuration(cfg.ConnMaxLifetime)
    db.SetConnMaxLifetime(lifetime)
    return db,nil
}

func run(configPath string)error{
    cfg,err:=settings.Load(configPath)
    if err!=nil{return err}
    if err=logging.Init("sdk-core",cfg.Log);err!=nil{return fmt.Errorf("初始化日志失败: %w",err)}
    db,err:=openDB(cfg.Database)
    if err!=nil{return err}
    defer db.Close()
    initCtx,cancel:=context.WithTimeout(context.Background(),10*time.Second)
    defer cancel()
    if err=db.PingContext(initCtx);err!=nil{return fmt.Errorf("连接配置中心数据库失败: %w",err)}
    repository:=store.NewStore(db)
    if err=repository.VerifySchema(initCtx);err!=nil{return err}
    resolver,err:=mapping.LoadDefault()
    if err!=nil{return fmt.Errorf("加载响应字段映射失败: %w",err)}
    if err=resolver.Require(mapping.RouteCoreConfig);err!=nil{return err}

    // 配置查询与订单读写共用一个 sql.DB，订单路由和补偿 Worker 随进程启动。
    // 不自动建表：必须先通过 config-center/cmd/migrate 执行 V12。
        rows, checkErr := db.QueryContext(initCtx, `SELECT id,appid,game_id,env,game_order_id,
            amount,attach,game_notify_url,platform_order_no,paid_at,payment_status,
            notify_status,notify_attempts,last_notify_at,last_notify_error,
            notify_success_at,next_notify_at,created_at,updated_at
            FROM game_order LIMIT 0`)
        if checkErr != nil {
            return fmt.Errorf("检查 game_order 表结构失败（需要 V12）: %w", checkErr)
        }
        if err=rows.Close();err!=nil{return fmt.Errorf("关闭 game_order 表结构检查结果失败: %w",err)}
        orders:=&service.OrderService{
            ConfigReader:repository,
            Orders:store.NewOrderStore(db),
            PlatformSecretReader:repository,
            GameSecretReader:repository,
            Notifier:notify.NewClient(),
        }
        if err=orders.Validate();err!=nil{return err}
        workerCtx,stopWorker:=context.WithCancel(context.Background())
        workerDone:=make(chan struct{})
        go func(){defer close(workerDone);orders.RunWorker(workerCtx)}()
        defer func(){stopWorker();<-workerDone}()

    var ready atomic.Bool
    ready.Store(true)
    server:=&http.Server{
        Addr:cfg.Server.Address,
        Handler:logging.HTTP(cfg.Log,withDrainReadiness(signature.AdminMiddleware(api.RouterWithAdmin(service.NewConfigService(repository,resolver),db,orders), "/api/core/admin", cfg.AdminAuth.Secret),&ready)),
        ReadHeaderTimeout:5*time.Second,ReadTimeout:10*time.Second,
        WriteTimeout:15*time.Second,IdleTimeout:60*time.Second,
    }
    stopCtx,stop:=signal.NotifyContext(context.Background(),os.Interrupt,syscall.SIGTERM)
    defer stop()
    errCh:=make(chan error,1)
    go func(){slog.Info("SDK Core 已启动","address",cfg.Server.Address);errCh<-server.ListenAndServe()}()
    var listenErr error
    select{
    case <-stopCtx.Done():
    case err=<-errCh:
        if !errors.Is(err,http.ErrServerClosed){listenErr=err}
    }
    ready.Store(false)
    stop()
    stopWorker()
    slog.Info("SDK Core 正在关闭")
    shutdownCtx,done:=context.WithTimeout(context.Background(),30*time.Second)
    defer done()
    shutdownErr:=server.Shutdown(shutdownCtx)
    if shutdownErr!=nil{
        slog.Warn("HTTP 优雅关闭失败，正在关闭活动连接","error",shutdownErr)
        shutdownErr=errors.Join(shutdownErr,server.Close())
    }
    if err=errors.Join(listenErr,shutdownErr);err!=nil{return err}
    slog.Info("SDK Core 已停止")
    return nil
}

func withDrainReadiness(next http.Handler,ready *atomic.Bool)http.Handler{
    return http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
        if r.URL.Path=="/health/ready"&&!ready.Load(){
            w.Header().Set("Content-Type","application/json; charset=utf-8")
            w.Header().Set("Cache-Control","no-store")
            w.WriteHeader(http.StatusServiceUnavailable)
            _,_=w.Write([]byte("{\"status\":\"DOWN\"}\n"))
            return
        }
        next.ServeHTTP(w,r)
    })
}
