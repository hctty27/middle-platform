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

	db, err := openDB(cfg.Database)
	if err != nil {
		return err
	}
	defer db.Close()

	initCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.PingContext(initCtx); err != nil {
		return fmt.Errorf("连接配置中心数据库失败: %w", err)
	}

	routes := store.New(db)
	if err := routes.VerifySchema(initCtx); err != nil {
		return err
	}

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

	publisher, err := platform.NewPublisher(cfg.Platform.EventReportURL, cfg.Platform.SubscribeMessageURL, cfg.Platform.UserCheckURL)
	if err != nil {
		return fmt.Errorf("初始化平台上报客户端失败: %w", err)
	}
	checker := &service.UserCheckService{Routes: routes, Sender: publisher}
	outbound := &service.OutboundService{Routes: routes, Sender: publisher}

	server := &http.Server{
		Addr: cfg.Server.Address,
		Handler: logging.HTTP(cfg.Log, api.RouterWithPublishers(
			checker, db, roleQuerier, serverQuerier, controller, outbound, outbound,
		)),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      timeout + 5*time.Second,
		IdleTimeout:       60 * time.Second,
	}

	stopCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		slog.Info("SDK Service 已启动", "address", cfg.Server.Address)
		errCh <- server.ListenAndServe()
	}()

	select {
	case <-stopCtx.Done():
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	}

	slog.Info("SDK Service 正在关闭")
	shutdownCtx, finish := context.WithTimeout(context.Background(), 20*time.Second)
	defer finish()
	if err := server.Shutdown(shutdownCtx); err != nil {
		_ = server.Close()
		return fmt.Errorf("HTTP 服务优雅关闭失败: %w", err)
	}

	slog.Info("SDK Service 已停止")
	return nil
}
