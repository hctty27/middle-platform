// Package app 负责 SDK Admin 服务的初始化和生命周期。
package app

import (
 "context"
 "errors"
 "fmt"
 "log/slog"
 "net/http"
 "os"
 "os/signal"
 "syscall"
 "time"

 "hitall.com/middle-platform/sdk-admin/internal/api"
 "hitall.com/middle-platform/sdk-admin/internal/settings"
 "hitall.com/middle-platform/shared/bootstrap"
 "hitall.com/middle-platform/shared/logging"
)

func Run(configPath string) error {
 cfg, err := settings.Load(configPath)
 if err != nil { return err }
 if err := logging.Init("sdk-admin", cfg.Log); err != nil { return fmt.Errorf("初始化日志失败: %w", err) }
 stopCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
 defer stop()
 db, err := bootstrap.OpenMySQL(bootstrap.MySQLConfig{
  DSN: cfg.Database.DSN, MaxOpenConns: cfg.Database.MaxOpenConns,
  MaxIdleConns: cfg.Database.MaxIdleConns, ConnMaxLifetime: "30m",
 })
 if err != nil { return err }
 defer db.Close()
 initCtx, cancel := context.WithTimeout(stopCtx, 10*time.Second)
 defer cancel()
 if err := db.PingContext(initCtx); err != nil { return fmt.Errorf("连接配置中心数据库失败: %w", err) }
 health := bootstrap.NewHealthState()
 health.SetReady(true)
 server := &http.Server{
  Addr: cfg.Server.Address,
  Handler: logging.HTTP(cfg.Log, health.Middleware(api.Router(db, cfg.AdminAuth.Secret))),
  ReadHeaderTimeout: 5*time.Second, ReadTimeout: 10*time.Second,
  WriteTimeout: 15*time.Second, IdleTimeout: 60*time.Second,
 }
 errCh := make(chan error, 1)
 go func() {
  slog.Info("SDK Admin 已启动", "address", cfg.Server.Address)
  errCh <- server.ListenAndServe()
 }()
 var listenErr error
 select {
 case <-stopCtx.Done():
 case err := <-errCh:
  if !errors.Is(err,http.ErrServerClosed) { listenErr = err }
 }
 health.SetReady(false)
 stop()
 slog.Info("SDK Admin 正在关闭")
 shutdownCtx, done := context.WithTimeout(context.Background(), bootstrap.HTTPShutdownTimeout)
 defer done()
 shutdownErr := server.Shutdown(shutdownCtx)
 if shutdownErr != nil {
  slog.Warn("HTTP 优雅关闭失败，正在关闭活动连接", "error", shutdownErr)
  shutdownErr = errors.Join(shutdownErr, server.Close())
 }
 if err := errors.Join(listenErr, shutdownErr); err != nil { return err }
 slog.Info("SDK Admin 已停止")
 return nil
}
