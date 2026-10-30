// 数据库迁移独立命令：不依赖 Java、Gradle 或 Flyway。
package main

import (
 "context"
 "flag"
 "log/slog"
 "os"
 "time"

 "github.com/hctty27/middle-platform/config-center/internal/migration"
)

// main 解析迁移目标版本和旧库升级开关，独立运行 Go 数据库迁移。
func main() {
 target := flag.Int("target", migration.Latest, "schema version to migrate to (1..4)")
 // 旧版结构迁移可能改变数据布局，必须显式开启，不能默认用于生产库。
 allowLegacy := flag.Bool("allow-legacy-upgrade", false, "permit upgrading pre-V4 legacy databases")
 flag.Parse()
 dsn := os.Getenv("CONFIG_DB_DSN")
 if dsn == "" {
  slog.Error("CONFIG_DB_DSN is required")
  os.Exit(1)
 }
 ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
 defer cancel()
 if err := migration.Run(ctx, dsn, migration.Options{
  Target: *target, AllowLegacyUpgrade: *allowLegacy,
 }); err != nil {
  slog.Error("schema migration failed", "error", err)
  os.Exit(1)
 }
 slog.Info("schema migration complete", "target", *target)
}
