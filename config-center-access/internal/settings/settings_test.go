package settings

import (
 "os"
 "path/filepath"
 "strings"
 "testing"
)

func testDefaultYAML(t *testing.T, yaml string) {
 t.Helper()
 dir:=t.TempDir()
 t.Chdir(dir)
 path:=filepath.Join("configs","config.yaml")
 if err:=os.MkdirAll(filepath.Dir(path),0700);err!=nil{t.Fatal(err)}
 if err:=os.WriteFile(path,[]byte(yaml),0600);err!=nil{t.Fatal(err)}
}

// 默认读取 YAML，环境变量覆盖指定值。
func TestLoad(t *testing.T) {
 testDefaultYAML(t,"server:\n  address: ':8089'\ndatabase:\n  dsn: file-dsn\nwebhook:\n  secret: file-secret\n")
 t.Setenv("ACCESS_DB_DSN","root:secret@tcp(127.0.0.1:3306)/access?parseTime=true")
 t.Setenv("CONFIG_WEBHOOK_SECRET","safe-secret")
 t.Setenv("CONFIG_CENTER_URL","http://127.0.0.1:8080")
 t.Setenv("CONFIG_CLIENT_CONNECT_TIMEOUT_MS","2000")
 t.Setenv("CONFIG_CLIENT_READ_TIMEOUT_MS","15000")
 cfg,err:=Load()
 if err!=nil{t.Fatal(err)}
 if cfg.Address!=":8089"||cfg.ConnectTimeout.Milliseconds()!=2000{t.Fatalf("%+v",cfg)}
}

// 不再允许仅靠环境变量而缺少默认配置文件启动。
func TestLoadRequiresDefaultYAML(t *testing.T) {
 t.Chdir(t.TempDir())
 t.Setenv("ACCESS_DB_DSN","test-dsn")
 t.Setenv("CONFIG_WEBHOOK_SECRET","valid-secret")
 if _,err:=Load();err==nil||!strings.Contains(err.Error(),"configs/config.yaml"){
  t.Fatalf("缺少默认 YAML 应拒绝启动: %v",err)
 }
}

func TestLoadRejectsMissingSecret(t *testing.T) {
 testDefaultYAML(t,"database:\n  dsn: file-dsn\nwebhook:\n  secret: ''\n")
 t.Setenv("ACCESS_DB_DSN","root:secret@tcp(127.0.0.1:3306)/access")
 t.Setenv("CONFIG_WEBHOOK_SECRET","")
 if _,err:=Load();err==nil||!strings.Contains(err.Error(),"webhook.secret"){
  t.Fatalf("缺少回调密钥必须拒绝启动: %v",err)
 }
}
