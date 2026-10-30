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
	"sync/atomic"
	"syscall"
	"time"

	"github.com/go-sql-driver/mysql"
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

	var ready atomic.Bool
	ready.Store(true)
	server := &http.Server{
		Addr:              cfg.Server.Address,
		Handler:           withDrainReadiness(api.Router(service.NewConfigService(repository, resolver), db), &ready),
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
	slog.Info("sdk-core shutting down")
	shutdownCtx, done := context.WithTimeout(context.Background(), 30*time.Second)
	defer done()
	shutdownErr := server.Shutdown(shutdownCtx)
	if shutdownErr != nil {
		slog.Warn("HTTP graceful shutdown failed; closing active connections", "error", shutdownErr)
		shutdownErr = errors.Join(shutdownErr, server.Close())
	}
	if err := errors.Join(listenErr, shutdownErr); err != nil {
		return err
	}
	slog.Info("sdk-core stopped", "duration", time.Since(started))
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
