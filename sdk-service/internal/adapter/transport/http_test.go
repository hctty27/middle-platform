package transport

import (
    "context"
    "errors"
    "io"
    "net/http"
    "net/http/httptest"
    "testing"

    "github.com/hctty27/middle-platform/sdk-service/internal/model"
    "github.com/hctty27/middle-platform/shared/signature"
)

func TestPostJSONPreservesSignatureFields(t *testing.T) {
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        body, _ := io.ReadAll(r.Body)
        if r.Method != http.MethodPost || string(body) != `{"role_id":"1"}` ||
            r.Header.Get("Content-Type") != "application/json" ||
            r.Header.Get("x-app-id") != "app" {
            t.Fatalf("request mismatch: %s", body)
        }
        expected := signature.SignValues("game-key", "app", r.Header.Get("x-timestamp"),
            r.Header.Get("x-nonce"), "1")
        if r.Header.Get("x-signature") != expected {
            t.Fatal("signature mismatch")
        }
        _, _ = w.Write([]byte(`{"code":0}`))
    }))
    defer server.Close()
    raw, err := PostJSON(context.Background(), server.Client(), server.URL, model.Target{
        AppID: "app", GameSecret: "game-key",
    }, []byte(`{"role_id":"1"}`), []string{"1"}, 1024)
    if err != nil || string(raw) != `{"code":0}` {
        t.Fatalf("response=%s err=%v", raw, err)
    }
}

func TestPostJSONResponseLimits(t *testing.T) {
    for _, tc := range []struct {
        name string
        status int
        body string
    }{
        {"too long", 200, "123456"},
        {"http error", 503, "{}"},
    } {
        t.Run(tc.name, func(t *testing.T) {
            server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                w.WriteHeader(tc.status)
                _, _ = w.Write([]byte(tc.body))
            }))
            defer server.Close()
            _, err := PostJSON(context.Background(), server.Client(), server.URL,
                model.Target{}, []byte("{}"), nil, 5)
            if !errors.Is(err, model.ErrUpstream) {
                t.Fatalf("expected upstream error, got %v", err)
            }
        })
    }
}
