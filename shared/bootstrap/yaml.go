// Package bootstrap 提供中台服务共用的配置解析工具。
package bootstrap

import (
 "bytes"
 "errors"
 "fmt"
 "io"
 "os"
 "gopkg.in/yaml.v3"
)

func ReadYAML(path string, target any) error {
 raw,err:=os.ReadFile(path)
 if err!=nil{return fmt.Errorf("读取 YAML %s 失败: %w",path,err)}
 decoder:=yaml.NewDecoder(bytes.NewReader(raw))
 decoder.KnownFields(true)
 if err:=decoder.Decode(target);err!=nil{return fmt.Errorf("解析 YAML %s 失败: %w",path,err)}
 var extra any
 if err:=decoder.Decode(&extra);!errors.Is(err,io.EOF){return fmt.Errorf("YAML %s 必须只包含一份文档",path)}
 return nil
}

func OverrideString(key string,target *string){
 if value,ok:=os.LookupEnv(key);ok{*target=value}
}
