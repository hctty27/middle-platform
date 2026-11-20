package settings

import (
 "os"
 "path/filepath"
 "testing"
)
func TestAdminSettings(t *testing.T) {
 path := filepath.Join(t.TempDir(),"config.yaml")
 if err:=os.WriteFile(path,[]byte("database:\n  dsn: test-dsn\nadminAuth:\n  secret: 0123456789abcdef0123456789abcdef\n"),0600);err!=nil{t.Fatal(err)}
 cfg,err:=Load(path);if err!=nil{t.Fatal(err)}
 if cfg.Database.MaxOpenConns!=10 || cfg.Database.MaxIdleConns!=5 || cfg.Server.Address!=":8231" {t.Fatalf("unexpected defaults: %+v",cfg)}
 t.Setenv("SDK_ADMIN_ADDR","127.0.0.1:8231")
 cfg,err=Load(path);if err!=nil||cfg.Server.Address!="127.0.0.1:8231"{t.Fatalf("address override: %v",err)}
}
func TestAdminSecretRequired(t *testing.T) {
 path := filepath.Join(t.TempDir(),"config.yaml")
 if err:=os.WriteFile(path,[]byte("database:\n  dsn: test-dsn\n"),0600);err!=nil{t.Fatal(err)}
 if _,err:=Load(path);err==nil {t.Fatal("admin secret must be required")}
}
