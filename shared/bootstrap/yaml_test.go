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

func TestLayeredYAML(t *testing.T){
 dir:=t.TempDir()
 base:=filepath.Join(dir,"config.yaml")
 env:=filepath.Join(dir,"config-dev.yaml")
 _=os.WriteFile(base,[]byte("name: base\nvalue: keep\n"),0600)
 _=os.WriteFile(env,[]byte("name: env\n"),0600)
 var c struct{Name string `yaml:"name"`;Value string `yaml:"value"`}
 if err:=ReadLayeredYAML(env,&c);err!=nil{t.Fatal(err)}
 if c.Name!="env"||c.Value!="keep"{t.Fatal(c)}
 t.Setenv("BOOTSTRAP_TEST_NAME","")
 OverrideString("BOOTSTRAP_TEST_NAME",&c.Name)
 if c.Name!=""{t.Fatal("empty environment value must override")}
}
