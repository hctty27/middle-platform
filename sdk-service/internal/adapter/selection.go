// Package adapter 提供各游戏业务共用的适配器选择规则。
package adapter

import "strings"

const DefaultName = "standard"

// NameForService 只在指定非空适配器时覆盖默认实现。
// 所有后续游戏业务均复用此规则，避免每新增一个接口就要求配置适配器。
func NameForService(config map[string]string, serviceCode string) string {
    if name := strings.TrimSpace(config[serviceCode]); name != "" {
        return name
    }
    return DefaultName
}
