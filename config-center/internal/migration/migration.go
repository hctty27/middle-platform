// Package migration 在 Go 进程中执行配置中心的增量数据库迁移。
package migration

import (
 "context"
 "crypto/sha256"
 "database/sql"
 "embed"
 "errors"
 "fmt"
 "log/slog"
 "time"

 "github.com/go-sql-driver/mysql"
)

//go:embed sql/*.sql
var scripts embed.FS

// Latest 是当前内置的最新数据库结构版本，新增迁移时需同步更新文件列表。
const Latest = 5

var files = [...]string{
 "",
 "sql/V1__config_schema.sql",
 "sql/V2__ancestor_ids.sql",
 "sql/V3__subscriber_epoch_cursor.sql",
 "sql/V4__channel_app_hierarchy.sql",
 "sql/V5__region_env_type.sql",
}

// Options 控制迁移终点和旧版数据库升级。生产默认不自动执行破坏性旧库升级。
type Options struct {
 Target int
 AllowLegacyUpgrade bool
}

// Run 使用 MySQL 命名锁协调多实例，执行完后立即关闭独立迁移连接。
func Run(ctx context.Context, dsn string, opts Options) error {
 if opts.Target == 0 { opts.Target = Latest }
 if opts.Target < 1 || opts.Target > Latest { return fmt.Errorf("invalid schema target %d", opts.Target) }
 cfg, err := mysql.ParseDSN(dsn)
 if err != nil { return err }
 if cfg.DBName == "" { return errors.New("database name is required for migration") }
 cfg.MultiStatements = true
 cfg.ParseTime = true
 cfg.Loc = time.UTC
 cfg.Timeout = 5*time.Second
 cfg.ReadTimeout = 90*time.Second
 cfg.WriteTimeout = 90*time.Second
 db, err := sql.Open("mysql", cfg.FormatDSN())
 if err != nil { return err }
 defer db.Close()
 conn, err := db.Conn(ctx)
 if err != nil { return err }
 defer conn.Close()

 // 锁名采用数据库名摘要，避免超过 MySQL 64 字节的限制。
 name := fmt.Sprintf("config-schema-%x", sha256.Sum256([]byte(cfg.DBName)))
 name = name[:48]
 var got sql.NullInt64
 if err := conn.QueryRowContext(ctx, "SELECT GET_LOCK(?, 30)", name).Scan(&got); err != nil { return err }
 if !got.Valid || got.Int64 != 1 { return errors.New("failed to acquire schema migration lock") }
 defer func() {
  release, cancel := context.WithTimeout(context.Background(), 5*time.Second)
  defer cancel()
  if _, err := conn.ExecContext(release, "SELECT RELEASE_LOCK(?)", name); err != nil {
   slog.Warn("schema migration lock release failed", "error", err)
  }
 }()
 return migrate(ctx, conn, opts)
}

// exists 检查指定表是否存在，用于判定全新安装、Go 历史或旧版 Flyway 历史。
func exists(ctx context.Context, conn *sql.Conn, table string) (bool, error) {
 var count int
 err := conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=DATABASE() AND table_name=?", table).Scan(&count)
 return count != 0, err
}

