// 配置中心接入示例：仅同步本地副本，不提供 Starter SDK。
package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
    "flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/hctty27/middle-platform/config-center-access/internal/api"
	"github.com/hctty27/middle-platform/config-center-access/internal/remote"
	"github.com/hctty27/middle-platform/config-center-access/internal/settings"
	"github.com/hctty27/middle-platform/config-center-access/internal/store"
	"github.com/hctty27/middle-platform/config-center-access/internal/syncer"
	_ "github.com/go-sql-driver/mysql"
	"github.com/hctty27/middle-platform/shared/logging"
)

// main 统一处理启动与运行失败。
func main() {
    configPath := flag.String("config", "", "YAML configuration file")
    flag.Parse()
    if err := run(*configPath); err != nil {
		slog.Error("config access stopped", "error", err)
		os.Exit(1)
	}
}

// run 初始化本地副本和同步服务，并支持优雅退出。
func run(configPath string) error {
    cfg, err := settings.LoadFile(configPath)
	if err != nil {
		return err
	}
	if err := logging.Init("config-center-access", cfg.Log); err != nil { return err }
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := sql.Open("mysql", cfg.DSN)
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)

	startupCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := db.PingContext(startupCtx); err != nil {
		return err
	}
	replica := store.New(db)
	if err := replica.EnsureSchema(startupCtx); err != nil {
		return err
	}
	center := remote.New(cfg.CenterURL, cfg.ConnectTimeout, cfg.RequestTimeout)
	sync := syncer.New(replica, center)
	trigger := syncer.NewTrigger(sync, cfg.ReconcileInterval)
	workerCtx, cancelWorker := context.WithCancel(context.Background())
	defer cancelWorker()
	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		trigger.Run(workerCtx)
	}()


	var ready atomic.Bool
	ready.Store(true)
	server := &http.Server{
		Addr: cfg.Address,
		Handler: logging.HTTP(cfg.Log, withDrainReadiness(api.Router(replica, trigger, cfg.WebhookSecret), &ready)),
		ReadHeaderTimeout: 5 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		slog.Info("config access started", "address", cfg.Address, "center", cfg.CenterURL)
		errCh <- server.ListenAndServe()
	}()

	var listenErr error
	select {
	case <-ctx.Done():
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			listenErr = err
		}
	}
	ready.Store(false)
	stop()
	started := time.Now()
	slog.Info("config access shutting down")
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
		workerErr = fmt.Errorf("sync worker shutdown exceeded deadline: %w", shutdownCtx.Err())
	}
	if err := errors.Join(listenErr, shutdownErr, workerErr); err != nil {
		return err
	}
	slog.Info("config access stopped", "duration", time.Since(started))
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
