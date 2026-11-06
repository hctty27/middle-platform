package settings

import (
 "os"
 "path/filepath"
 "testing"
 "strings"
)

func TestYAMLAndEnvironmentOverride(t *testing.T) {
 dir:=t.TempDir()
 base:=filepath.Join(dir,"config.yaml")
 dev:=filepath.Join(dir,"config-dev.yaml")
 if err:=os.WriteFile(base,[]byte("server:\n  address: ':9999'\n"),0600);err!=nil {t.Fatal(err)}
 if err:=os.WriteFile(dev,[]byte("server:\n  address: ':8080'\ndatabase:\n  dsn: from-file\n"),0600);err!=nil {t.Fatal(err)}
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


func TestRemovedAdminAuthEnabledRejected(t *testing.T) {
    for _, enabled := range []string{"true", "false"} {
        path := filepath.Join(t.TempDir(), "config.yaml")
        data := "database:\n  dsn: test-dsn\nadminAuth:\n  enabled: " + enabled + "\n"
        if err := os.WriteFile(path, []byte(data), 0600); err != nil { t.Fatal(err) }
        if _, err := LoadFile(path); err == nil {
            t.Fatalf("removed adminAuth.enabled=%s should be rejected", enabled)
        }
    }
}

func TestConfigFileDoesNotInheritBaseConfig(t *testing.T) {
    dir := t.TempDir()
    base := filepath.Join(dir, "config.yaml")
    dev := filepath.Join(dir, "config-dev.yaml")
    if err := os.WriteFile(base, []byte("server:\n  address: ':9999'\nadminAuth:\n  secret: base-only-secret\n"), 0600); err != nil { t.Fatal(err) }
    if err := os.WriteFile(dev, []byte("database:\n  dsn: test\n"), 0600); err != nil { t.Fatal(err) }
    cfg, err := LoadFile(dev)
    if err != nil { t.Fatal(err) }
    if cfg.AdminSecret != "" || cfg.Address != "127.0.0.1:8080" {
        t.Fatalf("指定文件不应继承同目录的 config.yaml: %+v", cfg)
    }
}

func TestLoadReadsDefaultYAML(t *testing.T) {
 dir:=t.TempDir()
 t.Chdir(dir)
 if err:=os.MkdirAll("configs",0700);err!=nil{t.Fatal(err)}
 if err:=os.WriteFile(filepath.Join("configs","config.yaml"),[]byte("server:\n  address: ':8095'\ndatabase:\n  dsn: file-dsn\n"),0600);err!=nil{t.Fatal(err)}
 cfg,err:=Load()
 if err!=nil{t.Fatal(err)}
 if cfg.Address!=":8095"||cfg.DSN!="file-dsn"{t.Fatalf("未读取默认配置文件: %+v",cfg)}
 t.Setenv("CONFIG_GO_ADDR","127.0.0.1:8255")
 cfg,err=LoadFile("")
 if err!=nil{t.Fatal(err)}
 if cfg.Address!="127.0.0.1:8255"{t.Fatal("环境变量必须优先于 YAML")}
}

func TestLoadRequiresDefaultYAML(t *testing.T) {
 t.Chdir(t.TempDir())
 t.Setenv("CONFIG_DB_DSN","test-dsn")
 if _,err:=LoadFile("");err==nil||!strings.Contains(err.Error(),"configs/config.yaml"){
  t.Fatalf("缺少默认 YAML 时不应退回纯环境变量启动: %v",err)
 }
}
