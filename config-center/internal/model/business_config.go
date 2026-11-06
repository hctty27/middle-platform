package model

import (
	"fmt"

	"github.com/hctty27/middle-platform/config-center/internal/configschema"
)

// ValidateBusinessConfig 使用与对外 Schema 同源的字段定义校验基础节点配置。
func ValidateBusinessConfig(nodeType, config string) error {
	if err := configschema.Validate(nodeType, config); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	return nil
}