// migrate 按历史版本递进升级；无法确认的旧库拒绝自动改写。
func migrate(ctx context.Context, conn *sql.Conn, opts Options) error {
 hasState, err := exists(ctx, conn, "config_schema_migrations")
 if err != nil { return err }
 version := 0
 if hasState {
  var dirty bool
  err = conn.QueryRowContext(ctx, "SELECT version,dirty FROM config_schema_migrations WHERE id=1").Scan(&version, &dirty)
  if err != nil { return fmt.Errorf("read Go schema state: %w", err) }
  if dirty { return errors.New("database schema is marked dirty; inspect and recover the partial MySQL DDL before retrying") }
 } else {
  legacy, err := exists(ctx, conn, "flyway_schema_history")
  if err != nil { return err }
  if legacy {
   var failures int
   if err := conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM flyway_schema_history WHERE success=0").Scan(&failures); err != nil { return err }
   if failures != 0 { return errors.New("legacy migration history contains failed entries; recover before switching to Go") }
   var last sql.NullInt64
   if err := conn.QueryRowContext(ctx, "SELECT MAX(CAST(version AS UNSIGNED)) FROM flyway_schema_history WHERE success=1").Scan(&last); err != nil { return err }
   if !last.Valid || last.Int64 < 1 || last.Int64 > Latest { return fmt.Errorf("unsupported legacy schema version: %v", last) }
   version = int(last.Int64)
   if version < 4 && !opts.AllowLegacyUpgrade {
    return fmt.Errorf("legacy schema V%d needs V4/V5; back up, stop old writers and run migration with -allow-legacy-upgrade", version)
   }
  } else {
   hasNode, err := exists(ctx, conn, "config_node")
   if err != nil { return err }
   if hasNode { return errors.New("existing config_node has no known schema history; refuse automatic migration") }
  }
  if _, err := conn.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS config_schema_migrations (
   id TINYINT NOT NULL PRIMARY KEY,
   version INT NOT NULL,
   dirty BOOLEAN NOT NULL,
   updated_at DATETIME(6) NOT NULL
  ) ENGINE=InnoDB`); err != nil { return err }
  if _, err := conn.ExecContext(ctx, "INSERT INTO config_schema_migrations(id,version,dirty,updated_at) VALUES(1,?,?,UTC_TIMESTAMP(6))", version, false); err != nil { return err }
 }
 if version > opts.Target { return fmt.Errorf("schema version %d exceeds requested target %d", version, opts.Target) }
 if version == opts.Target { return validate(ctx, conn, version) }
 for next := version+1; next <= opts.Target; next++ {
  body, err := scripts.ReadFile(files[next])
  if err != nil { return err }
  // MySQL DDL 会隐式提交，失败时保留 dirty 标志，阻止再次自动执行半成品脚本。
  if _, err := conn.ExecContext(ctx, "UPDATE config_schema_migrations SET dirty=1,updated_at=UTC_TIMESTAMP(6) WHERE id=1"); err != nil { return err }
  slog.Info("applying config schema migration", "version", next)
  if _, err := conn.ExecContext(ctx, string(body)); err != nil {
   return fmt.Errorf("apply config schema V%d failed (dirty): %w", next, err)
  }
  if _, err := conn.ExecContext(ctx, "UPDATE config_schema_migrations SET version=?,dirty=0,updated_at=UTC_TIMESTAMP(6) WHERE id=1", next); err != nil {
   return fmt.Errorf("mark config schema V%d completed: %w", next, err)
  }
 }
 return validate(ctx, conn, opts.Target)
}

// validate 用真实表结构检查替代仅相信迁移历史记录，防止部分 DDL 遗漏。
func validate(ctx context.Context, conn *sql.Conn, version int) error {
 if version < Latest { return nil } // 仅迁移测试用的阶段性旧版本
 var rev int64
 if err := conn.QueryRowContext(ctx, "SELECT revision FROM config_revision WHERE id=1").Scan(&rev); err != nil {
  return fmt.Errorf("invalid schema revision singleton: %w", err)
 }
 // 对从旧版历史导入的数据库进行真实结构验证，避免仅凭历史记录跳过缺失 DDL。
 for _, statement := range []string{
  "SELECT game_id,region_id,server_group_id,env_type FROM config_node LIMIT 0",
  "SELECT acknowledged_epoch FROM config_delivery_state LIMIT 0",
  "SELECT id FROM config_node_v4_legacy_archive LIMIT 0",
 } {
  rows, err := conn.QueryContext(ctx, statement)
  if err != nil { return fmt.Errorf("schema V5 validation failed: %w", err) }
  if err := rows.Close(); err != nil { return err }
 }
 return nil
}
