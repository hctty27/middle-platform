// Package app 负责配置接入端依赖初始化和服务生命周期。
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

	_ "github.com/go-sql-driver/mysql"
	"hitall.com/middle-platform/config-center-access/internal/api"
	"hitall.com/middle-platform/config-center-access/internal/remote"
	"hitall.com/middle-platform/config-center-access/internal/settings"
	"hitall.com/middle-platform/config-center-access/internal/store"
	"hitall.com/middle-platform/config-center-access/internal/syncer"
	"hitall.com/middle-platform/shared/bootstrap"
	"hitall.com/middle-platform/shared/logging"
)

// Run 加载指定配置，初始化本地副本和同步服务，并支持优雅退出。
func Run(configPath string) error {
	cfg, err := settings.LoadFile(configPath)
	if err != nil {
		return err
	}
	if err := logging.Init("config-center-access", cfg.Log); err != nil {
		return err
	}

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
	if err := replica.VerifySchema(startupCtx); err != nil {
		return err
	}

	center := remote.New(cfg.CenterURL, cfg.ConnectTimeout, cfg.RequestTimeout, cfg.SubscriberCode, cfg.SubscriberSecret)
	synchronizer := syncer.New(replica, center)
	trigger := syncer.NewTrigger(synchronizer, cfg.ReconcileInterval)

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
		Addr:              cfg.Address,
		Handler:           logging.HTTP(cfg.Log, bootstrap.ReadinessMiddleware(api.Router(replica, trigger, cfg.SubscriberCode, cfg.SubscriberSecret), &ready)),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("配置接入服务已启动", "address", cfg.Address, "center", cfg.CenterURL)
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
	slog.Info("配置接入服务正在关闭")

	shutdownCtx, done := context.WithTimeout(context.Background(), 30*time.Second)
	defer done()
	cancelWorker()

	shutdownErr := server.Shutdown(shutdownCtx)
	if shutdownErr != nil {
		slog.Warn("HTTP 优雅关闭失败，正在关闭活动连接", "error", shutdownErr)
		shutdownErr = errors.Join(shutdownErr, server.Close())
	}

	var workerErr error
	select {
	case <-workerDone:
	case <-shutdownCtx.Done():
		workerErr = fmt.Errorf("同步工作线程关闭超时: %w", shutdownCtx.Err())
	}

	if err := errors.Join(listenErr, shutdownErr, workerErr); err != nil {
		return err
	}
	slog.Info("配置接入服务已停止", "duration", time.Since(started))
	return nil
}
