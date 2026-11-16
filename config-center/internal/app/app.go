// Package app 负责配置中心依赖初始化和优雅退出。
package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"hitall.com/middle-platform/config-center/internal/api"
	"hitall.com/middle-platform/config-center/internal/schema"
	"hitall.com/middle-platform/config-center/internal/settings"
	"hitall.com/middle-platform/config-center/internal/store"
	"hitall.com/middle-platform/config-center/internal/webhook"
	"hitall.com/middle-platform/shared/bootstrap"
	"hitall.com/middle-platform/shared/logging"
	"hitall.com/middle-platform/shared/signature"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// Run 加载指定的 YAML 配置文件，未指定时使用 configs/config.yaml。
func Run(configPath string) error {
	cfg, err := settings.LoadFile(configPath)
	if err != nil {
		return err
	}
	if err := logging.Init("config-center", cfg.Log); err != nil {
		return err
	}
	stopCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	parsed, err := bootstrap.ParseMySQLUTC(cfg.DSN)
	if err != nil {
		return err
	}
	parsed.Timeout = 5 * time.Second
	parsed.ReadTimeout = 10 * time.Second
	parsed.WriteTimeout = 10 * time.Second

	// 启动只读校验数据库结构；建表与升级 SQL 由管理员手动执行。
	migrationCtx, migrationCancel := context.WithTimeout(stopCtx, 3*time.Minute)
	defer migrationCancel()
	if err := schema.Validate(migrationCtx, parsed.FormatDSN()); err != nil {
		return err
	}

	db, err := sql.Open("mysql", parsed.FormatDSN())
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(10 * time.Minute)

	ctx, cancel := context.WithTimeout(stopCtx, 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		return err
	}
	var revision int64
	if err := db.QueryRowContext(ctx, "SELECT revision FROM config_revision WHERE id=1").Scan(&revision); err != nil {
		return err
	}

	address := cfg.Address
	repository := &store.Store{DB: db}
	// 管理接口使用管理密钥；配置同步接口根据接入端编码读取订阅者独立密钥。
	var handler http.Handler = signature.AdminMiddleware(
		api.RouterWithWrites(repository, repository), "/api/config/admin", cfg.AdminSecret,
	)
	handler = signature.RequestSubscriberHMACMiddleware(
		handler,
		[]string{"/api/config/sync", "/api/config/nodes"},
		repository.SubscriberSecret,
	)
	health := bootstrap.NewHealthState()
	health.SetReady(true)
	handler = health.Middleware(handler)
	server := &http.Server{
		Addr:              address,
		Handler:           logging.HTTP(cfg.Log, handler),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       90 * time.Second,
	}
	// 后台 Webhook 投递使用独立生命周期，退出时先取消再等待任务结束。
	workerCtx, cancelWorker := context.WithCancel(context.Background())
	defer cancelWorker()
	workerDone := make(chan struct{})
	// 无需预先查询订阅者；Dispatcher 每轮从数据库领取最新任务，运行中新增订阅者自动生效。
	dispatcher := webhook.NewDispatcher(db)
	go func() {
		defer close(workerDone)
		dispatcher.Run(workerCtx)
	}()

	errCh := make(chan error, 1)
	go func() {
		slog.Info("配置中心已启动", "address", address, "revision", revision)
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
	// 收到终止信号后立即撤销就绪状态，供主动健康检查的网关摘除实例。
	health.SetReady(false)
	stop()
	started := time.Now()
	slog.Info("配置中心正在关闭")
	shutdownCtx, done := context.WithTimeout(context.Background(), bootstrap.HTTPShutdownTimeout)
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
		workerErr = fmt.Errorf("Webhook 工作线程关闭超时: %w", shutdownCtx.Err())
	}
	if err := errors.Join(listenErr, shutdownErr, workerErr); err != nil {
		return err
	}
	slog.Info("配置中心已停止", "duration", time.Since(started))
	return nil
}
