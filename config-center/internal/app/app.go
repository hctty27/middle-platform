// Package app 负责配置中心依赖初始化和优雅退出。
package app

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	"github.com/go-sql-driver/mysql"
	"github.com/hctty27/middle-platform/config-center/internal/api"
	"github.com/hctty27/middle-platform/config-center/internal/migration"
	"github.com/hctty27/middle-platform/config-center/internal/settings"
	"github.com/hctty27/middle-platform/config-center/internal/store"
	"github.com/hctty27/middle-platform/config-center/internal/webhook"
)

// Run 完成参数解析、数据库检查、HTTP 路由和回调调度器启动，并在终止信号后优雅退出。
func Run() error {
	cfg, err := settings.Load()
	if err != nil { return err }
	dsn := cfg.DSN
	parsed, err := mysql.ParseDSN(dsn)
	if err != nil {
		return err
	}
	parsed.ParseTime = true
	parsed.Loc = time.UTC
	parsed.Timeout = 5 * time.Second
	parsed.ReadTimeout = 10 * time.Second
	parsed.WriteTimeout = 10 * time.Second

	// 启动前使用 MySQL 命名锁执行 Go 迁移，避免集群实例并行修改表结构。
	migrationCtx, migrationCancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer migrationCancel()
	if err := migration.Run(migrationCtx, parsed.FormatDSN(), migration.Options{
		AllowLegacyUpgrade: cfg.AllowLegacyUpgrade,
	}); err != nil { return err }

	db, err := sql.Open("mysql", parsed.FormatDSN())
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(10 * time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		return err
	}
	var revision int64
	if err := db.QueryRowContext(ctx, "SELECT revision FROM config_revision WHERE id=1").Scan(&revision); err != nil {
		return err
	}

	address := cfg.Address
	repository := &store.Store{DB: db, AllowedHosts: cfg.AllowedHosts}
	writesEnabled := cfg.WritesEnabled
	webhookEnabled := cfg.WebhookEnabled
	// 管理接口的写权限由运行参数控制，读接口始终使用相同的数据仓库。
	var writer api.Writer
	if writesEnabled { writer = repository }
	var handler http.Handler = api.Router(repository)
	if writer != nil { handler = api.RouterWithWrites(repository, writer) }
	server := &http.Server{
		Addr:              address,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       90 * time.Second,
	}
	stopCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if webhookEnabled {
		dispatcher := webhook.NewDispatcher(db, repository.AllowedHosts)
		go dispatcher.Run(stopCtx)
	}
	errCh := make(chan error, 1)
	go func() {
		slog.Info("Go config center started", "address", address, "revision", revision, "writes_enabled", writesEnabled, "webhook_enabled", webhookEnabled)
		errCh <- server.ListenAndServe()
	}()
	select {
	case <-stopCtx.Done():
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
	shutdownCtx, done := context.WithTimeout(context.Background(), 15*time.Second)
	defer done()
	return server.Shutdown(shutdownCtx)
}
