// 数据库迁移独立命令：不依赖 Java、Gradle 或 Flyway。
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

// main 解析目标版本和安全升级开关。
func main() {
 target := flag.Int("target", migration.Latest, "schema version to migrate to (1..6)")
 // 旧版结构升级必须显式开启。
 allowLegacy := flag.Bool("allow-legacy-upgrade", false, "permit upgrading pre-V4 legacy databases")
 repairV6 := flag.Bool("repair-v6-gtid", false, "repair V6 GTID error 1786 after verifying the interrupted migration is safe to reset (back up first)")
 configPath := flag.String("config", "", "YAML configuration file")
 flag.Parse()
 cfg, err := settings.LoadFile(*configPath)
 if err != nil { slog.Error("invalid config", "error", err); os.Exit(1) }
 dsn := cfg.DSN
 ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
 defer cancel()
 if err := migration.Run(ctx, dsn, migration.Options{
  Target: *target, AllowLegacyUpgrade: *allowLegacy || cfg.AllowLegacyUpgrade,
  RepairV6GTID: *repairV6,
 }); err != nil {
  slog.Error("schema migration failed", "error", err)
  os.Exit(1)
 }
 slog.Info("schema migration complete", "target", *target)
}
