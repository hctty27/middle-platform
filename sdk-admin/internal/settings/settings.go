// Package settings 加载 SDK Admin 的单文件 YAML 配置。
package settings

import (
 "errors"
 "fmt"
 "os"
 "strings"

 "hitall.com/middle-platform/shared/bootstrap"
 "hitall.com/middle-platform/shared/logging"
)

type Config struct {
 Log logging.Config `yaml:"log"`
 Server struct { Address string `yaml:"address"` } `yaml:"server"`
 Database struct {
  DSN string `yaml:"dsn"`
  MaxOpenConns int `yaml:"maxOpenConns"`
  MaxIdleConns int `yaml:"maxIdleConns"`
 } `yaml:"database"`
 AdminAuth struct { Secret string `yaml:"secret"` } `yaml:"adminAuth"`
}

func Load(path string) (Config,error) {
 cfg := Config{}
 cfg.Log = logging.Default()
 cfg.Log.AccessEnabled = false
 cfg.Server.Address = ":8231"
 cfg.Database.MaxOpenConns = 10
 cfg.Database.MaxIdleConns = 5
 if path == "" { path="configs/config.yaml" }
 if err := bootstrap.ReadYAML(path,&cfg); err != nil { return Config{},err }
 bootstrap.OverrideString("SDK_ADMIN_ADDR", &cfg.Server.Address)
 bootstrap.OverrideString("SDK_ADMIN_DB_DSN", &cfg.Database.DSN)
 bootstrap.OverrideString("ADMIN_AUTH_SECRET", &cfg.AdminAuth.Secret)
 if err := cfg.Log.ApplyEnv(); err != nil { return Config{},err }
 if strings.TrimSpace(cfg.Database.DSN)=="" { return Config{},errors.New("必须配置 database.dsn 或 SDK_ADMIN_DB_DSN") }
 if strings.TrimSpace(cfg.Server.Address)=="" { return Config{},errors.New("必须配置 server.address") }
 if cfg.Database.MaxOpenConns<1 || cfg.Database.MaxIdleConns<0 || cfg.Database.MaxIdleConns>cfg.Database.MaxOpenConns {
  return Config{},errors.New("sdk-admin 数据库连接池配置不合法")
 }
 if len([]byte(cfg.AdminAuth.Secret))<32 { return Config{},fmt.Errorf("sdk-admin adminAuth.secret 至少需要 32 字节") }
 return cfg,nil
}
