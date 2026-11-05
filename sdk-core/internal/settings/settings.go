// Package settings 加载 SDK Core 的 YAML 配置和环境变量。
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

// Database 保存 config_center 单连接池参数。
type Database struct {
    DSN string `yaml:"dsn"`
    MaxOpenConns int `yaml:"maxOpenConns"`
    MaxIdleConns int `yaml:"maxIdleConns"`
    ConnMaxLifetime string `yaml:"connMaxLifetime"`
}

// Config 保存 SDK Core 的监听、数据库和日志配置。
type Config struct {
    Log logging.Config `yaml:"log"`
    Server struct {Address string `yaml:"address"`} `yaml:"server"`
    Database Database `yaml:"database"`
    AdminAuth struct {
        Enabled *bool `yaml:"enabled"`
        Secret string `yaml:"secret"`
    } `yaml:"adminAuth"`
}

func defaultDB() Database{
    return Database{MaxOpenConns:20,MaxIdleConns:10,ConnMaxLifetime:"30m"}
}
func validateDB(d Database)error {
    if d.MaxOpenConns<1||d.MaxIdleConns<0||d.MaxIdleConns>d.MaxOpenConns {
        return errors.New("数据库连接池配置不合法")
    }
    if _,err:=time.ParseDuration(d.ConnMaxLifetime);err!=nil{
        return fmt.Errorf("数据库 connMaxLifetime 配置不合法: %w",err)
    }
    return nil
}
// Load 严格解析单份 YAML，并允许环境变量覆盖连接和监听地址。
func Load(path string)(Config,error){
    cfg:=Config{}
    cfg.Log=logging.Default()
    cfg.Server.Address=":8082"
    cfg.Database=defaultDB()
    if path==""{path="configs/config.yaml"}
    if err:=bootstrap.ReadYAML(path,&cfg);err!=nil{return Config{},err}
    if v,ok:=os.LookupEnv("SDK_CORE_DB_DSN");ok{cfg.Database.DSN=v}
    if v,ok:=os.LookupEnv("SDK_CORE_ADDR");ok{cfg.Server.Address=v}
    if v,ok:=os.LookupEnv("ADMIN_AUTH_SECRET");ok{cfg.AdminAuth.Secret=v}
    if err=cfg.Log.ApplyEnv();err!=nil{return Config{},err}
    if strings.TrimSpace(cfg.Database.DSN)==""{
        return Config{},errors.New("必须配置 database.dsn 或 SDK_CORE_DB_DSN")
    }
    if strings.TrimSpace(cfg.Server.Address)==""{
        return Config{},errors.New("必须配置 server.address")
    }
    if err=validateDB(cfg.Database);err!=nil{return Config{},err}
    return cfg,nil
}
