// Package settings 严格解析 SDK Service 配置。
package settings

import (
    "errors"
    "fmt"
    "os"
    "strings"
    "time"

    "github.com/hctty27/middle-platform/shared/bootstrap"
 "github.com/hctty27/middle-platform/shared/logging"
)

type Database struct {
    DSN string `yaml:"dsn"`
    MaxOpenConns int `yaml:"maxOpenConns"`
    MaxIdleConns int `yaml:"maxIdleConns"`
    ConnMaxLifetime string `yaml:"connMaxLifetime"`
}

type Config struct {
    Server struct { Address string `yaml:"address"` } `yaml:"server"`
    Database Database `yaml:"database"`
    Game struct { Timeout string `yaml:"timeout"` } `yaml:"game"`
    Platform struct {
        EventReportURL string `yaml:"eventReportURL"`
        SubscribeMessageURL string `yaml:"subscribeMessageURL"`
    } `yaml:"platform"`
    Log logging.Config `yaml:"log"`
}

func Load(path string) (Config, error) {
    cfg := Config{}
    cfg.Server.Address = ":8221"
    cfg.Database = Database{MaxOpenConns: 10, MaxIdleConns: 5, ConnMaxLifetime: "30m"}
    cfg.Game.Timeout = "5s"
    cfg.Log = logging.Default()
    if path == "" { path = "configs/config.yaml" }
    if err:=bootstrap.ReadYAML(path,&cfg);err!=nil{return Config{},err}
    if value, ok := os.LookupEnv("SDK_SERVICE_DB_DSN"); ok { cfg.Database.DSN = value }
    if value, ok := os.LookupEnv("SDK_SERVICE_ADDR"); ok { cfg.Server.Address = value }
    if err := cfg.Log.ApplyEnv(); err != nil { return Config{}, err }
    if strings.TrimSpace(cfg.Database.DSN) == "" { return Config{}, errors.New("必须配置 database.dsn") }
    if strings.TrimSpace(cfg.Server.Address) == "" { return Config{}, errors.New("server.address 不能为空") }
    if cfg.Database.MaxOpenConns < 1 || cfg.Database.MaxIdleConns < 0 ||
        cfg.Database.MaxIdleConns > cfg.Database.MaxOpenConns {
        return Config{}, errors.New("数据库连接池配置不合法")
    }
    if d, err := time.ParseDuration(cfg.Database.ConnMaxLifetime); err != nil || d <= 0 {
        return Config{}, errors.New("数据库连接生存时间不合法")
    }
    if d, err := time.ParseDuration(cfg.Game.Timeout); err != nil || d <= 0 || d > 30*time.Second {
        return Config{}, errors.New("game.timeout 必须在 0 到 30 秒之间")
    }
    return cfg, nil
}
