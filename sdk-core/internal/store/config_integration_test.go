package store

import (
    "context"
    "database/sql"
    "encoding/json"
    "errors"
    "os"
    "testing"

    _ "github.com/go-sql-driver/mysql"
    "github.com/hctty27/middle-platform/sdk-core/internal/model"
)

// TestLookupMySQL 使用真实 MySQL 验证单次 JOIN 的环境关联、状态过滤与大区 JSON。
func TestLookupMySQL(t *testing.T) {
    dsn := os.Getenv("TEST_SDK_CORE_DB_DSN")
    if dsn == "" {
        t.Skip("set TEST_SDK_CORE_DB_DSN to run MySQL integration test")
    }

    db, err := sql.Open("mysql", dsn)
    if err != nil { t.Fatal(err) }
    defer db.Close()

    ctx := context.Background()
    if err := db.PingContext(ctx); err != nil { t.Fatal(err) }

    schema := []string{
        "DROP TABLE IF EXISTS config_channel_app",
        "DROP TABLE IF EXISTS config_node",
        `CREATE TABLE config_node (
            id BIGINT PRIMARY KEY,
            node_type VARCHAR(32) NOT NULL,
            code VARCHAR(128) NOT NULL,
            status TINYINT NOT NULL,
            config JSON NOT NULL,
            game_id BIGINT NULL,
            region_id BIGINT NULL,
            env_type VARCHAR(16) NULL
        ) ENGINE=InnoDB`,
        `CREATE TABLE config_channel_app (
            id BIGINT PRIMARY KEY,
            game_id BIGINT NOT NULL,
            code VARCHAR(128) NOT NULL UNIQUE,
            name VARCHAR(128) NOT NULL,
            status TINYINT NOT NULL,
            config JSON NOT NULL,
            prod_server_group_id BIGINT NULL,
            test_server_group_id BIGINT NULL,
            review_server_group_id BIGINT NULL
        ) ENGINE=InnoDB`,
        `INSERT INTO config_node (id,node_type,code,status,config,game_id,region_id,env_type) VALUES
            (1,'game','game1001',1,'{"gameKey":"main","login":{"sdk":{"enabled":true}}}',NULL,NULL,NULL),
            (2,'region','region-prod',1,'{"regionKey":"prod","login":{"url":"https://prod.example.com"}}',1,NULL,'prod'),
            (3,'region','region-test',1,'{"regionKey":"test"}',1,NULL,'test'),
            (4,'region','region-review',1,'{"regionKey":"review"}',1,NULL,'review'),
            (5,'server_group','group-prod',1,'{"groupKey":"p"}',1,2,NULL),
            (6,'server_group','group-test',1,'{"groupKey":"t"}',1,3,NULL),
            (7,'server_group','group-review',1,'{"groupKey":"r"}',1,4,NULL)`,
        `INSERT INTO config_channel_app
            (id,game_id,code,name,status,config,prod_server_group_id,test_server_group_id,review_server_group_id)
          VALUES (10,1,'app1001','Demo',1,'{"tsKey":"abc"}',5,6,7)`,
    }
    for _, query := range schema {
        if _,err := db.ExecContext(ctx,query);err!=nil { t.Fatalf("schema setup: %v; SQL: %s",err,query) }
    }
    t.Cleanup(func() {
        db.ExecContext(context.Background(),"DROP TABLE IF EXISTS config_channel_app")
        db.ExecContext(context.Background(),"DROP TABLE IF EXISTS config_node")
    })

    store := NewStore(db)
    if err := store.VerifySchema(ctx);err!=nil { t.Fatal(err) }

    for _,tc := range []struct{env,regionCode,groupCode,regionKey string}{
        {"prod","region-prod","group-prod","prod"},
        {"test","region-test","group-test","test"},
        {"review","region-review","group-review","review"},
    } {
        t.Run(tc.env, func(t *testing.T) {
            app,group,err := store.Lookup(ctx,"app1001",tc.env)
            if err!=nil { t.Fatal(err) }
            if app.ID!=10 || app.Code!="app1001" || app.GameCode!="game1001" ||
                string(app.Config)=="" || group.RegionCode!=tc.regionCode ||
                group.ServerGroupCode!=tc.groupCode {
                t.Fatalf("unexpected app or group: %+v %+v",app,group)
            }
            if !jsonHasRegionKey(group.RegionConfig,tc.regionKey) {
                t.Fatalf("region config mismatch: %s",group.RegionConfig)
            }
            var gameConfig map[string]any
            if err := json.Unmarshal(app.GameConfig,&gameConfig);err!=nil { t.Fatal(err) }
            if gameConfig["gameKey"]!="main" { t.Fatalf("unexpected game JSON: %s",app.GameConfig) }
            if len(group.Config)==0 { t.Fatal("missing server group JSON") }
        })
    }

    // 未绑定、区服组禁用、大区禁用、游戏禁用、渠道禁用均不可返回配置。
    if _,err := db.ExecContext(ctx,"UPDATE config_channel_app SET review_server_group_id=NULL WHERE id=10");err!=nil { t.Fatal(err) }
    assertLookupNotFound(t,store,"review")
    if _,err := db.ExecContext(ctx,"UPDATE config_node SET status=0 WHERE id=5");err!=nil { t.Fatal(err) }
    assertLookupNotFound(t,store,"prod")
    if _,err := db.ExecContext(ctx,"UPDATE config_node SET status=0 WHERE id=3");err!=nil { t.Fatal(err) }
    assertLookupNotFound(t,store,"test")
    if _,err := db.ExecContext(ctx,"UPDATE config_node SET status=0 WHERE id=1");err!=nil { t.Fatal(err) }
    assertLookupNotFound(t,store,"prod")
    if _,err := db.ExecContext(ctx,"UPDATE config_channel_app SET status=0 WHERE id=10");err!=nil { t.Fatal(err) }
    assertLookupNotFound(t,store,"prod")
}

func assertLookupNotFound(t *testing.T,store *Store,env string) {
    t.Helper()
    _,group,err := store.Lookup(context.Background(),"app1001",env)
    if !errors.Is(err,model.ErrNotFound) || group!=nil {
        t.Fatalf("env=%s: expected ErrNotFound, got group=%+v err=%v",env,group,err)
    }
}

func jsonHasRegionKey(raw []byte,want string) bool {
    var obj struct {
        RegionKey string `json:"regionKey"`
    }
    return json.Unmarshal(raw,&obj)==nil && obj.RegionKey==want
}
