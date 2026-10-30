// Package service 定义节点和业务请求的校验规则。
package service

import (
	"encoding/json"
	"strings"
	"github.com/hctty27/middle-platform/config-center/internal/model"
)

var ErrInvalid = model.ErrInvalid

// ValidateNode 验证节点名称、状态、JSON 配置及备注长度。
func ValidateNode(name string, status *int, config string, remark *string) error {
	if strings.TrimSpace(name) == "" || len([]rune(name)) > 128 ||
		status == nil || (*status != 0 && *status != 1) ||
		strings.TrimSpace(config) == "" || !json.Valid([]byte(config)) {
		return ErrInvalid
	}
	if remark != nil && len([]rune(*remark)) > 500 { return ErrInvalid }
	return nil
}

// ValidateCreate 验证节点类型及父节点标识符合四级层级规则。
func ValidateCreate(n model.CreateNodeRequest) error {
	if err := ValidateNode(n.Name, n.Status, n.Config, n.Remark); err != nil { return err }
	if strings.TrimSpace(n.Code) == "" || len([]rune(n.Code)) > 128 { return ErrInvalid }
	if err := ValidateEnvType(n.NodeType, n.EnvType); err != nil { return err }
	switch n.NodeType {
	case "game":
		if n.ParentID != 0 { return ErrInvalid }
	case "region", "server_group", "channel_app":
		if n.ParentID <= 0 { return ErrInvalid }
	default:
		return ErrInvalid
	}
	return nil
}


// ValidateEnvType 大区必须选择环境类型，其他节点不允许携带该字段。
func ValidateEnvType(nodeType string, envType *string) error {
    if nodeType != "region" {
        if envType != nil { return ErrInvalid }
        return nil
    }
    if envType == nil { return ErrInvalid }
    switch *envType {
    case "test", "review", "production": return nil
    default: return ErrInvalid
    }
}
