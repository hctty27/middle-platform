package api

import (
    "context"
    "encoding/json"
    "errors"
    "io"
    "net/http"
    "strconv"
    "strings"

    "github.com/hctty27/middle-platform/sdk-service/internal/model"
    "github.com/hctty27/middle-platform/sdk-service/internal/platform"
    "github.com/hctty27/middle-platform/shared/logging"
)

type ServerQuerier interface {
    Query(*http.Request, string, string, model.ServerQueryRequest) (model.ServerQueryResult, error)
}

var errInvalidServerQuery = errors.New("区服查询参数不合法")

func parseServerBody(w http.ResponseWriter, r *http.Request) (model.ServerQueryRequest, error) {
    input := model.ServerQueryRequest{PageSize: 1000}
    if strings.TrimSpace(r.URL.RawQuery) != "" ||
        !strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
        return input, errInvalidServerQuery
    }
    dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8*1024))
    start, err := dec.Token()
    if err != nil || start != json.Delim('{') {
        return input, errInvalidServerQuery
    }
    seen := make(map[string]bool, 5)
    for dec.More() {
        token, err := dec.Token()
        if err != nil {
            return input, errInvalidServerQuery
        }
        key, ok := token.(string)
        if !ok || seen[key] {
            return input, errInvalidServerQuery
        }
        seen[key] = true
        var raw json.RawMessage
        if err := dec.Decode(&raw); err != nil || len(raw) == 0 {
            return input, errInvalidServerQuery
        }
        switch key {
        case "original_server_id", "current_server_id", "zonekey":
            if raw[0] != '"' {
                return input, errInvalidServerQuery
            }
            var value string
            if err := json.Unmarshal(raw, &value); err != nil || len(value) > 256 {
                return input, errInvalidServerQuery
            }
            switch key {
            case "original_server_id":
                input.OriginalServerID = value
            case "current_server_id":
                input.CurrentServerID = value
            case "zonekey":
                input.Zonekey = value
            }
        case "page", "page_size":
            var value int
            if err := json.Unmarshal(raw, &value); err != nil ||
                string(raw) != strconv.Itoa(value) || value < 1 {
                return input, errInvalidServerQuery
            }
            if key == "page" {
                if value > 1000000 {
                    return input, errInvalidServerQuery
                }
                input.Page = value
            } else {
                if value > 1000 {
                    return input, errInvalidServerQuery
                }
                input.PageSize = value
                input.PageSizeProvided = true
            }
        default:
            return input, errInvalidServerQuery
        }
    }
    end, err := dec.Token()
    if err != nil || end != json.Delim('}') {
        return input, errInvalidServerQuery
    }
    if _, err := dec.Token(); !errors.Is(err, io.EOF) || !seen["page"] {
        return input, errInvalidServerQuery
    }
    return input, nil
}

func serverQueryHandler(query ServerQuerier) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        env, appID := r.PathValue("env"), r.Header.Get("x-app-id")
        if (env != "prod" && env != "test" && env != "review") || !appIDPattern.MatchString(appID) {
            writeJSON(w, http.StatusBadRequest, response{Code: 400, Message: "环境或渠道 AppID 不合法"})
            return
        }
        input, err := parseServerBody(w, r)
        if err != nil {
            writeJSON(w, http.StatusBadRequest, response{Code: 400, Message: "区服查询参数不合法"})
            return
        }
        data, err := query.Query(r, env, appID, input)
        if err != nil {
            status, message := http.StatusServiceUnavailable, "区服查询服务暂不可用"
            switch {
            case errors.Is(err, model.ErrNotFound):
                status, message = http.StatusNotFound, "渠道包不存在或未绑定该环境"
            case errors.Is(err, model.ErrDisabled):
                status, message = http.StatusForbidden, "当前渠道环境未开启区服查询"
            case errors.Is(err, platform.ErrUnauthorized):
                status, message = http.StatusUnauthorized, "平台请求签名验证失败"
            case errors.Is(err, model.ErrUpstream):
                status, message = http.StatusBadGateway, "游戏区服查询接口调用失败"
                if errors.Is(err, context.DeadlineExceeded) {
                    status, message = http.StatusGatewayTimeout, "游戏区服查询接口请求超时"
                }
            case errors.Is(err, model.ErrInvalidConfig), errors.Is(err, model.ErrAdapterMissing):
                status, message = http.StatusInternalServerError, "区服查询服务配置不合法"
            }
            logging.FromContext(r.Context()).WarnContext(r.Context(),
                "区服查询失败", "app_id", appID, "env", env, "status", status, "error", err)
            writeJSON(w, status, response{Code: status, Message: message})
            return
        }
        if data.List == nil {
            data.List = []model.ServerInfo{}
        }
        writeJSON(w, http.StatusOK, response{Code: 0, Message: "ok", Data: data})
    }
}
