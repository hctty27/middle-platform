package bootstrap

import (
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
)

func TestParseMySQLUTC(t *testing.T) {
	for _, dsn := range []string{
		"user:pass@tcp(127.0.0.1:3306)/db",
		"user:pass@tcp(127.0.0.1:3306)/db?loc=Local&parseTime=false&time_zone=%27%2B08%3A00%27&timeout=2s&readTimeout=3s&writeTimeout=4s&charset=utf8mb4&sql_mode=%27STRICT_TRANS_TABLES%27",
	} {
		t.Run(dsn, func(t *testing.T) {
			original, err := mysql.ParseDSN(dsn)
			if err != nil {
				t.Fatal(err)
			}
			got, err := ParseMySQLUTC(dsn)
			if err != nil {
				t.Fatal(err)
			}
			for _, cfg := range []*mysql.Config{got, mustParseDSN(t, got.FormatDSN())} {
				if !cfg.ParseTime || cfg.Loc != time.UTC || cfg.Params["time_zone"] != "'+00:00'" {
					t.Fatalf("UTC settings missing: parseTime=%v loc=%v params=%v", cfg.ParseTime, cfg.Loc, cfg.Params)
				}
				if cfg.Timeout != original.Timeout || cfg.ReadTimeout != original.ReadTimeout || cfg.WriteTimeout != original.WriteTimeout {
					t.Fatal("DSN timeouts changed")
				}
				if cfg.DBName != original.DBName || cfg.User != original.User || cfg.Passwd != original.Passwd || cfg.Addr != original.Addr {
					t.Fatal("DSN connection target changed")
				}
				for key, value := range original.Params {
					if key != "time_zone" && cfg.Params[key] != value {
						t.Fatalf("DSN parameter %s changed", key)
					}
				}
			}
		})
	}
	if _, err := ParseMySQLUTC("user:pass@tcp(127.0.0.1:3306/db"); err == nil {
		t.Fatal("invalid DSN accepted")
	}
}

func mustParseDSN(t *testing.T, dsn string) *mysql.Config {
	t.Helper()
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestOpenMySQLConfig(t *testing.T) {
	c := MySQLConfig{DSN: "user:pass@tcp(127.0.0.1:3306)/db", MaxOpenConns: 20, MaxIdleConns: 10, ConnMaxLifetime: "30m"}
	db, err := OpenMySQL(c)
	if err != nil {
		t.Fatal(err)
	}
	if db.Stats().MaxOpenConnections != 20 {
		t.Fatal(db.Stats())
	}
	_ = db.Close()
	c.MaxIdleConns = 21
	if _, err := OpenMySQL(c); err == nil {
		t.Fatal("bad pool limits accepted")
	}
	c.MaxIdleConns = 10
	c.ConnMaxLifetime = "not-a-duration"
	if _, err := OpenMySQL(c); err == nil {
		t.Fatal("bad duration accepted")
	}
}
