// Package api 定义平台统一 HTTP 入站接口。
package api

import (
    "context"
    "encoding/json"
    "errors"
    "io"
    "net/http"
    "regexp"
    "strings"
    "time"

    "github.com/hctty27/middle-platform/sdk-service/internal/model"
    "github.com/hctty27/middle-platform/sdk-service/internal/platform"
    "github.com/hctty27/middle-platform/shared/logging"
)

type UserChecker interface {
    Check(*http.Request, string, string, model.UserCheckRequest) (model.UserCheckResult, error)
}

type Pinger interface { PingContext(context.Context) error }

var appIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
var errInvalidBody = errors.New("请求体必须只包含非空 open_id 字符串")

type response struct {
    Code int `json:"code"`
    Message string `json:"message"`
    Data any `json:"data"`
}

func writeJSON(w http.ResponseWriter, status int, data response) {
    w.Header().Set("Content-Type", "application/json; charset=utf-8")
    w.Header().Set("Cache-Control", "no-store")
    w.WriteHeader(status)
    _ = json.NewEncoder(w).Encode(data)
}

func parseBody(w http.ResponseWriter, r *http.Request) (model.UserCheckRequest, error) {
    if strings.TrimSpace(r.URL.RawQuery) != "" || !strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
        return model.UserCheckRequest{}, errInvalidBody
    }
    dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8*1024))
    start, err := dec.Token()
    if err != nil || start != json.Delim('{') { return model.UserCheckRequest{}, errInvalidBody }
    if !dec.More() { return model.UserCheckRequest{}, errInvalidBody }
    key, err := dec.Token()
    if err != nil || key != "open_id" { return model.UserCheckRequest{}, errInvalidBody }
    var openID string
    if dec.Decode(&openID) != nil { return model.UserCheckRequest{}, errInvalidBody }
    if dec.More() { return model.UserCheckRequest{}, errInvalidBody }
    end, err := dec.Token()
    if err != nil || end != json.Delim('}') { return model.UserCheckRequest{}, errInvalidBody }
    if _, err := dec.Token(); !errors.Is(err, io.EOF) { return model.UserCheckRequest{}, errInvalidBody }
    if strings.TrimSpace(openID) == "" || len(openID) > 256 { return model.UserCheckRequest{}, errInvalidBody }
    return model.UserCheckRequest{OpenID: openID}, nil
}

func Router(checker UserChecker, db Pinger, roleQueriers ...RoleQuerier) http.Handler {
    mux := http.NewServeMux()
    if len(roleQueriers) > 0 && roleQueriers[0] != nil {
        mux.HandleFunc("POST /api/service/role/query/{env}", roleQueryHandler(roleQueriers[0]))
    }
    mux.HandleFunc("POST /api/service/user/check/{env}", func(w http.ResponseWriter, r *http.Request) {
        env := r.PathValue("env")
        appID := r.Header.Get("x-app-id")
        if (env != "prod" && env != "test" && env != "review") || !appIDPattern.MatchString(appID) {
            writeJSON(w, http.StatusBadRequest, response{Code: 400, Message: "环境或渠道 AppID 不合法"})
            return
        }
        input, err := parseBody(w, r)
        if err != nil {
            writeJSON(w, http.StatusBadRequest, response{Code: 400, Message: "用户核验参数不合法"})
            return
        }
        result, err := checker.Check(r, env, appID, input)
        if err != nil {
            status := http.StatusServiceUnavailable
            code, message := 503, "用户核验服务暂不可用"
            switch {
            case errors.Is(err, model.ErrNotFound):
                status, code, message = 404, 404, "渠道包不存在或未绑定该环境"
            case errors.Is(err, model.ErrDisabled):
                status, code, message = 403, 403, "当前渠道环境未开启用户核验"
            case errors.Is(err, platform.ErrUnauthorized):
                status, code, message = 401, 401, "平台请求签名验证失败"
            case errors.Is(err, model.ErrUpstream):
                status, code, message = 502, 502, "游戏核验接口调用失败"
                if errors.Is(err, context.DeadlineExceeded) {
                    status, code, message = 504, 504, "游戏核验接口请求超时"
                }
            }
            logging.FromContext(r.Context()).WarnContext(r.Context(),
                "用户核验失败", "app_id", appID, "env", env, "status", status, "error", err)
            writeJSON(w, status, response{Code: code, Message: message})
            return
        }
        if !result.Exists {
            writeJSON(w, http.StatusOK, response{Code: 1021003, Message: "用户信息不存在"})
            return
        }
        writeJSON(w, http.StatusOK, response{Code: 0, Message: "ok", Data: true})
    })
    mux.HandleFunc("GET /health/live", func(w http.ResponseWriter, _ *http.Request) {
        writeJSON(w, 200, response{Code: 0, Message: "UP", Data: true})
    })
    mux.HandleFunc("GET /health/ready", func(w http.ResponseWriter, r *http.Request) {
        ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
        defer cancel()
        if db.PingContext(ctx) != nil {
            writeJSON(w, http.StatusServiceUnavailable, response{Code: 503, Message: "DOWN"})
            return
        }
        writeJSON(w, 200, response{Code: 0, Message: "UP", Data: true})
    })
    return mux
}
