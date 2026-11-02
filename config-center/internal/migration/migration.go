// Package migration 管理 V11 基线初始化和后续增量升级。
package migration

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
)

//go:embed sql/*.sql
var scripts embed.FS

// BaselineVersion 是当前全量初始化脚本所对应的数据库版本。
const BaselineVersion = 11

// Latest 为代码支持的最新数据库版本；后续新增 V12 时同步维护。
const Latest = BaselineVersion

// upgrades 只存放基线之后的增量 SQL，历史 V1～V11 不再逐步回放。
var upgrades = map[int]string{}

// Options 控制独立迁移命令的目标版本。
type Options struct {
	Target int
}

// Run 对空库执行 V11 基线，对已初始化数据库执行后续增量迁移。
func Run(ctx context.Context, dsn string, opts Options) error {
	target := opts.Target
	if target == 0 {
		target = Latest
	}
	if target < BaselineVersion || target > Latest {
		return fmt.Errorf("unsupported schema target %d (supported %d..%d)", target, BaselineVersion, Latest)
	}
	return withConnection(ctx, dsn, true, func(conn *sql.Conn) error {
		if err := lock(ctx, conn); err != nil {
			return err
		}
		return migrate(ctx, conn, target)
	})
}

// Validate 在业务服务启动时只校验数据库，不执行任何 DDL。
func Validate(ctx context.Context, dsn string) error {
	return withConnection(ctx, dsn, false, func(conn *sql.Conn) error {
		version, dirty, err := readVersion(ctx, conn)
		if err != nil {
			return err
		}
		if dirty || version != Latest {
			return fmt.Errorf("database schema version=%d dirty=%t; expected version=%d dirty=false; run cmd/migrate first", version, dirty, Latest)
		}
		return validateSchema(ctx, conn)
	})
}

func withConnection(ctx context.Context, dsn string, multiStatements bool, fn func(*sql.Conn) error) error {
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		return err
	}
	if cfg.DBName == "" {
		return errors.New("database name is required")
	}
	cfg.MultiStatements = multiStatements
	cfg.ParseTime = true
	cfg.Loc = time.UTC
	cfg.Timeout = 5 * time.Second
	cfg.ReadTimeout = 90 * time.Second
	cfg.WriteTimeout = 90 * time.Second
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		return err
	}
	defer db.Close()
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	return fn(conn)
}

func lock(ctx context.Context, conn *sql.Conn) error {
	var database string
	if err := conn.QueryRowContext(ctx, "SELECT DATABASE()").Scan(&database); err != nil {
		return err
	}
	sum := sha256.Sum256([]byte(database))
	name := fmt.Sprintf("config-schema-%x", sum)
	name = name[:48]
	var acquired sql.NullInt64
	if err := conn.QueryRowContext(ctx, "SELECT GET_LOCK(?, 30)", name).Scan(&acquired); err != nil {
		return err
	}
	if !acquired.Valid || acquired.Int64 != 1 {
		return errors.New("failed to acquire schema migration lock")
	}
	// 锁属于当前连接；迁移完成前不会释放连接。
	// 此处通过连接生命周期释放锁，避免在错误路径中误用已取消的 ctx。
	return nil
}

func hasTable(ctx context.Context, conn *sql.Conn, table string) (bool, error) {
	var count int
	err := conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=DATABASE() AND table_name=?", table).Scan(&count)
	return count > 0, err
}

func readVersion(ctx context.Context, conn *sql.Conn) (int, bool, error) {
	exists, err := hasTable(ctx, conn, "config_schema_migrations")
	if err != nil {
		return 0, false, err
	}
	if !exists {
		return 0, false, errors.New("database migration state missing; initialize an empty database with cmd/migrate")
	}
	var version int
	var dirty bool
	if err := conn.QueryRowContext(ctx, "SELECT version,dirty FROM config_schema_migrations WHERE id=1").Scan(&version, &dirty); err != nil {
		return 0, false, fmt.Errorf("read migration state: %w", err)
	}
	return version, dirty, nil
}

