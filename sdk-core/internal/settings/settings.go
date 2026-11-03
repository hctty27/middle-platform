// Package settings 加载 SDK Core 的 YAML 配置和环境变量。
package settings

import (
    "bytes"
    "errors"
    "fmt"
    "io"
    "os"
    "strings"
    "time"

    "gopkg.in/yaml.v3"
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
    raw,err:=os.ReadFile(path)
    if err!=nil{return Config{},fmt.Errorf("读取配置文件 %s 失败: %w",path,err)}
    d:=yaml.NewDecoder(bytes.NewReader(raw))
    d.KnownFields(true)
    if err=d.Decode(&cfg);err!=nil{return Config{},fmt.Errorf("解析配置文件失败: %w",err)}
    var extra any
    if err=d.Decode(&extra);!errors.Is(err,io.EOF){
        return Config{},errors.New("配置文件必须只包含一份 YAML 文档")
    }
    if v,ok:=os.LookupEnv("SDK_CORE_DB_DSN");ok{cfg.Database.DSN=v}
    if v,ok:=os.LookupEnv("SDK_CORE_ADDR");ok{cfg.Server.Address=v}
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
