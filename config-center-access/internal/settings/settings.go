// Package settings 负责接入端配置文件解析、环境变量覆盖和参数校验。
package settings

import (
 "bytes"
 "fmt"
 "io"
 "net/url"
 "os"
 "path/filepath"
 "strconv"
 "strings"
 "time"

 "gopkg.in/yaml.v3"
 "github.com/hctty27/middle-platform/shared/logging"
)

// Config 保存接入服务运行参数。
type Config struct {
 Address string
 DSN string
 CenterURL string
 WebhookSecret string
 ConnectTimeout time.Duration
 RequestTimeout time.Duration
 ReconcileInterval time.Duration
 Log logging.Config
}

type configFile struct {
 Server struct { Address string `yaml:"address"` } `yaml:"server"`
 Database struct { DSN string `yaml:"dsn"` } `yaml:"database"`
 ConfigCenter struct { URL string `yaml:"url"` } `yaml:"configCenter"`
 Webhook struct { Secret string `yaml:"secret"` } `yaml:"webhook"`
 Sync struct {
  ConnectTimeout string `yaml:"connectTimeout"`
  RequestTimeout string `yaml:"requestTimeout"`
  ReconcileInterval string `yaml:"reconcileInterval"`
 } `yaml:"sync"`
 Log logging.Config `yaml:"log"`
}

// Load 保留纯环境变量启动方式。
func Load() (Config,error) { return LoadFile("") }

// LoadFile 依次加载共享 YAML、环境 YAML，再用环境变量覆盖。
func LoadFile(path string) (Config,error) {
 data:=configFile{Log:logging.Default()}
 if path!="" {
  base:=filepath.Join(filepath.Dir(path),"config.yaml")
  if filepath.Clean(path)!=filepath.Clean(base) {
   if _,err:=os.Stat(base);err==nil {
    if err:=readYAML(base,&data);err!=nil{return Config{},err}
   } else if !os.IsNotExist(err) {return Config{},err}
  }
  if err:=readYAML(path,&data);err!=nil{return Config{},err}
 }
 cfg:=Config{
  Address:":8081",
  DSN:data.Database.DSN,
  CenterURL:"http://127.0.0.1:8080",
  WebhookSecret:data.Webhook.Secret,
  ConnectTimeout:2*time.Second,
  RequestTimeout:15*time.Second,
  ReconcileInterval:5*time.Minute,
  Log:data.Log,
 }
 if data.Server.Address!="" {cfg.Address=data.Server.Address}
 if data.ConfigCenter.URL!="" {cfg.CenterURL=data.ConfigCenter.URL}
 for _,item:=range []struct{name,value string; target *time.Duration; min,max time.Duration}{
  {"sync.connectTimeout",data.Sync.ConnectTimeout,&cfg.ConnectTimeout,time.Millisecond,120*time.Second},
  {"sync.requestTimeout",data.Sync.RequestTimeout,&cfg.RequestTimeout,time.Millisecond,120*time.Second},
  {"sync.reconcileInterval",data.Sync.ReconcileInterval,&cfg.ReconcileInterval,time.Second,0},
 } {
  if item.value=="" {continue}
  parsed,err:=time.ParseDuration(item.value)
  if err!=nil || parsed<item.min || (item.max>0 && parsed>item.max) {
   return Config{},fmt.Errorf("%s 时间配置不合法: %q",item.name,item.value)
  }
  *item.target=parsed
 }
 // 环境变量可以覆盖 YAML 的数据库和连接参数。
 overrideString("CONFIG_ACCESS_ADDR",&cfg.Address)
 overrideString("ACCESS_DB_DSN",&cfg.DSN)
 overrideString("CONFIG_CENTER_URL",&cfg.CenterURL)
 overrideString("CONFIG_WEBHOOK_SECRET",&cfg.WebhookSecret)
 for _,item:=range []struct{name string; target *time.Duration}{
  {"CONFIG_CLIENT_CONNECT_TIMEOUT_MS",&cfg.ConnectTimeout},
  {"CONFIG_CLIENT_READ_TIMEOUT_MS",&cfg.RequestTimeout},
 } {
  if raw,ok:=os.LookupEnv(item.name);ok {
   ms,err:=strconv.Atoi(raw)
   if err!=nil || ms<1 || ms>120000 {return Config{},fmt.Errorf("%s 必须是 1 到 120000 毫秒",item.name)}
   *item.target=time.Duration(ms)*time.Millisecond
  }
 }
 if raw,ok:=os.LookupEnv("CONFIG_RECONCILE_INTERVAL");ok {
  duration,err:=time.ParseDuration(raw)
  if err!=nil || duration<time.Second {return Config{},fmt.Errorf("CONFIG_RECONCILE_INTERVAL 必须是大于等于 1s 的时长")}
  cfg.ReconcileInterval=duration
 }
 if err:=cfg.Log.ApplyEnv();err!=nil{return Config{},err}
 cfg.CenterURL=strings.TrimRight(cfg.CenterURL,"/")
 if strings.TrimSpace(cfg.DSN)=="" {return Config{},fmt.Errorf("必须配置 database.dsn 或 ACCESS_DB_DSN")}
 if strings.TrimSpace(cfg.WebhookSecret)=="" || cfg.WebhookSecret=="change-me-in-development" {
  return Config{},fmt.Errorf("必须配置 webhook.secret 或 CONFIG_WEBHOOK_SECRET")
 }
 parsed,err:=url.Parse(cfg.CenterURL)
 if err!=nil || parsed.Host=="" || (parsed.Scheme!="http" && parsed.Scheme!="https") || parsed.User!=nil || parsed.RawQuery!="" || parsed.Fragment!="" || parsed.Path!="" {
  return Config{},fmt.Errorf("configCenter.url 必须是没有路径的 HTTP(S) 地址")
 }
 return cfg,nil
}

func readYAML(path string,target *configFile) error {
 raw,err:=os.ReadFile(path)
 if err!=nil{return fmt.Errorf("读取 YAML %s 失败: %w",path,err)}
 decoder:=yaml.NewDecoder(bytes.NewReader(raw))
 decoder.KnownFields(true)
 if err:=decoder.Decode(target);err!=nil{return fmt.Errorf("解析 YAML %s 失败: %w",path,err)}
 var extra any
 if err:=decoder.Decode(&extra);err!=io.EOF{return fmt.Errorf("YAML %s 必须只包含一份文档",path)}
 return nil
}

func overrideString(key string,target *string) {
 if value,ok:=os.LookupEnv(key);ok {*target=value}
}
