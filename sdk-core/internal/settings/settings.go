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
    "github.com/hctty27/middle-platform/sdk-core/internal/auth"
)

type Database struct {
    DSN string `yaml:"dsn"`
    MaxOpenConns int `yaml:"maxOpenConns"`
    MaxIdleConns int `yaml:"maxIdleConns"`
    ConnMaxLifetime string `yaml:"connMaxLifetime"`
}

type Config struct {
    Log logging.Config `yaml:"log"`
    Server struct {Address string `yaml:"address"`} `yaml:"server"`
    Database Database `yaml:"database"`
    OrderDatabase Database `yaml:"orderDatabase"`
    GameAuth struct{ Keys map[string]auth.Credential `yaml:"keys"`} `yaml:"gameAuth"`
    Payment struct {
        Secrets map[string]map[string]string `yaml:"secrets"`
        AllowUnverifiedNonProd bool `yaml:"allowUnverifiedNonProd"`
    } `yaml:"payment"`
    GameNotify struct {
        AllowedHosts []string `yaml:"allowedHosts"`
        Secrets map[string]string `yaml:"secrets"`
    } `yaml:"gameNotify"`
}

func defaultDB() Database{
    return Database{MaxOpenConns:20,MaxIdleConns:10,ConnMaxLifetime:"30m"}
}
func validateDB(d Database)error {
    if d.MaxOpenConns<1||d.MaxIdleConns<0||d.MaxIdleConns>d.MaxOpenConns {
        return errors.New("invalid database pool configuration")
    }
    if _,err:=time.ParseDuration(d.ConnMaxLifetime);err!=nil{
        return fmt.Errorf("invalid database connMaxLifetime: %w",err)
    }
    return nil
}
func Load(path string)(Config,error){
    cfg:=Config{}
    cfg.Log=logging.Default()
    cfg.Server.Address=":8082"
    cfg.Database=defaultDB()
    cfg.OrderDatabase=defaultDB()
    if path==""{path="configs/config.yaml"}
    raw,err:=os.ReadFile(path)
    if err!=nil{return Config{},fmt.Errorf("read configuration %s: %w",path,err)}
    d:=yaml.NewDecoder(bytes.NewReader(raw))
    d.KnownFields(true)
    if err=d.Decode(&cfg);err!=nil{return Config{},fmt.Errorf("decode configuration: %w",err)}
    var extra any
    if err=d.Decode(&extra);!errors.Is(err,io.EOF){
        return Config{},errors.New("configuration must contain exactly one YAML document")
    }
    if v,ok:=os.LookupEnv("SDK_CORE_DB_DSN");ok{cfg.Database.DSN=v}
    if v,ok:=os.LookupEnv("SDK_CORE_ORDER_DB_DSN");ok{cfg.OrderDatabase.DSN=v}
    if v,ok:=os.LookupEnv("SDK_CORE_ADDR");ok{cfg.Server.Address=v}
    if err=cfg.Log.ApplyEnv();err!=nil{return Config{},err}
    if strings.TrimSpace(cfg.Database.DSN)==""{
        return Config{},errors.New("database.dsn or SDK_CORE_DB_DSN is required")
    }
    if strings.TrimSpace(cfg.Server.Address)==""{
        return Config{},errors.New("server.address is required")
    }
    if err=validateDB(cfg.Database);err!=nil{return Config{},err}
    if err=validateDB(cfg.OrderDatabase);err!=nil{return Config{},err}
    return cfg,nil
}
