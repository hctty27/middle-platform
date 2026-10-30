// 配置中心接入示例：仅同步本地副本，不提供 Starter SDK。
package main

import (
	"context"
	"database/sql"
	"errors"
    "flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hctty27/middle-platform/config-center-access/internal/api"
	"github.com/hctty27/middle-platform/config-center-access/internal/remote"
	"github.com/hctty27/middle-platform/config-center-access/internal/settings"
	"github.com/hctty27/middle-platform/config-center-access/internal/store"
	"github.com/hctty27/middle-platform/config-center-access/internal/syncer"
	_ "github.com/go-sql-driver/mysql"
)

func main() {
    configPath := flag.String("config", "", "YAML configuration file")
    flag.Parse()
    if err := run(*configPath); err != nil {
		slog.Error("config access stopped", "error", err)
		os.Exit(1)
	}
}

func run(configPath string) error {
    cfg, err := settings.LoadFile(configPath)
	if err != nil {
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
	if err := replica.EnsureSchema(startupCtx); err != nil {
		return err
	}
	center := remote.New(cfg.CenterURL, cfg.ConnectTimeout, cfg.RequestTimeout)
	sync := syncer.New(replica, center)
	trigger := syncer.NewTrigger(sync, cfg.ReconcileInterval)
	go trigger.Run(ctx)

	server := &http.Server{
		Addr: cfg.Address,
		Handler: api.Router(replica, trigger, cfg.WebhookSecret),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			slog.Error("HTTP shutdown failed", "error", err)
		}
	}()
	slog.Info("config access started", "address", cfg.Address, "center", cfg.CenterURL)
	if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
