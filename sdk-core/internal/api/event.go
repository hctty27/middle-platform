package api

import (
    "encoding/json"
    "errors"
    "net/http"

    "github.com/hctty27/middle-platform/sdk-core/internal/auth"
    "github.com/hctty27/middle-platform/sdk-core/internal/model"
    "github.com/hctty27/middle-platform/sdk-core/internal/platform"
    "github.com/hctty27/middle-platform/sdk-core/internal/service"
    "github.com/hctty27/middle-platform/shared/logging"
)

// EventAPI 负责渠道事件上报的验签、开关检查及平台投递。
type EventAPI interface {
    Report(*http.Request, string, string, []byte) error
}

func eventError(w http.ResponseWriter, r *http.Request, err error) {
    status, message := http.StatusServiceUnavailable, "事件上报服务暂时不可用"
    switch {
    case errors.Is(err, service.ErrInvalidEvent), errors.Is(err, model.ErrInvalidRequest):
        status, message = http.StatusBadRequest, "事件上报参数不合法"
    case errors.Is(err, auth.ErrUnauthorized):
        status, message = http.StatusUnauthorized, "事件请求签名验证失败"
    case errors.Is(err, model.ErrNotFound):
        status, message = http.StatusNotFound, "渠道包不存在或未绑定该环境"
    case errors.Is(err, service.ErrServiceDisabled):
        status, message = http.StatusForbidden, "当前渠道环境未开启事件上报"
    case errors.Is(err, model.ErrInvalidConfig):
        status, message = http.StatusUnprocessableEntity, "事件上报所需配置不合法"
    case errors.Is(err, platform.ErrEventEndpointMissing):
        status, message = http.StatusServiceUnavailable, "未配置平台事件上报地址"
    case errors.Is(err, platform.ErrEventRejected):
        status, message = http.StatusBadGateway, "平台事件上报失败"
    }
    if status >= 500 {
        logging.FromContext(r.Context()).ErrorContext(r.Context(), "转发渠道事件失败", "error", err)
    }
    writeJSON(w, status, response{Code: status, Message: message})
}

func registerEventRoutes(mux *http.ServeMux, svc EventAPI) {
    mux.HandleFunc("POST /api/core/{env}/events", func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Cache-Control", "no-store")
        if r.URL.RawQuery != "" {
            eventError(w, r, service.ErrInvalidEvent)
            return
        }
        raw, err := readBody(w, r)
        if err != nil {
            writeJSON(w, http.StatusRequestEntityTooLarge,
                response{Code: 413, Message: "事件请求体不合法或超过 64KiB"})
            return
        }
        var event struct {
            Event string          `json:"event"`
            Ext json.RawMessage  `json:"ext"`
        }
        if err := decodeOrderJSON(raw, []string{"event", "ext"}, &event); err != nil {
            eventError(w, r, service.ErrInvalidEvent)
            return
        }
        if err := svc.Report(r, r.PathValue("env"), r.Header.Get("x-app-id"), raw); err != nil {
            eventError(w, r, err)
            return
        }
        writeJSON(w, http.StatusOK, response{Code: 0, Message: "success", Data: true})
    })
}
