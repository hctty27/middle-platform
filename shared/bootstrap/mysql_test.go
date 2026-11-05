package bootstrap
import "testing"

func TestOpenMySQLConfig(t *testing.T){
 c:=MySQLConfig{DSN:"user:pass@tcp(127.0.0.1:3306)/db",MaxOpenConns:20,MaxIdleConns:10,ConnMaxLifetime:"30m"}
 db,err:=OpenMySQL(c)
 if err!=nil{t.Fatal(err)}
 if db.Stats().MaxOpenConnections!=20{t.Fatal(db.Stats())}
 _=db.Close()
 c.MaxIdleConns=21
 if _,err:=OpenMySQL(c);err==nil{t.Fatal("bad pool limits accepted")}
 c.MaxIdleConns=10;c.ConnMaxLifetime="not-a-duration"
 if _,err:=OpenMySQL(c);err==nil{t.Fatal("bad duration accepted")}
}
