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

// Target 保存已验证的渠道环境路由、上下游密钥及游戏接口地址。
type Target struct {
    AppID string
    Env string
    GameCode string
    UserCheckURL string
    RoleQueryURL string
    ServerQueryURL string
    RoleControlURL string
    AdapterName string
    AppSecret string
    GameSecret string
}

// UserCheckRequest 表示用户核验的身份参数。
type UserCheckRequest struct {
    OpenID string
}

// UserCheckResult 表示游戏侧用户是否存在。
type UserCheckResult struct {
    Exists bool
}

// RoleQueryRequest 表示平台支持的角色身份及区服筛选条件。
type RoleQueryRequest struct {
    OpenID           string `json:"open_id,omitempty"`
    RoleID           string `json:"role_id,omitempty"`
    OriginalServerID string `json:"original_server_id,omitempty"`
    CurrentServerID  string `json:"current_server_id,omitempty"`
    Extension        string `json:"extension,omitempty"`
}

// SignatureValues 按平台协议顺序返回角色查询的签名参数。
func (r RoleQueryRequest) SignatureValues() []string {
    return []string{r.OpenID, r.RoleID, r.OriginalServerID, r.CurrentServerID, r.Extension}
}

// HasIdentityFilter 判断是否提供至少一个角色身份或区服筛选条件。
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

// Valid 检查角色响应中平台要求的必填字段。
func (r Role) Valid() bool {
    return r.RoleID != "" && r.RoleName != "" && r.Level != "" &&
        r.OriginalServerID != "" && r.OriginalServerName != "" &&
        r.CurrentServerID != "" && r.CurrentServerName != "" &&
        r.CreatedAt != nil && r.LevelUpdatedAt != nil &&
        r.LastOfflineAt != nil && r.LastLoginAt != nil
}

// ServerQueryRequest 表示区服查询条件及分页字段的显式提交状态。
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

// ServerInfo 保持平台区服信息的原始字段及可选数值类型。
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

// Valid 校验区服状态、必要标识及非负统计数据。
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

// ServerQueryResult 返回区服总数及当前页列表。
type ServerQueryResult struct {
    Total int64        `json:"total"`
    List  []ServerInfo `json:"list"`
}

// RoleControlRequest 表示封号、解封、禁言及解禁言操作。
// Duration 使用指针区分未提交与显式传入零值。
type RoleControlRequest struct {
    Type      string `json:"type"`
    ServerID  string `json:"server_id,omitempty"`
    OpenID    string `json:"open_id,omitempty"`
    RoleID    string `json:"role_id,omitempty"`
    RoleName  string `json:"role_name,omitempty"`
    IP        string `json:"ip,omitempty"`
    Duration  *int64 `json:"duration,omitempty"`
    Describe  string `json:"describe,omitempty"`
    Extension string `json:"extension,omitempty"`
}

// SignatureValues 同时用于平台入站验签和游戏转发签名，显式的 duration=0 必须参与签名。
func (r RoleControlRequest) SignatureValues() []string {
    values := []string{r.Type, r.ServerID, r.OpenID, r.RoleID, r.RoleName, r.IP}
    if r.Duration != nil {
        values = append(values, strconv.FormatInt(*r.Duration, 10))
    }
    return append(values, r.Describe, r.Extension)
}
