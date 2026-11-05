package api

import (
    "encoding/json"
    "errors"
    "io"
    "net"
    "net/http"
    "strconv"
    "strings"

    "github.com/hctty27/middle-platform/sdk-service/internal/model"
    "github.com/hctty27/middle-platform/shared/logging"
)

type RoleController interface {
    Execute(*http.Request, string, string, model.RoleControlRequest) error
}

var errInvalidRoleControl = errors.New("封解禁参数不合法")

func parseRoleControlBody(w http.ResponseWriter, r *http.Request) (model.RoleControlRequest, error) {
    var input model.RoleControlRequest
    if strings.TrimSpace(r.URL.RawQuery) != "" ||
        !strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
        return input, errInvalidRoleControl
    }
    dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8*1024))
    token, err := dec.Token()
    if err != nil || token != json.Delim('{') {
        return input, errInvalidRoleControl
    }
    seen := make(map[string]bool, 9)
    for dec.More() {
        token, err := dec.Token()
        if err != nil {
            return input, errInvalidRoleControl
        }
        key, ok := token.(string)
        if !ok || seen[key] {
            return input, errInvalidRoleControl
        }
        seen[key] = true
        var raw json.RawMessage
        if dec.Decode(&raw) != nil || len(raw) == 0 {
            return input, errInvalidRoleControl
        }
        if key == "duration" {
            var value int64
            if json.Unmarshal(raw, &value) != nil || string(raw) != strconv.FormatInt(value, 10) || value < 0 {
                return input, errInvalidRoleControl
            }
            input.Duration = &value
            continue
        }
        if raw[0] != '"' {
            return input, errInvalidRoleControl
        }
        var value string
        if json.Unmarshal(raw, &value) != nil || len(value) > 2048 ||
            (key != "describe" && key != "extension" && len(value) > 256) {
            return input, errInvalidRoleControl
        }
        switch key {
        case "type":
            input.Type = value
        case "server_id":
            input.ServerID = value
        case "open_id":
            input.OpenID = value
        case "role_id":
            input.RoleID = value
        case "role_name":
            input.RoleName = value
        case "ip":
            input.IP = value
        case "describe":
            input.Describe = value
        case "extension":
            input.Extension = value
        default:
            return input, errInvalidRoleControl
        }
    }
    token, err = dec.Token()
    if err != nil || token != json.Delim('}') {
        return input, errInvalidRoleControl
    }
    if _, err := dec.Token(); !errors.Is(err, io.EOF) {
        return input, errInvalidRoleControl
    }
    switch input.Type {
    case "close", "unClose", "lock", "unLock":
    default:
        return input, errInvalidRoleControl
    }
    if strings.TrimSpace(input.OpenID) == "" && strings.TrimSpace(input.RoleID) == "" &&
        strings.TrimSpace(input.RoleName) == "" && strings.TrimSpace(input.IP) == "" {
        return input, errInvalidRoleControl
    }
    if input.IP != "" && net.ParseIP(input.IP) == nil {
        return input, errInvalidRoleControl
    }
    if (input.Type == "close" || input.Type == "lock") && input.Duration == nil {
        return input, errInvalidRoleControl
    }
    return input, nil
}

func roleControlHandler(control RoleController) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        env, appID := r.PathValue("env"), r.Header.Get("x-app-id")
        if (env != "prod" && env != "test" && env != "review") || !appIDPattern.MatchString(appID) {
            writeJSON(w, http.StatusBadRequest, response{Code: 400, Message: "环境或渠道 AppID 不合法"})
            return
        }
        input, err := parseRoleControlBody(w, r)
        if err != nil {
            writeJSON(w, http.StatusBadRequest, response{Code: 400, Message: "封解禁参数不合法"})
            return
        }
        if err := control.Execute(r, env, appID, input); err != nil {
            status, message := mapServiceError(err, "封解禁")
            logging.FromContext(r.Context()).WarnContext(r.Context(), "封解禁执行失败",
                "app_id", appID, "env", env, "type", input.Type, "status", status, "error", err)
            writeJSON(w, status, response{Code: status, Message: message})
            return
        }
        logging.FromContext(r.Context()).InfoContext(r.Context(), "封解禁执行成功",
            "app_id", appID, "env", env, "type", input.Type)
        writeJSON(w, http.StatusOK, response{Code: 0, Message: "ok"})
    }
}
