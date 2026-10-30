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
)

// Settings 保存配置中心运行参数。
type Settings struct {
 DSN string
 Address string
 AllowedHosts string
 WritesEnabled bool
 WebhookEnabled bool
 AllowLegacyUpgrade bool
}

type configFile struct {
 Server struct { Address string `yaml:"address"` } `yaml:"server"`
 Database struct { DSN string `yaml:"dsn"` } `yaml:"database"`
 Admin struct { WritesEnabled *bool `yaml:"writesEnabled"` } `yaml:"admin"`
 Webhook struct {
  Enabled *bool `yaml:"enabled"`
  AllowedHosts []string `yaml:"allowedHosts"`
 } `yaml:"webhook"`
 Migration struct { AllowLegacyUpgrade *bool `yaml:"allowLegacyUpgrade"` } `yaml:"migration"`
}

// Load 兼容旧版环境变量启动方式。
func Load() (Settings,error) { return LoadFile("") }

// LoadFile 依次加载共享 YAML、指定环境 YAML，再应用环境变量覆盖。
func LoadFile(path string) (Settings,error) {
 var data configFile
 if path!="" {
  base:=filepath.Join(filepath.Dir(path),"config.yaml")
  if filepath.Clean(path)!=filepath.Clean(base) {
   if _,err:=os.Stat(base);err==nil {
    if err:=readYAML(base,&data);err!=nil { return Settings{},err }
   } else if !errors.Is(err,os.ErrNotExist) { return Settings{},err }
  }
  if err:=readYAML(path,&data);err!=nil { return Settings{},err }
 }
 cfg:=Settings{Address:":8081",DSN:data.Database.DSN,AllowedHosts:strings.Join(data.Webhook.AllowedHosts,",")}
 if data.Server.Address!="" {cfg.Address=data.Server.Address}
 if data.Admin.WritesEnabled!=nil {cfg.WritesEnabled=*data.Admin.WritesEnabled}
 if data.Webhook.Enabled!=nil {cfg.WebhookEnabled=*data.Webhook.Enabled}
 if data.Migration.AllowLegacyUpgrade!=nil {cfg.AllowLegacyUpgrade=*data.Migration.AllowLegacyUpgrade}
 // 环境变量优先级最高，管理写入和回调均由显式开关控制。
 overrideString("CONFIG_DB_DSN",&cfg.DSN)
 overrideString("CONFIG_GO_ADDR",&cfg.Address)
 overrideString("WEBHOOK_ALLOWED_HOSTS",&cfg.AllowedHosts)
 for _,b:=range []struct{name string; target *bool}{
  {"CONFIG_GO_ENABLE_WRITES",&cfg.WritesEnabled},
  {"CONFIG_GO_ENABLE_WEBHOOK",&cfg.WebhookEnabled},
  {"CONFIG_ALLOW_LEGACY_UPGRADE",&cfg.AllowLegacyUpgrade},
 } {
  if err:=overrideBool(b.name,b.target);err!=nil{return Settings{},err}
 }
 if strings.TrimSpace(cfg.DSN)=="" {return Settings{},errors.New("database.dsn or CONFIG_DB_DSN is required")}
 if cfg.WebhookEnabled && !cfg.WritesEnabled {return Settings{},errors.New("webhook requires admin.writesEnabled=true")}
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
