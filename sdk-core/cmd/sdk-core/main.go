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
    "sync"
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
)

func main(){
    path:=flag.String("config","configs/config.yaml","YAML configuration file")
    flag.Parse()
    if err:=run(*path);err!=nil{slog.Error("sdk-core stopped","error",err);os.Exit(1)}
}

func openDB(cfg settings.Database)(*sql.DB,error){
    parsed,err:=mysql.ParseDSN(cfg.DSN)
    if err!=nil{return nil,fmt.Errorf("invalid DSN: %w",err)}
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

type readyDB struct {config *sql.DB;orders *sql.DB}
func(r readyDB)PingContext(ctx context.Context)error{
    if err:=r.config.PingContext(ctx);err!=nil{return err}
    if r.orders!=nil{return r.orders.PingContext(ctx)}
    return nil
}

func run(configPath string)error{
    cfg,err:=settings.Load(configPath)
    if err!=nil{return err}
    if err=logging.Init("sdk-core",cfg.Log);err!=nil{return fmt.Errorf("initialize logging: %w",err)}
    db,err:=openDB(cfg.Database)
    if err!=nil{return err}
    defer db.Close()
    initCtx,cancel:=context.WithTimeout(context.Background(),10*time.Second)
    defer cancel()
    if err=db.PingContext(initCtx);err!=nil{return fmt.Errorf("connect config center: %w",err)}
    repository:=store.NewStore(db)
    if err=repository.VerifySchema(initCtx);err!=nil{return err}
    resolver,err:=mapping.LoadDefault()
    if err!=nil{return fmt.Errorf("load response mapping: %w",err)}
    if err=resolver.Require(mapping.RouteCoreConfig);err!=nil{return err}

    var ordersDB *sql.DB
    var extra []api.OrderAPI
    var workerCancel context.CancelFunc
    var workerWG sync.WaitGroup
    if cfg.OrderDatabase.DSN!=""{
        ordersDB,err=openDB(cfg.OrderDatabase)
        if err!=nil{return err}
        defer ordersDB.Close()
        if err=ordersDB.PingContext(initCtx);err!=nil{return fmt.Errorf("connect order database: %w",err)}
        var check int
        if err=ordersDB.QueryRowContext(initCtx,"SELECT COUNT(*) FROM game_order WHERE 1=0").Scan(&check);err!=nil{
            return fmt.Errorf("verify order schema: %w",err)
        }
        orders:=&service.OrderService{
            ConfigReader:repository,Orders:store.NewOrderStore(ordersDB),
            PlatformSecretReader:repository,
            GameSecretReader:repository,
            Notifier:notify.NewClient(cfg.GameNotify.AllowedHosts),
            AllowUnverifiedNonProd:cfg.Payment.AllowUnverifiedNonProd,
        }
        if err=orders.Validate();err!=nil{return err}
        extra=append(extra,orders)
        workerCtx,stopWorker:=context.WithCancel(context.Background())
        workerCancel=stopWorker
        workerWG.Add(1)
        go func(){defer workerWG.Done();orders.RunWorker(workerCtx)}()
    }
    if workerCancel!=nil{defer func(){workerCancel();workerWG.Wait()}()}
    var ready atomic.Bool
    ready.Store(true)
    server:=&http.Server{
        Addr:cfg.Server.Address,
        Handler:logging.HTTP(cfg.Log,withDrainReadiness(api.Router(
            service.NewConfigService(repository,resolver),readyDB{config:db,orders:ordersDB},extra...),&ready)),
        ReadHeaderTimeout:5*time.Second,ReadTimeout:10*time.Second,
        WriteTimeout:15*time.Second,IdleTimeout:60*time.Second,
    }
    stopCtx,stop:=signal.NotifyContext(context.Background(),os.Interrupt,syscall.SIGTERM)
    defer stop()
    errCh:=make(chan error,1)
    go func(){slog.Info("sdk-core started","address",cfg.Server.Address);errCh<-server.ListenAndServe()}()
    var listenErr error
    select{
    case <-stopCtx.Done():
    case err=<-errCh:
        if !errors.Is(err,http.ErrServerClosed){listenErr=err}
    }
    ready.Store(false)
    stop()
    if workerCancel!=nil{workerCancel()}
    slog.Info("sdk-core shutting down")
    shutdownCtx,done:=context.WithTimeout(context.Background(),30*time.Second)
    defer done()
    shutdownErr:=server.Shutdown(shutdownCtx)
    if shutdownErr!=nil{
        slog.Warn("HTTP graceful shutdown failed; closing active connections","error",shutdownErr)
        shutdownErr=errors.Join(shutdownErr,server.Close())
    }
    if err=errors.Join(listenErr,shutdownErr);err!=nil{return err}
    slog.Info("sdk-core stopped")
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
