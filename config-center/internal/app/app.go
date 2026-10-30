// Package app 负责配置中心依赖初始化和优雅退出。
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
	"sync/atomic"
	"syscall"
	"time"
	"github.com/go-sql-driver/mysql"
	"github.com/hctty27/middle-platform/config-center/internal/api"
	"github.com/hctty27/middle-platform/config-center/internal/migration"
	"github.com/hctty27/middle-platform/config-center/internal/settings"
	"github.com/hctty27/middle-platform/config-center/internal/store"
	"github.com/hctty27/middle-platform/config-center/internal/webhook"
)

// Run 支持 YAML 文件和旧版环境变量模式。
func Run(configPath string) error {
    cfg, err := settings.LoadFile(configPath)
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
	// 管理写入能力由开关控制，读 API 不受影响。
	var writer api.Writer
	if writesEnabled { writer = repository }
	var handler http.Handler = api.Router(repository)
	if writer != nil { handler = api.RouterWithWrites(repository, writer) }
	var ready atomic.Bool
	ready.Store(true)
	handler = withDrainReadiness(handler, &ready)
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

	workerCtx, cancelWorker := context.WithCancel(context.Background())
	defer cancelWorker()
	workerDone := make(chan struct{})
	if webhookEnabled {
		dispatcher := webhook.NewDispatcher(db, repository.AllowedHosts)
		go func() {
			defer close(workerDone)
			dispatcher.Run(workerCtx)
		}()
	} else {
		close(workerDone)
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("Go config center started", "address", address, "revision", revision, "writes_enabled", writesEnabled, "webhook_enabled", webhookEnabled)
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
	ready.Store(false)
	stop()
	started := time.Now()
	slog.Info("Go config center shutting down")
	shutdownCtx, done := context.WithTimeout(context.Background(), 30*time.Second)
	defer done()
	cancelWorker()
	shutdownErr := server.Shutdown(shutdownCtx)
	if shutdownErr != nil {
		slog.Warn("HTTP graceful shutdown failed; closing active connections", "error", shutdownErr)
		shutdownErr = errors.Join(shutdownErr, server.Close())
	}
	var workerErr error
	select {
	case <-workerDone:
	case <-shutdownCtx.Done():
		workerErr = fmt.Errorf("webhook worker shutdown exceeded deadline: %w", shutdownCtx.Err())
	}
	if err := errors.Join(listenErr, shutdownErr, workerErr); err != nil {
		return err
	}
	slog.Info("Go config center stopped", "duration", time.Since(started))
	return nil
}

func withDrainReadiness(next http.Handler, ready *atomic.Bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health/ready" && !ready.Load() {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("{\"status\":\"DOWN\"}\n"))
			return
		}
		next.ServeHTTP(w, r)
	})
}
