// 数据库结构初始化与增量升级命令，必须先于业务进程执行。
package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"time"

	"github.com/hctty27/middle-platform/config-center/internal/migration"
	"github.com/hctty27/middle-platform/config-center/internal/settings"
)

func main() {
	target := flag.Int("target", migration.Latest, "schema version to migrate to (currently 11)")
	configPath := flag.String("config", "", "YAML configuration file")
	flag.Parse()

	cfg, err := settings.LoadFile(*configPath)
	if err != nil {
		slog.Error("invalid config", "error", err)
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if err := migration.Run(ctx, cfg.DSN, migration.Options{Target: *target}); err != nil {
		slog.Error("schema migration failed", "error", err)
		os.Exit(1)
	}
	slog.Info("schema migration complete", "target", *target)
}
