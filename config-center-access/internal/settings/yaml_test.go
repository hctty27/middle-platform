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
 if err:=os.WriteFile(path,[]byte("database:\n  dsn: yaml-dsn\nconfigCenter:\n  url: http://localhost:8080\nwebhook:\n  secret: file-secret\n"),0600);err!=nil {t.Fatal(err)}
 t.Setenv("ACCESS_DB_DSN","env-dsn")
 t.Setenv("CONFIG_WEBHOOK_SECRET","env-secret")
 cfg,err:=LoadFile(path)
 if err!=nil {t.Fatal(err)}
 if cfg.Address!=":8091" || cfg.DSN!="env-dsn" || cfg.WebhookSecret!="env-secret" || cfg.ConnectTimeout!=3*time.Second {t.Fatalf("%+v",cfg)}
}

func TestRejectInvalidYAML(t *testing.T) {
 path:=filepath.Join(t.TempDir(),"config-dev.yaml")
 if err:=os.WriteFile(path,[]byte("server:\n  typo: 1\n"),0600);err!=nil {t.Fatal(err)}
 if _,err:=LoadFile(path);err==nil {t.Fatal("invalid YAML accepted")}
}
