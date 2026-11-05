package settings

import (
    "os"
    "path/filepath"
    "strings"
    "testing"
)

func TestSingleDatabaseConfiguration(t *testing.T) {
    path:=filepath.Join(t.TempDir(),"config.yaml")
    data:="database:\n  dsn: 'sdk_core:pass@tcp(127.0.0.1:3306)/config_center?parseTime=true'\n"
    if err:=os.WriteFile(path,[]byte(data),0600);err!=nil{t.Fatal(err)}
    cfg,err:=Load(path)
    if err!=nil{t.Fatal(err)}
    if cfg.Database.MaxOpenConns!=20{t.Fatalf("unexpected pool default: %d",cfg.Database.MaxOpenConns)}
}

func TestRemovedFeatureSwitchesRejected(t *testing.T) {
    for _,extra := range []string{
        "order:\n  enabled: true\n",
        "payment:\n  allowUnverifiedNonProd: true\n",
        "gameNotify:\n  allowedHosts: []\n",
    } {
        path:=filepath.Join(t.TempDir(),"config.yaml")
        data:="database:\n  dsn: 'sdk_core:pass@tcp(127.0.0.1:3306)/config_center?parseTime=true'\n"+extra
        if err:=os.WriteFile(path,[]byte(data),0600);err!=nil{t.Fatal(err)}
        if _,err:=Load(path);err==nil{t.Errorf("obsolete switch accepted: %q",extra)}
    }
}

func TestLegacyOrderDatabaseConfigurationRejected(t *testing.T) {
    path:=filepath.Join(t.TempDir(),"config.yaml")
    data:="database:\n  dsn: 'sdk_core:pass@tcp(127.0.0.1:3306)/config_center?parseTime=true'\norderDatabase:\n  dsn: 'old-connection'\n"
    if err:=os.WriteFile(path,[]byte(data),0600);err!=nil{t.Fatal(err)}
    _,err:=Load(path)
    if err==nil||!strings.Contains(err.Error(),"orderDatabase"){t.Fatalf("expected obsolete orderDatabase rejection, got %v",err)}
}

func TestMigratedEventReportURLRejected(t *testing.T) {
    path := filepath.Join(t.TempDir(), "config.yaml")
    raw := "database:\n  dsn: 'sdk_core:pass@tcp(127.0.0.1:3306)/config_center'\n" +
        "platform:\n  eventReportURL: 'https://openapiv3.qk6.top/report/event'\n"
    if err := os.WriteFile(path, []byte(raw), 0600); err != nil { t.Fatal(err) }
    if _, err := Load(path); err == nil {
        t.Fatal("旧 platform.eventReportURL 已迁移到 sdk-service，不应被 SDK Core 接受")
    }
}


func TestAdminSecretConfiguration(t *testing.T) {
 path := filepath.Join(t.TempDir(), "config.yaml")
 data := "database:\n  dsn: test-dsn\nadminAuth:\n  secret: file-secret-0123456789abcdef0123456789\n"
 if err := os.WriteFile(path, []byte(data), 0600); err != nil { t.Fatal(err) }
 cfg, err := Load(path)
 if err != nil { t.Fatal(err) }
 if cfg.AdminAuth.Secret != "file-secret-0123456789abcdef0123456789" { t.Fatal("YAML 管理密钥加载失败") }
 t.Setenv("ADMIN_AUTH_SECRET", "env-secret-0123456789abcdef0123456789")
 cfg, err = Load(path)
 if err != nil { t.Fatal(err) }
 if cfg.AdminAuth.Secret != "env-secret-0123456789abcdef0123456789" { t.Fatal("管理密钥环境变量覆盖失败") }
}
