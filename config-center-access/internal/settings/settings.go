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
}

// Load 保留纯环境变量启动方式。
func Load() (Config,error) { return LoadFile("") }

// LoadFile 依次加载共享 YAML、环境 YAML，再用环境变量覆盖。
func LoadFile(path string) (Config,error) {
 var data configFile
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
   return Config{},fmt.Errorf("invalid %s duration %q",item.name,item.value)
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
   if err!=nil || ms<1 || ms>120000 {return Config{},fmt.Errorf("%s must be 1..120000 milliseconds",item.name)}
   *item.target=time.Duration(ms)*time.Millisecond
  }
 }
 if raw,ok:=os.LookupEnv("CONFIG_RECONCILE_INTERVAL");ok {
  duration,err:=time.ParseDuration(raw)
  if err!=nil || duration<time.Second {return Config{},fmt.Errorf("CONFIG_RECONCILE_INTERVAL must be a duration >= 1s")}
  cfg.ReconcileInterval=duration
 }
 cfg.CenterURL=strings.TrimRight(cfg.CenterURL,"/")
 if strings.TrimSpace(cfg.DSN)=="" {return Config{},fmt.Errorf("database.dsn or ACCESS_DB_DSN is required")}
 if strings.TrimSpace(cfg.WebhookSecret)=="" || cfg.WebhookSecret=="change-me-in-development" {
  return Config{},fmt.Errorf("webhook.secret or CONFIG_WEBHOOK_SECRET must be configured")
 }
 parsed,err:=url.Parse(cfg.CenterURL)
 if err!=nil || parsed.Host=="" || (parsed.Scheme!="http" && parsed.Scheme!="https") || parsed.User!=nil || parsed.RawQuery!="" || parsed.Fragment!="" || parsed.Path!="" {
  return Config{},fmt.Errorf("configCenter.url must be an http(s) origin without path")
 }
 return cfg,nil
}

func readYAML(path string,target *configFile) error {
 raw,err:=os.ReadFile(path)
 if err!=nil{return fmt.Errorf("read YAML %s: %w",path,err)}
 decoder:=yaml.NewDecoder(bytes.NewReader(raw))
 decoder.KnownFields(true)
 if err:=decoder.Decode(target);err!=nil{return fmt.Errorf("decode YAML %s: %w",path,err)}
 var extra any
 if err:=decoder.Decode(&extra);err!=io.EOF{return fmt.Errorf("YAML %s must contain exactly one document",path)}
 return nil
}

func overrideString(key string,target *string) {
 if value,ok:=os.LookupEnv(key);ok {*target=value}
}