func migrate(ctx context.Context, conn *sql.Conn, target int) error {
	hasState, err := hasTable(ctx, conn, "config_schema_migrations")
	if err != nil {
		return err
	}
	if !hasState {
		var tables int
		if err := conn.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=DATABASE()").Scan(&tables); err != nil {
			return err
		}
		if tables != 0 {
			return errors.New("refuse to baseline a non-empty database without migration history; recover its historical migrations first")
		}
		if _, err := conn.ExecContext(ctx, `CREATE TABLE config_schema_migrations (
			id TINYINT NOT NULL PRIMARY KEY,
			version INT NOT NULL,
			dirty BOOLEAN NOT NULL,
			updated_at DATETIME(6) NOT NULL
		) ENGINE=InnoDB COMMENT='配置中心数据库迁移版本记录'`); err != nil {
			return err
		}
		if _, err := conn.ExecContext(ctx,
			"INSERT INTO config_schema_migrations(id,version,dirty,updated_at) VALUES(1,0,0,UTC_TIMESTAMP(6))"); err != nil {
			return err
		}
	}
	version, dirty, err := readVersion(ctx, conn)
	if err != nil {
		return err
	}
	if dirty {
		return fmt.Errorf("schema version=%d is dirty; inspect partial MySQL DDL and restore from backup", version)
	}
	if version > target {
		return fmt.Errorf("database version=%d exceeds requested target=%d", version, target)
	}
	if version > 0 && version < BaselineVersion {
		return fmt.Errorf("schema V%d is older than V%d baseline; use pre-baseline code to upgrade to V%d before deploying", version, BaselineVersion, BaselineVersion)
	}
	if version == 0 {
		body, err := scripts.ReadFile("sql/schema.sql")
		if err != nil {
			return err
		}
		if err := apply(ctx, conn, BaselineVersion, string(body)); err != nil {
			return err
		}
		version = BaselineVersion
	}
	for next := version + 1; next <= target; next++ {
		path, ok := upgrades[next]
		if !ok {
			return fmt.Errorf("missing migration file for V%d", next)
		}
		body, err := scripts.ReadFile(path)
		if err != nil {
			return err
		}
		if err := apply(ctx, conn, next, string(body)); err != nil {
			return err
		}
	}
	return validateSchema(ctx, conn)
}

func apply(ctx context.Context, conn *sql.Conn, version int, sqlBody string) error {
	if _, err := conn.ExecContext(ctx,
		"UPDATE config_schema_migrations SET dirty=1,updated_at=UTC_TIMESTAMP(6) WHERE id=1"); err != nil {
		return err
	}
	slog.Info("applying database schema", "version", version)
	if _, err := conn.ExecContext(ctx, sqlBody); err != nil {
		return fmt.Errorf("apply schema V%d failed (dirty): %w", version, err)
	}
	if _, err := conn.ExecContext(ctx,
		"UPDATE config_schema_migrations SET version=?,dirty=0,updated_at=UTC_TIMESTAMP(6) WHERE id=1", version); err != nil {
		return fmt.Errorf("mark schema V%d completed: %w", version, err)
	}
	return nil
}

func validateSchema(ctx context.Context, conn *sql.Conn) error {
	// 运行态表和关键字段必须真实存在，不能只相信迁移记录。
	statements := []string{
		"SELECT revision,epoch FROM config_revision WHERE id=1",
		"SELECT id,game_id,region_id,env_type,node_type FROM config_node LIMIT 0",
		"SELECT revision,node_id,operation FROM config_node_change_log LIMIT 0",
		"SELECT id,code,webhook_secret FROM config_subscriber LIMIT 0",
		"SELECT acknowledged_revision,acknowledged_epoch FROM config_subscriber_delivery_state LIMIT 0",
		"SELECT id,game_id,code,prod_server_group_id,test_server_group_id,review_server_group_id FROM config_channel_app LIMIT 0",
	}
	for _, statement := range statements {
		rows, err := conn.QueryContext(ctx, statement)
		if err != nil {
			return fmt.Errorf("database schema validation failed: %w", err)
		}
		if err := rows.Close(); err != nil {
			return err
		}
	}
	for _, obsolete := range []string{"config_channel_app_binding", "config_change_log", "config_delivery_state"} {
		exists, err := hasTable(ctx, conn, obsolete)
		if err != nil {
			return err
		}
		if exists {
			return fmt.Errorf("obsolete table %s still exists", obsolete)
		}
	}
	var indexCount int
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.statistics
		WHERE table_schema=DATABASE() AND table_name='config_channel_app'
		AND index_name='uq_channel_app_code' AND column_name='code'
		AND seq_in_index=1 AND non_unique=0`).Scan(&indexCount); err != nil {
		return err
	}
	if indexCount != 1 {
		return errors.New("config_channel_app global code unique index missing")
	}
	for _, constraint := range []string{"chk_config_node_env_type", "fk_app_prod_group", "fk_app_test_group", "fk_app_review_group"} {
		var count int
		if err := conn.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM information_schema.table_constraints WHERE constraint_schema=DATABASE() AND constraint_name=?", constraint).Scan(&count); err != nil {
			return err
		}
		if count == 0 {
			return fmt.Errorf("missing schema constraint %s", constraint)
		}
	}
	var clause string
	if err := conn.QueryRowContext(ctx, `SELECT check_clause FROM information_schema.check_constraints
		WHERE constraint_schema=DATABASE() AND constraint_name='chk_config_node_env_type'`).Scan(&clause); err != nil {
		return err
	}
	if strings.Contains(clause, "production") || !strings.Contains(clause, "prod") {
		return errors.New("region environment CHECK constraint is obsolete")
	}
	return nil
}
