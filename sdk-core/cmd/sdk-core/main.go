package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/hctty27/middle-platform/shared/logging"
	"github.com/hctty27/middle-platform/sdk-core/internal/api"
	"github.com/hctty27/middle-platform/sdk-core/internal/mapping"
	"github.com/hctty27/middle-platform/sdk-core/internal/service"
	"github.com/hctty27/middle-platform/sdk-core/internal/settings"
	"github.com/hctty27/middle-platform/sdk-core/internal/store"
)

func main() {
	configPath := flag.String("config", "configs/config.yaml", "YAML configuration file")
	flag.Parse()
	if err := run(*configPath); err != nil {
		slog.Error("sdk-core stopped", "error", err)
		os.Exit(1)
	}
}

func run(configPath string) error {
	cfg, err := settings.Load(configPath)
	if err != nil {
		return err
	}
	if err := logging.Init("sdk-core", cfg.Log); err != nil { return fmt.Errorf("initialize logging: %w", err) }

	dsn, err := mysql.ParseDSN(cfg.Database.DSN)
	if err != nil {
		return fmt.Errorf("invalid database DSN: %w", err)
	}
	dsn.ParseTime = true
	dsn.Loc = time.UTC
	dsn.Timeout = 5 * time.Second
	dsn.ReadTimeout = 10 * time.Second
	dsn.WriteTimeout = 10 * time.Second

	db, err := sql.Open("mysql", dsn.FormatDSN())
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(cfg.Database.MaxOpenConns)
	db.SetMaxIdleConns(cfg.Database.MaxIdleConns)
	lifetime, _ := time.ParseDuration(cfg.Database.ConnMaxLifetime)
	db.SetConnMaxLifetime(lifetime)

	initCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.PingContext(initCtx); err != nil {
		return fmt.Errorf("connect config center database: %w", err)
	}
	repository := store.NewStore(db)
	if err := repository.VerifySchema(initCtx); err != nil {
		return err
	}

	resolver, err := mapping.LoadDefault()
	if err != nil { return fmt.Errorf("load response mapping: %w", err) }
	if err := resolver.Require(mapping.RouteCoreConfig); err != nil { return err }

	server := &http.Server{
		Addr:              cfg.Server.Address,
		Handler:           logging.HTTP(cfg.Log, api.Router(service.NewConfigService(repository, resolver), db)),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	stopCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	errCh := make(chan error, 1)
	go func() {
		slog.Info("sdk-core started", "address", cfg.Server.Address)
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
