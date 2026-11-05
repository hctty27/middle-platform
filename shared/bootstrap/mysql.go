package bootstrap

import (
 "database/sql"
 "errors"
 "fmt"
 "time"
 "github.com/go-sql-driver/mysql"
)

type MySQLConfig struct{
 DSN string
 MaxOpenConns int
 MaxIdleConns int
 ConnMaxLifetime string
}

// OpenMySQL 保持 SDK Core / Service 现有的超时、UTC 与连接池行为。
func OpenMySQL(cfg MySQLConfig)(*sql.DB,error){
 if cfg.MaxOpenConns<1||cfg.MaxIdleConns<0||cfg.MaxIdleConns>cfg.MaxOpenConns {
  return nil,errors.New("数据库连接池配置不合法")
 }
 lifetime,err:=time.ParseDuration(cfg.ConnMaxLifetime)
 if err!=nil{return nil,fmt.Errorf("数据库 connMaxLifetime 配置不合法: %w",err)}
 parsed,err:=mysql.ParseDSN(cfg.DSN)
 if err!=nil{return nil,fmt.Errorf("数据库 DSN 不合法: %w",err)}
 parsed.ParseTime=true
 parsed.Loc=time.UTC
 parsed.Timeout=5*time.Second
 parsed.ReadTimeout=10*time.Second
 parsed.WriteTimeout=10*time.Second
 if parsed.Params==nil{parsed.Params=make(map[string]string)}
 parsed.Params["time_zone"]="'+00:00'"
 db,err:=sql.Open("mysql",parsed.FormatDSN())
 if err!=nil{return nil,err}
 db.SetMaxOpenConns(cfg.MaxOpenConns)
 db.SetMaxIdleConns(cfg.MaxIdleConns)
 db.SetConnMaxLifetime(lifetime)
 return db,nil
}
