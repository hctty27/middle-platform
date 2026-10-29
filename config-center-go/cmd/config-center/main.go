package main

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

	"github.com/hctty27/middle-platform/config-center-go/internal/config"
)

func main() {
	if err := run(); err != nil {
		slog.Error("config-center-go stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	dsn := os.Getenv("CONFIG_DB_DSN")
	if dsn == "" {
		return errors.New("CONFIG_DB_DSN is required")
	}
	parsed, err := mysql.ParseDSN(dsn)
	if err != nil {
		return err
	}
	parsed.ParseTime = true
	parsed.Loc = time.UTC
	parsed.Timeout = 5 * time.Second
	parsed.ReadTimeout = 10 * time.Second
	parsed.WriteTimeout = 10 * time.Second

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

	address := os.Getenv("CONFIG_GO_ADDR")
	if address == "" {
		address = ":8081"
	}
	server := &http.Server{
		Addr:              address,
		Handler:           config.Router(&config.Store{DB: db}),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       90 * time.Second,
	}
	stopCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	errCh := make(chan error, 1)
	go func() {
		slog.Info("read-only Go config center started", "address", address, "revision", revision)
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
