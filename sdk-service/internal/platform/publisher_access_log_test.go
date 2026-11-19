package platform

import (
    "bytes"
    "context"
    "log/slog"
    "net/http"
    "net/http/httptest"
    "strings"
    "sync/atomic"
    "testing"
    "time"
)

func TestPlatformAccessSwitchKeepsFailuresAndSlowCalls(t *testing.T) {
    var logs bytes.Buffer
    previous := slog.Default()
    slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
    defer slog.SetDefault(previous)

    var httpStatus atomic.Int32
    httpStatus.Store(http.StatusOK)
    var responseDelayNanos atomic.Int64
    platformServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
        if delay := time.Duration(responseDelayNanos.Load()); delay > 0 {
            time.Sleep(delay)
        }
        w.WriteHeader(int(httpStatus.Load()))
        _, _ = w.Write([]byte("{\"code\":0,\"data\":true}"))
    }))
    defer platformServer.Close()

    makePublisher := func(enabled bool, threshold time.Duration) *Publisher {
        t.Helper()
        p, err := NewPublisherWithOptions(
            platformServer.URL+"/report/event",
            platformServer.URL+"/game/subscribe/send-message",
            platformServer.URL+"/cp/user/check",
            PublisherOptions{MaxIdlePerHost: 32, AccessEnabled: enabled, SlowThreshold: threshold},
        )
        if err != nil {
            t.Fatal(err)
        }
        t.Cleanup(p.CloseIdleConnections)
        return p
    }
    const appID = "access-switch-unit-test"
    sendAll := func(p *Publisher) {
        t.Helper()
        if ok, err := p.CheckUser(context.Background(), appID, "secret", []byte("{}")); err != nil || !ok {
            t.Fatalf("user check: ok=%t err=%v", ok, err)
        }
        for _, code := range []string{"event_report", "subscribe_message"} {
            if err := p.Send(context.Background(), code, appID, "secret", []byte("{}")); err != nil {
                t.Fatal(err)
            }
        }
    }

    disabled := makePublisher(false, time.Hour)
    for i := 0; i < 10; i++ {
        sendAll(disabled)
    }
    if strings.Contains(logs.String(), "\"app_id\":\""+appID+"\"") {
        t.Fatalf("normal platform requests must not be logged when disabled: %s", logs.String())
    }

    enabled := makePublisher(true, time.Hour)
    sendAll(enabled)
    if count := strings.Count(logs.String(), "\"app_id\":\""+appID+"\""); count != 3 {
        t.Fatalf("enabling access logging should keep all three services, got %d: %s", count, logs.String())
    }

    httpStatus.Store(http.StatusBadGateway)
    if err := disabled.Send(context.Background(), "subscribe_message", appID, "secret", []byte("{}")); err == nil {
        t.Fatal("platform HTTP error expected")
    }
    if !strings.Contains(logs.String(), "\"level\":\"WARN\"") {
        t.Fatalf("platform failure must not be suppressed: %s", logs.String())
    }

    httpStatus.Store(http.StatusOK)
    responseDelayNanos.Store(int64(4 * time.Millisecond))
    disabled.slowThreshold = time.Millisecond
    if err := disabled.Send(context.Background(), "event_report", appID, "secret", []byte("{}")); err != nil {
        t.Fatal(err)
    }
    if !strings.Contains(logs.String(), "平台接口请求过慢") {
        t.Fatalf("slow platform success must be logged: %s", logs.String())
    }
}
