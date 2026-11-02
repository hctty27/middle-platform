package settings

import (
 "bytes"
 "errors"
 "fmt"
 "io"
 "os"
 "path/filepath"
 "strconv"
 "strings"

 "gopkg.in/yaml.v3"
 "github.com/hctty27/middle-platform/shared/logging"
)

// Settings 保存配置中心运行参数。
type Settings struct {
 DSN string
 Address string
 AllowedHosts string
 WritesEnabled bool
 Log logging.Config
}

type configFile struct {
 Server struct { Address string `yaml:"address"` } `yaml:"server"`
 Database struct { DSN string `yaml:"dsn"` } `yaml:"database"`
 Admin struct { WritesEnabled *bool `yaml:"writesEnabled"` } `yaml:"admin"`
 Webhook struct {
  AllowedHosts []string `yaml:"allowedHosts"`
 } `yaml:"webhook"`
 Log logging.Config `yaml:"log"`
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
 cfg:=Settings{Address:":8081",DSN:data.Database.DSN,AllowedHosts:strings.Join(data.Webhook.AllowedHosts,","),Log:data.Log}
 if data.Server.Address!="" {cfg.Address=data.Server.Address}
 if data.Admin.WritesEnabled!=nil {cfg.WritesEnabled=*data.Admin.WritesEnabled}
 // 环境变量优先级最高；管理写接口仍由独立安全开关控制。
 overrideString("CONFIG_DB_DSN",&cfg.DSN)
 overrideString("CONFIG_GO_ADDR",&cfg.Address)
 overrideString("WEBHOOK_ALLOWED_HOSTS",&cfg.AllowedHosts)
 if err:=overrideBool("CONFIG_GO_ENABLE_WRITES",&cfg.WritesEnabled);err!=nil{return Settings{},err}
 if err:=cfg.Log.ApplyEnv();err!=nil{return Settings{},err}
 if strings.TrimSpace(cfg.DSN)=="" {return Settings{},errors.New("database.dsn or CONFIG_DB_DSN is required")}
 return cfg,nil
}

func readYAML(path string,target *configFile) error {
 raw,err:=os.ReadFile(path)
 if err!=nil {return fmt.Errorf("read YAML %s: %w",path,err)}
 dec:=yaml.NewDecoder(bytes.NewReader(raw))
 dec.KnownFields(true)
 if err:=dec.Decode(target);err!=nil{return fmt.Errorf("decode YAML %s: %w",path,err)}
 var next any
 if err:=dec.Decode(&next);err!=io.EOF {return fmt.Errorf("YAML %s must contain exactly one document",path)}
 return nil
}

func overrideString(key string,target *string) {
 if value,ok:=os.LookupEnv(key);ok {*target=value}
}

func overrideBool(key string,target *bool) error {
 if value,ok:=os.LookupEnv(key);ok {
  parsed,err:=strconv.ParseBool(value)
  if err!=nil{return fmt.Errorf("invalid %s: %w",key,err)}
  *target=parsed
 }
 return nil
}
