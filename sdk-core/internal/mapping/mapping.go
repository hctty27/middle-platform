// Package mapping 提供按 HTTP 路由配置的全局响应字段映射。
package mapping

import (
    "bytes"
    _ "embed"
    "encoding/json"
    "errors"
    "fmt"
    "io"
    "strings"
)

// RouteCoreConfig 同时用于接口注册和映射查询，避免路由名称漂移。
const RouteCoreConfig = "GET /api/core/{env}/{appid}"

var ErrMissingField = errors.New("response mapping field is missing")

//go:embed response-mapping.json
var defaultMapping []byte

type fieldRule struct {
    target string
    source string
    segments []string
}

// Resolver 在启动时编译映射规则，请求期间只读且可并发使用。
type Resolver struct {
    routes map[string][]fieldRule
}

func LoadDefault() (*Resolver, error) {
    return Load(defaultMapping)
}

// Load 校验接口名、响应字段和来源路径，不支持执行 SQL 或表达式。
func Load(raw []byte) (*Resolver, error) {
    decoder := json.NewDecoder(bytes.NewReader(raw))
    var definitions map[string]map[string]string
    if err := decoder.Decode(&definitions); err != nil {
        return nil, fmt.Errorf("decode response mapping: %w", err)
    }
    if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
        return nil, errors.New("response mapping must contain one JSON object")
    }
    if len(definitions) == 0 {
        return nil, errors.New("response mapping has no routes")
    }
    compiled := make(map[string][]fieldRule, len(definitions))
    for route, fields := range definitions {
        parts := strings.Fields(route)
        if len(parts) != 2 || parts[0] == "" || !strings.HasPrefix(parts[1], "/") || strings.Join(parts," ") != route {
            return nil, fmt.Errorf("invalid route pattern %q", route)
        }
        if len(fields) == 0 {
            return nil, fmt.Errorf("route %q has no mapping fields", route)
        }
        rules := make([]fieldRule, 0, len(fields))
        for target, source := range fields {
            if strings.TrimSpace(target) != target || target == "" {
                return nil, fmt.Errorf("invalid response field %q for %s", target, route)
            }
            segments, err := parsePath(source)
            if err != nil {
                return nil, fmt.Errorf("route %s field %s: %w", route, target, err)
            }
            rules = append(rules, fieldRule{target:target, source:source, segments:segments})
        }
        compiled[route] = rules
    }
    return &Resolver{routes:compiled},nil
}

// Require 确保已注册的实际路由在映射文件中有规则，缺失时阻止服务启动。
func (r *Resolver) Require(route string) error {
    if r == nil || len(r.routes[route]) == 0 {
        return fmt.Errorf("missing response mapping for route %s", route)
    }
    return nil
}

func parsePath(source string) ([]string,error) {
    segments:=strings.Split(source,".")
    for _,segment:=range segments {
        if segment=="" || strings.TrimSpace(segment)!=segment {
            return nil,fmt.Errorf("invalid source path %q",source)
        }
    }
    switch source {
    case "env","channel_app.id","channel_app.code","channel_app.name",
         "game.code","region.code","server_group.code":
        return segments,nil
    }
    if len(segments)>=3 && (segments[0]=="channel_app" || segments[0]=="server_group") &&
        segments[1]=="config" {
        return segments,nil
    }
    return nil,fmt.Errorf("unsupported source path %q",source)
}

// Resolve 把业务层构建的数据上下文映射为单个接口的 data JSON。
func (r *Resolver) Resolve(route string, source map[string]any) (map[string]any,error) {
    if err:=r.Require(route);err!=nil { return nil,err }
    rules:=r.routes[route]
    result:=make(map[string]any,len(rules))
    for _,rule:=range rules {
        var current any=source
        for _,segment:=range rule.segments {
            object,ok:=current.(map[string]any)
            if !ok {
                return nil,fmt.Errorf("%w: %s -> %s",ErrMissingField,rule.target,rule.source)
            }
            value,exists:=object[segment]
            if !exists || value==nil {
                return nil,fmt.Errorf("%w: %s -> %s",ErrMissingField,rule.target,rule.source)
            }
            current=value
        }
        result[rule.target]=current
    }
    return result,nil
}

// DecodeObject 读取数据库 JSON 时保留数字精度；不接受非对象类型。
func DecodeObject(raw []byte) (map[string]any,error) {
    decoder:=json.NewDecoder(bytes.NewReader(raw))
    decoder.UseNumber()
    var value map[string]any
    if err:=decoder.Decode(&value);err!=nil { return nil,err }
    if value==nil { return nil,errors.New("expected JSON object") }
    if err:=decoder.Decode(new(any));!errors.Is(err,io.EOF) {
        return nil,errors.New("invalid trailing JSON content")
    }
    return value,nil
}
