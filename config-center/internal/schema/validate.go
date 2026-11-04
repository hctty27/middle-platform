// Package schema 只读校验人工执行的 MySQL 表结构，运行时绝不执行建表或升级。
package schema

import (
 "context"
 "database/sql"
 "errors"
 "fmt"
 "strings"
 "time"

 "github.com/go-sql-driver/mysql"
)

// Validate 校验权威库所需的表、字段、索引及约束，不依赖旧迁移版本记录。
func Validate(ctx context.Context, dsn string) error {
 cfg,err:=mysql.ParseDSN(dsn)
 if err!=nil{return err}
 if cfg.DBName=="" {return errors.New("必须配置数据库名称")}
 cfg.ParseTime=true
 cfg.Loc=time.UTC
 cfg.Timeout=5*time.Second
 cfg.ReadTimeout=10*time.Second
 db,err:=sql.Open("mysql",cfg.FormatDSN())
 if err!=nil{return err}
 defer db.Close()
 conn,err:=db.Conn(ctx)
 if err!=nil{return err}
 defer conn.Close()
 if err=validateSchema(ctx,conn);err!=nil{
    return fmt.Errorf("数据库结构不符合要求: %w；请手动执行 config-center/db/sql 中已审核的 SQL",err)
 }
 return nil
}

func hasTable(ctx context.Context, conn *sql.Conn, name string)(bool,error){
 var n int
 err:=conn.QueryRowContext(ctx,
 "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=DATABASE() AND table_name=?",name).Scan(&n)
 return n==1,err
}

func validateSchema(ctx context.Context, conn *sql.Conn) error {
	// 运行态表和关键字段必须真实存在，不能只相信迁移记录。
	statements := []string{
		"SELECT revision,epoch FROM config_revision WHERE id=1",
		"SELECT id,game_id,region_id,env_type,node_type FROM config_node LIMIT 0",
		"SELECT revision,node_id,operation FROM config_node_change_log LIMIT 0",
		"SELECT id,code,webhook_secret FROM config_subscriber LIMIT 0",
		"SELECT acknowledged_revision,acknowledged_epoch FROM config_subscriber_delivery_state LIMIT 0",
		"SELECT id,game_id,code,prod_server_group_id,test_server_group_id,review_server_group_id FROM config_channel_app LIMIT 0",
        "SELECT id,code,name,scope_type,description,enabled FROM sdk_service LIMIT 0",
        "SELECT id,service_id,target_id,env,enabled,config FROM sdk_service_binding LIMIT 0",
	}
	{
		statements = append(statements, "SELECT id,appid,game_id,env,game_order_id,amount,attach,game_notify_url,platform_order_no,paid_at,payment_status,notify_status,notify_attempts,last_notify_at,last_notify_error,notify_success_at,next_notify_at,created_at,updated_at FROM game_order LIMIT 0")
	}
	for _, statement := range statements {
		rows, err := conn.QueryContext(ctx, statement)
		if err != nil {
			return fmt.Errorf("数据库表结构校验失败: %w", err)
		}
		if err := rows.Close(); err != nil {
			return err
		}
	}
	for _, index := range []struct {table, name string} {
        {"sdk_service", "uk_sdk_service_code"},
        {"sdk_service_binding", "uk_sdk_service_binding"},
    } {
        var exists int
        if err := conn.QueryRowContext(ctx,`
SELECT COUNT(*) FROM information_schema.statistics
WHERE table_schema=DATABASE() AND table_name=? AND index_name=? AND non_unique=0`,
            index.table,index.name).Scan(&exists);err!=nil{return err}
        if exists==0{return fmt.Errorf("缺少 SDK 服务唯一索引 %s",index.name)}
    }
    for _, obsolete := range []string{"config_channel_app_binding", "config_change_log", "config_delivery_state"} {
		exists, err := hasTable(ctx, conn, obsolete)
		if err != nil {
			return err
		}
		if exists {
			return fmt.Errorf("旧版数据表 %s 仍然存在", obsolete)
		}
	}
	var indexCount int
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.statistics
		WHERE table_schema=DATABASE() AND table_name='config_channel_app'
		AND index_name='uq_channel_app_code' AND column_name='code'
		AND seq_in_index=1 AND non_unique=0`).Scan(&indexCount); err != nil {
		return err
	}
	if indexCount != 1 {
		return errors.New("config_channel_app 表缺少 code 全局唯一索引")
	}
	for _, constraint := range []string{"chk_config_node_env_type", "fk_app_prod_group", "fk_app_test_group", "fk_app_review_group"} {
		var count int
		if err := conn.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM information_schema.table_constraints WHERE constraint_schema=DATABASE() AND constraint_name=?", constraint).Scan(&count); err != nil {
			return err
		}
		if count == 0 {
			return fmt.Errorf("缺少数据库约束 %s", constraint)
		}
	}
	var clause string
	if err := conn.QueryRowContext(ctx, `SELECT check_clause FROM information_schema.check_constraints
		WHERE constraint_schema=DATABASE() AND constraint_name='chk_config_node_env_type'`).Scan(&clause); err != nil {
		return err
	}
	if strings.Contains(clause, "production") || !strings.Contains(clause, "prod") {
		return errors.New("大区环境 CHECK 约束已过时")
	}
	{
		for _, index := range []struct { name, column string; position int }{
			{"uk_app_game_order", "appid", 1},
			{"uk_app_game_order", "game_order_id", 2},
			{"uk_platform_order", "platform_order_no", 1},
		} {
			var count int
			err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.statistics
				WHERE table_schema=DATABASE() AND table_name='game_order'
				AND index_name=? AND column_name=? AND seq_in_index=? AND non_unique=0`,
				index.name,index.column,index.position).Scan(&count)
			if err != nil { return err }
			if count != 1 { return fmt.Errorf("game_order 唯一索引 %s 缺少字段 %s",index.name,index.column) }
		}
	}
	return nil
}
