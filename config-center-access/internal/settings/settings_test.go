package settings

import "testing"

func TestLoad(t *testing.T) {
	t.Setenv("ACCESS_DB_DSN","root:secret@tcp(127.0.0.1:3306)/access?parseTime=true")
	t.Setenv("CONFIG_WEBHOOK_SECRET","safe-secret")
	t.Setenv("CONFIG_CENTER_URL","http://127.0.0.1:8080")
	t.Setenv("CONFIG_CLIENT_CONNECT_TIMEOUT_MS","2000")
	t.Setenv("CONFIG_CLIENT_READ_TIMEOUT_MS","15000")
	cfg, err := Load()
	if err != nil { t.Fatal(err) }
	if cfg.Address != ":8081" || cfg.ConnectTimeout.Milliseconds()!=2000 { t.Fatal(cfg) }
}

func TestLoadRejectsMissingSecret(t *testing.T) {
	t.Setenv("ACCESS_DB_DSN","root:secret@tcp(127.0.0.1:3306)/access")
	t.Setenv("CONFIG_WEBHOOK_SECRET","")
	if _,err:=Load();err==nil { t.Fatal("expected missing secret failure") }
}
