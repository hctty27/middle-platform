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
