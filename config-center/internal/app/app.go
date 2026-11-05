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
	"github.com/hctty27/middle-platform/shared/bootstrap"
    "github.com/hctty27/middle-platform/shared/logging"
	"github.com/hctty27/middle-platform/shared/signature"
	"github.com/hctty27/middle-platform/config-center/internal/api"
	"github.com/hctty27/middle-platform/config-center/internal/schema"
	"github.com/hctty27/middle-platform/config-center/internal/settings"
	"github.com/hctty27/middle-platform/config-center/internal/store"
	"github.com/hctty27/middle-platform/config-center/internal/webhook"
)

// Run 加载指定的 YAML 配置文件，未指定时使用 configs/config.yaml。
func Run(configPath string) error {
    cfg, err := settings.LoadFile(configPath)
	if err != nil { return err }
	if err := logging.Init("config-center", cfg.Log); err != nil { return err }
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

	// 启动只读校验数据库结构；建表与升级 SQL 由管理员手动执行。
	migrationCtx, migrationCancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer migrationCancel()
	if err := schema.Validate(migrationCtx, parsed.FormatDSN()); err != nil { return err }

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
	repository := &store.Store{DB: db}
	// 管理读写接口始终注册，内建管理验签，并由网关限制外部可访问范围。
	var handler http.Handler = signature.AdminMiddleware(api.RouterWithWrites(repository, repository), "/api/config/admin", cfg.AdminSecret, cfg.AdminAuthEnabled)
	var ready atomic.Bool
	ready.Store(true)
	handler = bootstrap.ReadinessMiddleware(handler, &ready)
	server := &http.Server{
		Addr:              address,
		Handler:           logging.HTTP(cfg.Log, handler),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       90 * time.Second,
	}
	stopCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

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
	// 收到终止信号后立即撤销就绪状态，使网关停止分发新请求。
	ready.Store(false)
	stop()
	started := time.Now()
	slog.Info("配置中心正在关闭")
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
		workerErr = fmt.Errorf("Webhook 工作线程关闭超时: %w", shutdownCtx.Err())
	}
	if err := errors.Join(listenErr, shutdownErr, workerErr); err != nil {
		return err
	}
	slog.Info("配置中心已停止", "duration", time.Since(started))
	return nil
}

