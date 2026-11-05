// Package model 定义 SDK Service 的统一业务模型。
package model

import (
    "errors"
    "strconv"
)

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
    ServerQueryURL string
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

type ServerQueryRequest struct {
    OriginalServerID string `json:"original_server_id,omitempty"`
    CurrentServerID  string `json:"current_server_id,omitempty"`
    Page             int    `json:"page"`
    PageSize         int    `json:"page_size"`
    Zonekey          string `json:"zonekey,omitempty"`
    PageSizeProvided bool   `json:"-"`
}

// SignatureValues 用于校验平台实际发来的请求，未提交的可选字段不参与签名。
func (r ServerQueryRequest) SignatureValues() []string {
    values := []string{r.OriginalServerID, r.CurrentServerID, strconv.Itoa(r.Page)}
    if r.PageSizeProvided {
        values = append(values, strconv.Itoa(r.PageSize))
    }
    return append(values, r.Zonekey)
}

// ForwardSignatureValues 用于中台发送给游戏，page_size 会显式发送默认值。
func (r ServerQueryRequest) ForwardSignatureValues() []string {
    return []string{r.OriginalServerID, r.CurrentServerID, strconv.Itoa(r.Page), strconv.Itoa(r.PageSize), r.Zonekey}
}

type ServerInfo struct {
    OriginalServerID   string `json:"original_server_id"`
    OriginalServerName string `json:"original_server_name"`
    CurrentServerID    string `json:"current_server_id"`
    CurrentServerName  string `json:"current_server_name"`
    MasterServerID     string `json:"master_server_id"`
    MasterServerName   string `json:"master_server_name"`
    Status             string `json:"status"`
    OpenedAt           *int64 `json:"opened_at"`
    OpenDays           *int64 `json:"open_days"`
    MergeAt            *int64 `json:"merge_at,omitempty"`
    MergeDays          *int64 `json:"merge_days,omitempty"`
    MergeCount         *int64 `json:"merge_count,omitempty"`
    CloseAt            *int64 `json:"close_at"`
    RoleTotal          *int64 `json:"role_total"`
    RoleOnlineTotal    *int64 `json:"role_online_total"`
}

func (s ServerInfo) Valid() bool {
    switch s.Status {
    case "0", "1", "3", "4":
    default:
        return false
    }
    return s.OriginalServerID != "" && s.OriginalServerName != "" &&
        s.CurrentServerID != "" && s.CurrentServerName != "" &&
        s.MasterServerID != "" && s.MasterServerName != "" &&
        s.OpenedAt != nil && *s.OpenedAt >= 0 &&
        s.OpenDays != nil && *s.OpenDays >= 0 &&
        s.CloseAt != nil && *s.CloseAt >= 0 &&
        s.RoleTotal != nil && *s.RoleTotal >= 0 &&
        s.RoleOnlineTotal != nil && *s.RoleOnlineTotal >= 0 &&
        (s.MergeAt == nil || *s.MergeAt >= 0) &&
        (s.MergeDays == nil || *s.MergeDays >= 0) &&
        (s.MergeCount == nil || *s.MergeCount >= 0)
}

type ServerQueryResult struct {
    Total int64        `json:"total"`
    List  []ServerInfo `json:"list"`
}
