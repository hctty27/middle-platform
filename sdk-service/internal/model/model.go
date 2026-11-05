// Package model 定义 SDK Service 的统一业务模型。
package model

import "errors"

var (
    ErrNotFound = errors.New("渠道包不存在或未绑定环境")
    ErrDisabled = errors.New("当前渠道环境未启用对应服务")
    ErrInvalidConfig = errors.New("SDK 服务配置不合法")
    ErrAdapterMissing = errors.New("SDK 服务适配器未注册")
    ErrUpstream = errors.New("游戏服务请求失败")
)

type Target struct {
    AppID string
    Env string
    GameCode string
    UserCheckURL string
    RoleQueryURL string
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

type RoleQueryRequest struct {
    OpenID           string `json:"open_id,omitempty"`
    RoleID           string `json:"role_id,omitempty"`
    OriginalServerID string `json:"original_server_id,omitempty"`
    CurrentServerID  string `json:"current_server_id,omitempty"`
    Extension        string `json:"extension,omitempty"`
}

func (r RoleQueryRequest) SignatureValues() []string {
    return []string{r.OpenID, r.RoleID, r.OriginalServerID, r.CurrentServerID, r.Extension}
}

func (r RoleQueryRequest) HasIdentityFilter() bool {
    return r.OpenID != "" || r.RoleID != "" || r.OriginalServerID != "" || r.CurrentServerID != ""
}

// Role 保持平台原始字段名称及类型；可选数值缺省时不输出。
type Role struct {
    OpenID             string `json:"open_id,omitempty"`
    RoleID             string `json:"role_id"`
    RoleName           string `json:"role_name"`
    Level              string `json:"level"`
    Power              *int64 `json:"power,omitempty"`
    VIPLevel           *int64 `json:"vip_level,omitempty"`
    TotalAmount        *int64 `json:"total_amount,omitempty"`
    OriginalServerID   string `json:"original_server_id"`
    OriginalServerName string `json:"original_server_name"`
    CurrentServerID    string `json:"current_server_id"`
    CurrentServerName  string `json:"current_server_name"`
    CreatedAt          *int64 `json:"created_at"`
    LevelUpdatedAt     *int64 `json:"level_updated_at"`
    LastOfflineAt      *int64 `json:"last_offline_at"`
    LastLoginAt        *int64 `json:"last_login_at"`
    ProfessionID       string `json:"profession_id,omitempty"`
    ProfessionName     string `json:"profession_name,omitempty"`
    LoginDays          *int64 `json:"login_days,omitempty"`
    PalyTime           *int64 `json:"paly_time,omitempty"`
}

func (r Role) Valid() bool {
    return r.RoleID != "" && r.RoleName != "" && r.Level != "" &&
        r.OriginalServerID != "" && r.OriginalServerName != "" &&
        r.CurrentServerID != "" && r.CurrentServerName != "" &&
        r.CreatedAt != nil && r.LevelUpdatedAt != nil &&
        r.LastOfflineAt != nil && r.LastLoginAt != nil
}
