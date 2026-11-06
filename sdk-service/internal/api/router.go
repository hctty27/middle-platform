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

// UserChecker 定义用户核验 HTTP 接口依赖的业务能力。
type UserChecker interface {
    Check(*http.Request, string, string, model.UserCheckRequest) (model.UserCheckResult, error)
}

// Pinger 定义就绪探针使用的数据库连通性检查。
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

// parseBody 只接受包含单个 open_id 字符串的 JSON 对象，拒绝多余字段及尾随数据。
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

// Router 注册用户核验及可选的角色查询接口，兼容原有调用方式。
func Router(checker UserChecker, db Pinger, roleQueriers ...RoleQuerier) http.Handler {
    var role RoleQuerier
    if len(roleQueriers) > 0 {
        role = roleQueriers[0]
    }
    return RouterWithServices(checker, db, role, nil)
}

// RouterWithServices 在基础路由上注册可用的角色、区服及封解禁服务。
func RouterWithServices(checker UserChecker, db Pinger, role RoleQuerier, server ServerQuerier, controls ...RoleController) http.Handler {
    var control RoleController
    if len(controls) > 0 { control = controls[0] }
    return routerServices(checker, db, role, server, control, nil, nil)
}

// RouterWithPublishers 额外接入事件上报与订阅消息的转发服务。
func RouterWithPublishers(checker UserChecker, db Pinger, role RoleQuerier, server ServerQuerier,
    control RoleController, events EventReporter, subscriptions SubscribeSender) http.Handler {
    return routerServices(checker, db, role, server, control, events, subscriptions)
}

// routerServices 仅暴露已经注入实现的业务接口，并统一提供存活和就绪探针。
func routerServices(checker UserChecker, db Pinger, role RoleQuerier, server ServerQuerier,
    control RoleController, events EventReporter, subscriptions SubscribeSender) http.Handler {
    mux := http.NewServeMux()
    if role != nil {
        mux.HandleFunc("POST /api/service/role/query/{env}", roleQueryHandler(role))
    }
    if server != nil {
        mux.HandleFunc("POST /api/service/server/query/{env}", serverQueryHandler(server))
    }
    if control != nil {
        mux.HandleFunc("POST /api/service/role/control/{env}", roleControlHandler(control))
    }
    if events != nil {
        mux.HandleFunc("POST /api/service/events/{env}", func(w http.ResponseWriter, r *http.Request) {
            serveOutbound(w, r, "event_report", events.ReportEvent)
        })
    }
    if subscriptions != nil {
        mux.HandleFunc("POST /api/service/subscribe/{env}", func(w http.ResponseWriter, r *http.Request) {
            serveOutbound(w, r, "subscribe_message", subscriptions.SendSubscribe)
        })
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
