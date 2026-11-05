package settings

import (
 "os"
 "path/filepath"
 "testing"
)

func TestYAMLAndEnvironmentOverride(t *testing.T) {
 dir:=t.TempDir()
 base:=filepath.Join(dir,"config.yaml")
 dev:=filepath.Join(dir,"config-dev.yaml")
 if err:=os.WriteFile(base,[]byte("server:\n  address: ':8080'\n"),0600);err!=nil {t.Fatal(err)}
 if err:=os.WriteFile(dev,[]byte("database:\n  dsn: from-file\n"),0600);err!=nil {t.Fatal(err)}
 t.Setenv("CONFIG_DB_DSN","from-env")
 cfg,err:=LoadFile(dev)
 if err!=nil {t.Fatal(err)}
 if cfg.DSN!="from-env" || cfg.Address!=":8080" {t.Fatalf("%+v",cfg)}
}

func TestRejectInvalidYAML(t *testing.T) {
 dir:=t.TempDir()
 path:=filepath.Join(dir,"config-dev.yaml")
 if err:=os.WriteFile(path,[]byte("server:\n  typo: true\n"),0600);err!=nil {t.Fatal(err)}
 if _,err:=LoadFile(path);err==nil {t.Fatal("invalid yaml was accepted")}
}

func TestObsoleteSwitchesRejected(t *testing.T) {
    for _,extra:=range []string{
        "webhook:\n  enabled: true\n",
        "migration:\n  allowLegacyUpgrade: true\n",
        "admin:\n  writesEnabled: true\n",
    } {
        path:=filepath.Join(t.TempDir(),"config.yaml")
        data:="database:\n  dsn: test-dsn\n"+extra
        if err:=os.WriteFile(path,[]byte(data),0600);err!=nil{t.Fatal(err)}
        if _,err:=LoadFile(path);err==nil{t.Errorf("obsolete switch accepted: %q",extra)}
    }
}

func TestRemovedWebhookWhitelistRejected(t *testing.T) {
    path := filepath.Join(t.TempDir(), "config.yaml")
    data := "database:\n  dsn: test-dsn\nwebhook:\n  allowedHosts: [localhost]\n"
    if err := os.WriteFile(path, []byte(data), 0600); err != nil {
        t.Fatal(err)
    }
    if _, err := LoadFile(path); err == nil {
        t.Fatal("obsolete webhook.allowedHosts should be rejected")
    }
}


func TestAdminSecretConfiguration(t *testing.T) {
 path := filepath.Join(t.TempDir(), "config.yaml")
 data := "database:\n  dsn: test\nadminAuth:\n  secret: file-secret-0123456789abcdef0123456789\n"
 if err := os.WriteFile(path, []byte(data), 0600); err != nil { t.Fatal(err) }
 cfg, err := LoadFile(path)
 if err != nil { t.Fatal(err) }
 if cfg.AdminSecret != "file-secret-0123456789abcdef0123456789" { t.Fatal("YAML 管理密钥加载失败") }
 t.Setenv("ADMIN_AUTH_SECRET", "env-secret-0123456789abcdef0123456789")
 cfg, err = LoadFile(path)
 if err != nil { t.Fatal(err) }
 if cfg.AdminSecret != "env-secret-0123456789abcdef0123456789" { t.Fatal("管理密钥环境变量覆盖失败") }
}
