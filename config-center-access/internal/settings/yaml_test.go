package settings

import (
 "os"
 "path/filepath"
 "testing"
 "time"
)

func TestYAMLFileAndEnvironmentOverride(t *testing.T) {
 dir:=t.TempDir()
 if err:=os.WriteFile(filepath.Join(dir,"config.yaml"),[]byte("server:\n  address: ':8091'\nsync:\n  connectTimeout: 3s\n"),0600);err!=nil {t.Fatal(err)}
 path:=filepath.Join(dir,"config-dev.yaml")
 if err:=os.WriteFile(path,[]byte("server:\n  address: ':8092'\nsync:\n  connectTimeout: 4s\ndatabase:\n  dsn: yaml-dsn\nconfigCenter:\n  url: http://localhost:8080\nwebhook:\n  secret: file-secret\n"),0600);err!=nil {t.Fatal(err)}
 t.Setenv("ACCESS_DB_DSN","env-dsn")
 t.Setenv("CONFIG_WEBHOOK_SECRET","env-secret")
 cfg,err:=LoadFile(path)
 if err!=nil {t.Fatal(err)}
 if cfg.Address!=":8092" || cfg.DSN!="env-dsn" || cfg.WebhookSecret!="env-secret" || cfg.ConnectTimeout!=4*time.Second {t.Fatalf("%+v",cfg)}
}

func TestRejectInvalidYAML(t *testing.T) {
 path:=filepath.Join(t.TempDir(),"config-dev.yaml")
 if err:=os.WriteFile(path,[]byte("server:\n  typo: 1\n"),0600);err!=nil {t.Fatal(err)}
 if _,err:=LoadFile(path);err==nil {t.Fatal("invalid YAML accepted")}
}

func TestConfigFileDoesNotInheritBase(t *testing.T) {
 dir := t.TempDir()
 base := filepath.Join(dir, "config.yaml")
 dev := filepath.Join(dir, "config-dev.yaml")
 if err := os.WriteFile(base, []byte("server:\n  typo: ignored\n"), 0600); err != nil { t.Fatal(err) }
 if err := os.WriteFile(dev, []byte("database:\n  dsn: test\nwebhook:\n  secret: valid-secret\n"), 0600); err != nil { t.Fatal(err) }
 cfg, err := LoadFile(dev)
 if err != nil { t.Fatal(err) }
 if cfg.Address != ":8081" || cfg.ConnectTimeout != 2*time.Second {
  t.Fatalf("不应读取基础文件或继承其字段: %+v", cfg)
 }
}
