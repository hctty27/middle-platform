package api

import (
    "bytes"
    "context"
    "encoding/json"
    "errors"
    "io"
    "net/http"
    "strings"

    "github.com/hctty27/middle-platform/sdk-service/internal/auth"
    "github.com/hctty27/middle-platform/sdk-service/internal/model"
    "github.com/hctty27/middle-platform/sdk-service/internal/platform"
    "github.com/hctty27/middle-platform/shared/logging"
)

type EventReporter interface {
    ReportEvent(*http.Request, string, string, []byte) error
}

type SubscribeSender interface {
    SendSubscribe(*http.Request, string, string, []byte) error
}

var errInvalidOutbound = errors.New("上报或订阅消息参数不合法")
var errOutboundTooLarge = errors.New("事件或订阅消息请求体超过 64KiB")

func parseOutbound(w http.ResponseWriter, r *http.Request, code string) ([]byte, error) {
    if r.URL.RawQuery != "" ||
        !strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
        return nil, errInvalidOutbound
    }
    raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64*1024))
    if err != nil {
        var sizeError *http.MaxBytesError
        if errors.As(err, &sizeError) {
            return nil, errOutboundTooLarge
        }
        return nil, errInvalidOutbound
    }
    fields := map[string]json.RawMessage{}
    decoder := json.NewDecoder(bytes.NewReader(raw))
    start, err := decoder.Token()
    if err != nil || start != json.Delim('{') {
        return nil, errInvalidOutbound
    }
    for decoder.More() {
        token, err := decoder.Token()
        if err != nil {
            return nil, errInvalidOutbound
        }
        key, ok := token.(string)
        if !ok || fields[key] != nil {
            return nil, errInvalidOutbound
        }
        switch code {
        case "event_report":
            if key != "event" && key != "ext" {
                return nil, errInvalidOutbound
            }
        case "subscribe_message":
            if key != "open_id" && key != "tpl_id" && key != "data" && key != "ext" {
                return nil, errInvalidOutbound
            }
        default:
            return nil, errInvalidOutbound
        }
        var value json.RawMessage
        if decoder.Decode(&value) != nil {
            return nil, errInvalidOutbound
        }
        fields[key] = value
    }
    end, err := decoder.Token()
    if err != nil || end != json.Delim('}') {
        return nil, errInvalidOutbound
    }
    if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
        return nil, errInvalidOutbound
    }
    if code == "event_report" {
        event, ok := readOutboundString(fields["event"], 128, true)
        if !ok || strings.TrimSpace(event) != event {
            return nil, errInvalidOutbound
        }
        ext := bytes.TrimSpace(fields["ext"])
        if len(ext) == 0 || ext[0] != '{' {
            return nil, errInvalidOutbound
        }
        return raw, nil
    }
    if _, ok := readOutboundString(fields["open_id"], 256, true); !ok {
        return nil, errInvalidOutbound
    }
    if _, ok := readOutboundString(fields["tpl_id"], 256, true); !ok {
        return nil, errInvalidOutbound
    }
    data, ok := readOutboundString(fields["data"], 32*1024, false)
    if !ok {
        return nil, errInvalidOutbound
    }
    if data != "" {
        var object map[string]json.RawMessage
        if json.Unmarshal([]byte(data), &object) != nil || object == nil {
            return nil, errInvalidOutbound
        }
    }
    if _, ok := readOutboundString(fields["ext"], 4*1024, false); !ok {
        return nil, errInvalidOutbound
    }
    return raw, nil
}

func readOutboundString(raw json.RawMessage, max int, required bool) (string, bool) {
    if len(raw) == 0 {
        return "", !required
    }
    if raw[0] != '"' {
        return "", false
    }
    var value string
    if json.Unmarshal(raw, &value) != nil || len(value) > max {
        return "", false
    }
    if required && strings.TrimSpace(value) == "" {
        return "", false
    }
    return value, true
}

func serveOutbound(w http.ResponseWriter, r *http.Request, code string,
    send func(*http.Request, string, string, []byte) error) {
    env, appID := r.PathValue("env"), r.Header.Get("x-app-id")
    if (env != "prod" && env != "test" && env != "review") || !appIDPattern.MatchString(appID) {
        writeJSON(w, 400, response{Code: 400, Message: "环境或渠道 AppID 不合法"})
        return
    }
    body, err := parseOutbound(w, r, code)
    if err != nil {
        if errors.Is(err, errOutboundTooLarge) {
            writeJSON(w, 413, response{Code: 413, Message: "请求体超过 64KiB"})
        } else {
            writeJSON(w, 400, response{Code: 400, Message: "事件上报或订阅消息参数不合法"})
        }
        return
    }
    if err := send(r, env, appID, body); err != nil {
        status, message := http.StatusServiceUnavailable, "渠道服务暂不可用"
        switch {
        case errors.Is(err, model.ErrNotFound):
            status, message = 404, "渠道包不存在或未绑定该环境"
        case errors.Is(err, model.ErrDisabled):
            status, message = 403, "当前渠道环境未启用对应服务"
        case errors.Is(err, auth.ErrUnauthorized):
            status, message = 401, "游戏请求签名校验失败"
        case errors.Is(err, model.ErrInvalidConfig):
            status, message = 422, "渠道服务配置不合法"
        case errors.Is(err, platform.ErrPublishEndpointMissing):
            status, message = 503, "未配置平台接收地址"
        case errors.Is(err, platform.ErrPublishRejected):
            status, message = 502, "平台消息处理失败"
            if errors.Is(err, context.DeadlineExceeded) {
                status, message = 504, "平台消息处理超时"
            }
        }
        logging.FromContext(r.Context()).WarnContext(r.Context(),
            "渠道上报服务执行失败", "service_code", code, "app_id", appID,
            "env", env, "status", status, "error", err)
        writeJSON(w, status, response{Code: status, Message: message})
        return
    }
    message := "ok"
    if code == "event_report" {
        message = "success"
    }
    writeJSON(w, 200, response{Code: 0, Message: message, Data: true})
}
