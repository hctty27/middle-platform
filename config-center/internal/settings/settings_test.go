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
 if err:=os.WriteFile(base,[]byte("server:\n  address: ':8080'\nadmin:\n  writesEnabled: true\n"),0600);err!=nil {t.Fatal(err)}
 if err:=os.WriteFile(dev,[]byte("database:\n  dsn: from-file\n"),0600);err!=nil {t.Fatal(err)}
 t.Setenv("CONFIG_DB_DSN","from-env")
 cfg,err:=LoadFile(dev)
 if err!=nil {t.Fatal(err)}
 if cfg.DSN!="from-env" || cfg.Address!=":8080" || !cfg.WritesEnabled {t.Fatalf("%+v",cfg)}
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
    } {
        path:=filepath.Join(t.TempDir(),"config.yaml")
        data:="database:\n  dsn: test-dsn\n"+extra
        if err:=os.WriteFile(path,[]byte(data),0600);err!=nil{t.Fatal(err)}
        if _,err:=LoadFile(path);err==nil{t.Errorf("obsolete switch accepted: %q",extra)}
    }
}

func TestWebhookAllowedHostsDoesNotRequireWrites(t *testing.T) {
    path:=filepath.Join(t.TempDir(),"config.yaml")
    data:="database:\n  dsn: test-dsn\nwebhook:\n  allowedHosts:\n    - webhook.example.com\n"
    if err:=os.WriteFile(path,[]byte(data),0600);err!=nil{t.Fatal(err)}
    cfg,err:=LoadFile(path)
    if err!=nil{t.Fatal(err)}
    if cfg.WritesEnabled||cfg.AllowedHosts!="webhook.example.com" {
        t.Fatalf("webhook worker should run independently of admin writes: %+v",cfg)
    }
}
