package bootstrap

import(
 "os"
 "path/filepath"
 "testing"
)

func TestStrictYAML(t *testing.T){
 type C struct {Name string `yaml:"name"`}
 for _,tc:=range []struct{name,raw string;fail bool}{
  {"valid","name: ok\n",false},
  {"unknown","name: ok\nextra: 1\n",true},
  {"double","name: ok\n---\nname: bad\n",true},
 }{
  t.Run(tc.name,func(t *testing.T){
   path:=filepath.Join(t.TempDir(),"config.yaml")
   if err:=os.WriteFile(path,[]byte(tc.raw),0600);err!=nil{t.Fatal(err)}
   var cfg C
   err:=ReadYAML(path,&cfg)
   if (err!=nil)!=tc.fail{t.Fatalf("config=%+v err=%v",cfg,err)}
  })
 }
}

func TestOverrideString(t *testing.T) {
 t.Setenv("BOOTSTRAP_TEST_NAME", "")
 name := "default"
 OverrideString("BOOTSTRAP_TEST_NAME", &name)
 if name != "" { t.Fatal("环境变量空值应覆盖原配置") }
 OverrideString("BOOTSTRAP_TEST_NOT_SET", &name)
 if name != "" { t.Fatal("不存在的环境变量不应修改值") }
}
