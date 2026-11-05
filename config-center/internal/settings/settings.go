package settings

import (
 "bytes"
 "errors"
 "fmt"
 "io"
 "os"
 "path/filepath"
 "strings"

 "gopkg.in/yaml.v3"
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
 AdminAuth struct { Secret string `yaml:"secret"` } `yaml:"adminAuth"`
}

// Load 兼容旧版环境变量启动方式。
func Load() (Settings,error) { return LoadFile("") }

// LoadFile 依次加载共享 YAML、指定环境 YAML，再应用环境变量覆盖。
func LoadFile(path string) (Settings,error) {
 data:=configFile{Log:logging.Default()}
 if path!="" {
  base:=filepath.Join(filepath.Dir(path),"config.yaml")
  if filepath.Clean(path)!=filepath.Clean(base) {
   if _,err:=os.Stat(base);err==nil {
    if err:=readYAML(base,&data);err!=nil { return Settings{},err }
   } else if !errors.Is(err,os.ErrNotExist) { return Settings{},err }
  }
  if err:=readYAML(path,&data);err!=nil { return Settings{},err }
 }
 cfg:=Settings{Address:"127.0.0.1:8080",DSN:data.Database.DSN,Log:data.Log,AdminSecret:data.AdminAuth.Secret}
 if data.Server.Address!="" {cfg.Address=data.Server.Address}
 // 环境变量优先级最高；管理接口的安全边界由受保护的网关负责。
 overrideString("CONFIG_DB_DSN",&cfg.DSN)
 overrideString("CONFIG_GO_ADDR",&cfg.Address)
 overrideString("ADMIN_AUTH_SECRET",&cfg.AdminSecret)
 if err:=cfg.Log.ApplyEnv();err!=nil{return Settings{},err}
 if strings.TrimSpace(cfg.DSN)=="" {return Settings{},errors.New("必须配置 database.dsn 或 CONFIG_DB_DSN")}
 return cfg,nil
}

func readYAML(path string,target *configFile) error {
 raw,err:=os.ReadFile(path)
 if err!=nil {return fmt.Errorf("读取 YAML %s 失败: %w",path,err)}
 dec:=yaml.NewDecoder(bytes.NewReader(raw))
 dec.KnownFields(true)
 if err:=dec.Decode(target);err!=nil{return fmt.Errorf("解析 YAML %s 失败: %w",path,err)}
 var next any
 if err:=dec.Decode(&next);err!=io.EOF {return fmt.Errorf("YAML %s 必须只包含一份文档",path)}
 return nil
}

func overrideString(key string,target *string) {
 if value,ok:=os.LookupEnv(key);ok {*target=value}
}

