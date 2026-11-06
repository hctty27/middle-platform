package api

import (
    "encoding/json"
    "errors"
    "io"
    "net/http"
    "strings"

    "github.com/hctty27/middle-platform/sdk-service/internal/model"
    "github.com/hctty27/middle-platform/shared/logging"
)

// RoleQuerier 定义平台角色查询接口使用的业务能力。
type RoleQuerier interface {
    Query(*http.Request, string, string, model.RoleQueryRequest) ([]model.Role, error)
}

var errInvalidRoleBody = errors.New("角色查询请求需要至少一个身份条件，且只能包含平台约定的字符串字段")

// parseRoleBody 仅接受平台约定的查询字段，且至少包含一个身份筛选条件。
func parseRoleBody(w http.ResponseWriter, r *http.Request) (model.RoleQueryRequest, error) {
    var input model.RoleQueryRequest
    if strings.TrimSpace(r.URL.RawQuery) != "" ||
        !strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
        return input, errInvalidRoleBody
    }
    dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8*1024))
    start, err := dec.Token()
    if err != nil || start != json.Delim('{') {
        return input, errInvalidRoleBody
    }
    seen := make(map[string]bool, 5)
    for dec.More() {
        token, err := dec.Token()
        if err != nil {
            return input, errInvalidRoleBody
        }
        key, ok := token.(string)
        if !ok || seen[key] {
            return input, errInvalidRoleBody
        }
        seen[key] = true
        var raw json.RawMessage
        if err := dec.Decode(&raw); err != nil || len(raw) < 2 || raw[0] != '"' {
            return input, errInvalidRoleBody
        }
        var value string
        if err := json.Unmarshal(raw, &value); err != nil {
            return input, errInvalidRoleBody
        }
        if len(value) > 256 && key != "extension" || len(value) > 2048 {
            return input, errInvalidRoleBody
        }
        switch key {
        case "open_id":
            input.OpenID = value
        case "role_id":
            input.RoleID = value
        case "original_server_id":
            input.OriginalServerID = value
        case "current_server_id":
            input.CurrentServerID = value
        case "extension":
            input.Extension = value
        default:
            return input, errInvalidRoleBody
        }
    }
    end, err := dec.Token()
    if err != nil || end != json.Delim('}') {
        return input, errInvalidRoleBody
    }
    if _, err := dec.Token(); !errors.Is(err, io.EOF) {
        return input, errInvalidRoleBody
    }
    if strings.TrimSpace(input.OpenID) == "" && strings.TrimSpace(input.RoleID) == "" &&
        strings.TrimSpace(input.OriginalServerID) == "" && strings.TrimSpace(input.CurrentServerID) == "" {
        return input, errInvalidRoleBody
    }
    return input, nil
}

// roleQueryHandler 将角色查询结果转换为平台统一响应，空结果返回空数组。
func roleQueryHandler(query RoleQuerier) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        env, appID := r.PathValue("env"), r.Header.Get("x-app-id")
        if (env != "prod" && env != "test" && env != "review") || !appIDPattern.MatchString(appID) {
            writeJSON(w, http.StatusBadRequest, response{Code: 400, Message: "环境或渠道 AppID 不合法"})
            return
        }
        input, err := parseRoleBody(w, r)
        if err != nil {
            writeJSON(w, http.StatusBadRequest, response{Code: 400, Message: "角色查询参数不合法"})
            return
        }
        roles, err := query.Query(r, env, appID, input)
        if err != nil {
            status, message := mapServiceError(err, "角色查询")
            logging.FromContext(r.Context()).WarnContext(r.Context(),
                "角色查询失败", "app_id", appID, "env", env, "status", status, "error", err)
            writeJSON(w, status, response{Code: status, Message: message})
            return
        }
        if roles == nil {
            roles = []model.Role{}
        }
        writeJSON(w, http.StatusOK, response{Code: 0, Message: "ok", Data: roles})
    }
}
