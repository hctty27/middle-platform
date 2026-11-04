// Package model 定义用户核验的统一业务模型。
package model

import "errors"

var (
    ErrNotFound = errors.New("渠道包不存在或未绑定环境")
    ErrDisabled = errors.New("当前渠道环境未启用用户核验")
    ErrInvalidConfig = errors.New("用户核验配置不合法")
    ErrAdapterMissing = errors.New("用户核验适配器未注册")
    ErrUpstream = errors.New("游戏核验请求失败")
)

type Target struct {
    AppID string
    Env string
    GameCode string
    UserCheckURL string
    AdapterName string
    AppSecret string
    GameSecret string
}

type UserCheckRequest struct {
    OpenID string
}

type UserCheckResult struct {
    Exists bool
}
