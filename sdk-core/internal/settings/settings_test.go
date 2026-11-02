package settings

import (
    "os"
    "path/filepath"
    "strings"
    "testing"
)

func TestOrderEnabledDefaultsFalse(t *testing.T) {
    path:=filepath.Join(t.TempDir(),"config.yaml")
    data:="database:\n  dsn: 'sdk_core:pass@tcp(127.0.0.1:3306)/config_center?parseTime=true'\n"
    if err:=os.WriteFile(path,[]byte(data),0600);err!=nil{t.Fatal(err)}
    cfg,err:=Load(path)
    if err!=nil{t.Fatal(err)}
    if cfg.Order.Enabled {t.Fatal("order functionality must be disabled by default")}
    if cfg.Database.MaxOpenConns!=20{t.Fatalf("unexpected pool default: %d",cfg.Database.MaxOpenConns)}
}

func TestOrderEnabledExplicitly(t *testing.T) {
    path:=filepath.Join(t.TempDir(),"config.yaml")
    data:="database:\n  dsn: 'sdk_core:pass@tcp(127.0.0.1:3306)/config_center?parseTime=true'\norder:\n  enabled: true\n"
    if err:=os.WriteFile(path,[]byte(data),0600);err!=nil{t.Fatal(err)}
    cfg,err:=Load(path)
    if err!=nil{t.Fatal(err)}
    if !cfg.Order.Enabled{t.Fatal("explicit order.enabled=true ignored")}
}

func TestLegacyOrderDatabaseConfigurationRejected(t *testing.T) {
    path:=filepath.Join(t.TempDir(),"config.yaml")
    data:="database:\n  dsn: 'sdk_core:pass@tcp(127.0.0.1:3306)/config_center?parseTime=true'\norderDatabase:\n  dsn: 'old-connection'\n"
    if err:=os.WriteFile(path,[]byte(data),0600);err!=nil{t.Fatal(err)}
    _,err:=Load(path)
    if err==nil||!strings.Contains(err.Error(),"orderDatabase"){t.Fatalf("expected obsolete orderDatabase rejection, got %v",err)}
}
