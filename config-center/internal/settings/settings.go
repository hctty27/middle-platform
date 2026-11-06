package settings

import (
 "errors"
 "strings"

 "github.com/hctty27/middle-platform/shared/bootstrap"
 "github.com/hctty27/middle-platform/shared/logging"
)

// Settings 保存配置中心运行参数。
type Settings struct {
 DSN string
 Address string
 Log logging.Config
 AdminSecret string
}

type configFile struct {
 Server struct { Address string `yaml:"address"` } `yaml:"server"`
 Database struct { DSN string `yaml:"dsn"` } `yaml:"database"`
 Log logging.Config `yaml:"log"`
 AdminAuth struct {
  Secret string `yaml:"secret"`
 } `yaml:"adminAuth"`
}

// Load 使用默认 YAML 文件启动。
func Load() (Settings,error) { return LoadFile("") }

// LoadFile 只读取指定 YAML（空路径使用 configs/config.yaml），再应用环境变量覆盖。
func LoadFile(path string) (Settings,error) {
 data:=configFile{Log:logging.Default()}
 if path=="" { path="configs/config.yaml" }
 if err:=bootstrap.ReadYAML(path,&data);err!=nil{return Settings{},err}

 cfg:=Settings{Address:"127.0.0.1:8080",DSN:data.Database.DSN,Log:data.Log,AdminSecret:data.AdminAuth.Secret}
 if data.Server.Address!="" {cfg.Address=data.Server.Address}
 // 环境变量优先级最高；管理 API 内建签名验签，网关仍负责限制访问范围。
 bootstrap.OverrideString("CONFIG_DB_DSN",&cfg.DSN)
 bootstrap.OverrideString("CONFIG_GO_ADDR",&cfg.Address)
 bootstrap.OverrideString("ADMIN_AUTH_SECRET",&cfg.AdminSecret)
 if err:=cfg.Log.ApplyEnv();err!=nil{return Settings{},err}
 if strings.TrimSpace(cfg.DSN)=="" {return Settings{},errors.New("必须配置 database.dsn 或 CONFIG_DB_DSN")}
 return cfg,nil
}
