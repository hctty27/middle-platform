// Package app 负责 SDK Service 依赖初始化、适配器组装和服务生命周期。
package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"hitall.com/middle-platform/sdk-service/internal/adapter/rolecontrol"
	"hitall.com/middle-platform/sdk-service/internal/adapter/rolequery"
	"hitall.com/middle-platform/sdk-service/internal/adapter/serverquery"
	"hitall.com/middle-platform/sdk-service/internal/adapter/transport"
	"hitall.com/middle-platform/sdk-service/internal/api"
	"hitall.com/middle-platform/sdk-service/internal/platform"
	"hitall.com/middle-platform/sdk-service/internal/service"
	"hitall.com/middle-platform/sdk-service/internal/settings"
	"hitall.com/middle-platform/sdk-service/internal/store"
	"hitall.com/middle-platform/shared/bootstrap"
	"hitall.com/middle-platform/shared/logging"
)

func openDB(cfg settings.Database) (*sql.DB, error) {
	return bootstrap.OpenMySQL(bootstrap.MySQLConfig{
		DSN:             cfg.DSN,
		MaxOpenConns:    cfg.MaxOpenConns,
		MaxIdleConns:    cfg.MaxIdleConns,
		ConnMaxLifetime: cfg.ConnMaxLifetime,
	})
}

// Run 加载指定配置，注册内置适配器并启动业务转发服务。
func Run(configPath string) error {
	cfg, err := settings.Load(configPath)
	if err != nil {
		return err
	}
	if err := logging.Init("sdk-service", cfg.Log); err != nil {
		return err
	}
	stopCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := openDB(cfg.Database)
	if err != nil {
		return err
	}
	defer db.Close()
	stopDiagnostics, err := bootstrap.StartLoadDiagnostics(db)
	if err != nil {
		return err
	}
	defer stopDiagnostics()

	initCtx, cancel := context.WithTimeout(stopCtx, 10*time.Second)
	defer cancel()
	if err := db.PingContext(initCtx); err != nil {
		return fmt.Errorf("连接配置中心数据库失败: %w", err)
	}

	repository := store.New(db)
	if err := repository.VerifySchema(initCtx); err != nil {
		return err
	}
	routes, err := store.NewCachedReader(repository, cfg.ConfigCache.Options())
	if err != nil {
		return err
	}
	defer routes.Close()

	timeout, _ := time.ParseDuration(cfg.Game.Timeout)
	gameClient := transport.NewHTTPClient(timeout)

	roleRegistry, err := rolequery.NewRegistry(map[string]rolequery.Adapter{
		"standard": &rolequery.Standard{Client: gameClient},
	})
	if err != nil {
		return err
	}
	roleQuerier := &service.RoleQueryService{Routes: routes, Adapters: roleRegistry}

	serverRegistry, err := serverquery.NewRegistry(map[string]serverquery.Adapter{
		"standard": &serverquery.Standard{Client: gameClient},
	})
	if err != nil {
		return err
	}
	serverQuerier := &service.ServerQueryService{Routes: routes, Adapters: serverRegistry}

	controlRegistry, err := rolecontrol.NewRegistry(map[string]rolecontrol.Adapter{
		"standard": &rolecontrol.Standard{Client: gameClient},
	})
	if err != nil {
		return err
	}
	controller := &service.RoleControlService{Routes: routes, Adapters: controlRegistry}

	slowThreshold, _ := time.ParseDuration(cfg.Log.SlowThreshold)
	publisher, err := platform.NewPublisherWithOptions(cfg.Platform.EventReportURL, cfg.Platform.SubscribeMessageURL, cfg.Platform.UserCheckURL, platform.PublisherOptions{
		IsolateConnections: cfg.Platform.IsolateConnections,
		MaxIdlePerHost:     cfg.Platform.MaxIdlePerHost,
		EventMaxConns:      cfg.Platform.EventMaxConns,
		AccessEnabled:     cfg.Log.AccessEnabled,
		SlowThreshold:      slowThreshold,
	})
	if err != nil {
		return fmt.Errorf("初始化平台上报客户端失败: %w", err)
	}
	defer publisher.CloseIdleConnections()
	checker := &service.UserCheckService{Routes: routes, Sender: publisher}
	outbound := &service.OutboundService{Routes: routes, Sender: publisher}

	requestTimeout, _ := time.ParseDuration(cfg.Server.RequestTimeout)
	routerOptions := api.RouterOptions{RequestTimeout: requestTimeout}
	health := bootstrap.NewHealthState()
	health.SetReady(true)
	server := &http.Server{
		Addr: cfg.Server.Address,
		Handler: logging.HTTP(cfg.Log, health.Middleware(api.RouterWithOptions(
			checker, db, roleQuerier, serverQuerier, controller, outbound, outbound, routerOptions,
		))),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      max(timeout+5*time.Second, requestTimeout+2*time.Second),
		IdleTimeout:       60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("SDK Service 已启动", "address", cfg.Server.Address)
		errCh <- server.ListenAndServe()
	}()

	var listenErr error
	select {
	case <-stopCtx.Done():
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			listenErr = err
		}
	}

	health.SetReady(false)
	stop()
	started := time.Now()
	slog.Info("SDK Service 正在关闭")
	shutdownCtx, finish := context.WithTimeout(context.Background(), bootstrap.HTTPShutdownTimeout)
	defer finish()
	shutdownErr := server.Shutdown(shutdownCtx)
	if shutdownErr != nil {
		slog.Warn("HTTP 优雅关闭失败，正在关闭活动连接", "error", shutdownErr)
		shutdownErr = errors.Join(shutdownErr, server.Close())
	}
	if err := errors.Join(listenErr, shutdownErr); err != nil {
		return err
	}

	slog.Info("SDK Service 已停止", "duration", time.Since(started))
	return nil
}
