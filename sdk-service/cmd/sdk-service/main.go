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
    "syscall"
    "time"

    "github.com/hctty27/middle-platform/sdk-service/internal/adapter/usercheck"
    "github.com/hctty27/middle-platform/sdk-service/internal/adapter/rolequery"
    "github.com/hctty27/middle-platform/sdk-service/internal/adapter/rolecontrol"
    "github.com/hctty27/middle-platform/sdk-service/internal/adapter/serverquery"
    "github.com/hctty27/middle-platform/sdk-service/internal/api"
    "github.com/hctty27/middle-platform/sdk-service/internal/platform"
    "github.com/hctty27/middle-platform/sdk-service/internal/service"
    "github.com/hctty27/middle-platform/sdk-service/internal/settings"
    "github.com/hctty27/middle-platform/sdk-service/internal/store"
    "github.com/hctty27/middle-platform/shared/bootstrap"
    "github.com/hctty27/middle-platform/shared/logging"
)

func main() {
    path := flag.String("config", "configs/config.yaml", "YAML 配置文件")
    flag.Parse()
    if err := run(*path); err != nil {
        slog.Error("SDK Service 启动或运行失败", "error", err)
        os.Exit(1)
    }
}

func openDB(cfg settings.Database) (*sql.DB,error){
 return bootstrap.OpenMySQL(bootstrap.MySQLConfig{
  DSN:cfg.DSN,MaxOpenConns:cfg.MaxOpenConns,
  MaxIdleConns:cfg.MaxIdleConns,ConnMaxLifetime:cfg.ConnMaxLifetime,
 })
}

func run(path string) error {
    cfg, err := settings.Load(path)
    if err != nil { return err }
    if err := logging.Init("sdk-service", cfg.Log); err != nil { return err }
    db, err := openDB(cfg.Database)
    if err != nil { return err }
    defer db.Close()
    initCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()
    if err := db.PingContext(initCtx); err != nil { return fmt.Errorf("连接配置中心数据库失败: %w", err) }
    routes := store.New(db)
    if err := routes.VerifySchema(initCtx); err != nil { return err }

    timeout, _ := time.ParseDuration(cfg.Game.Timeout)
    gameClient := usercheck.NewHTTPClient(timeout)
    registry, err := usercheck.NewRegistry(map[string]usercheck.Adapter{
        "standard": &usercheck.Standard{Client: gameClient},
    })
    if err != nil { return err }
    checker := &service.UserCheckService{Routes: routes, Adapters: registry}
    roleRegistry, err := rolequery.NewRegistry(map[string]rolequery.Adapter{
        "standard": &rolequery.Standard{Client: gameClient},
    })
    if err != nil { return err }
    roleQuerier := &service.RoleQueryService{Routes: routes, Adapters: roleRegistry}
    serverRegistry, err := serverquery.NewRegistry(map[string]serverquery.Adapter{
        "standard": &serverquery.Standard{Client: gameClient},
    })
    if err != nil { return err }
    serverQuerier := &service.ServerQueryService{Routes: routes, Adapters: serverRegistry}
    controlRegistry, err := rolecontrol.NewRegistry(map[string]rolecontrol.Adapter{
        "standard": &rolecontrol.Standard{Client: gameClient},
    })
    if err != nil { return err }
    controller := &service.RoleControlService{Routes: routes, Adapters: controlRegistry}
    publisher, err := platform.NewPublisher(cfg.Platform.EventReportURL, cfg.Platform.SubscribeMessageURL)
    if err != nil { return fmt.Errorf("初始化平台上报客户端失败: %w", err) }
    outbound := &service.OutboundService{Routes: routes, Sender: publisher}
    srv := &http.Server{
        Addr: cfg.Server.Address,
        Handler: logging.HTTP(cfg.Log, api.RouterWithPublishers(checker, db, roleQuerier, serverQuerier, controller, outbound, outbound)),
        ReadHeaderTimeout: 5*time.Second,
        ReadTimeout: 10*time.Second,
        WriteTimeout: timeout + 5*time.Second,
        IdleTimeout: 60*time.Second,
    }
    stopCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
    defer stop()
    done := make(chan error, 1)
    go func() {
        slog.Info("SDK Service 已启动", "address", cfg.Server.Address)
        done <- srv.ListenAndServe()
    }()
    select {
    case <-stopCtx.Done():
    case err = <-done:
        if !errors.Is(err, http.ErrServerClosed) { return err }
    }
    slog.Info("SDK Service 正在关闭")
    shutdownCtx, finish := context.WithTimeout(context.Background(), 20*time.Second)
    defer finish()
    if err := srv.Shutdown(shutdownCtx); err != nil {
        _ = srv.Close()
        return fmt.Errorf("HTTP 服务优雅关闭失败: %w", err)
    }
    slog.Info("SDK Service 已停止")
    return nil
}
