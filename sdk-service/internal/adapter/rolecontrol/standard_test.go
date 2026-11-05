package rolecontrol

import (
    "context"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "testing"
    "time"

    "github.com/hctty27/middle-platform/sdk-service/internal/model"
    "github.com/hctty27/middle-platform/shared/signature"
)

func TestStandardExecute(t *testing.T) {
    cases := []struct {
        name, action, reply string
        duration *int64
        status int
        wantErr bool
    }{
        {"永久封号", "close", `{"code":0,"message":"ok","data":null}`, numberPtr(0), 200, false},
        {"限时禁言", "lock", `{"code":0,"message":"ok","data":null}`, numberPtr(3600), 200, false},
        {"解封", "unClose", `{"code":0,"message":"ok","data":null}`, nil, 200, false},
        {"解禁言", "unLock", `{"code":0,"message":"ok","data":null}`, nil, 200, false},
        {"业务失败", "close", `{"code":1,"message":"fail","data":null}`, numberPtr(1), 200, true},
        {"缺失 data", "close", `{"code":0,"message":"ok"}`, numberPtr(1), 200, true},
        {"错误 data", "close", `{"code":0,"message":"ok","data":true}`, numberPtr(1), 200, true},
        {"上游 HTTP 故障", "close", `{"code":0,"message":"ok","data":null}`, numberPtr(1), 503, true},
        {"非 JSON 响应", "close", "invalid", numberPtr(1), 200, true},
    }
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            game := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                var in model.RoleControlRequest
                if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
                    t.Errorf("无法解析请求: %v", err)
                }
                if in.Type != tc.action || in.RoleID != "r1" || r.Method != http.MethodPost ||
                    r.Header.Get("x-app-id") != "110006" {
                    t.Errorf("游戏请求参数错误: %+v", in)
                }
                if (in.Duration == nil) != (tc.duration == nil) ||
                    (tc.duration != nil && *in.Duration != *tc.duration) {
                    t.Errorf("duration 传递错误: %+v", in)
                }
                values := append([]string{"game-secret", "110006", r.Header.Get("x-timestamp"),
                    r.Header.Get("x-nonce")}, in.SignatureValues()...)
                if signature.SignValues(values...) != r.Header.Get("x-signature") {
                    t.Error("游戏请求签名错误")
                }
                w.WriteHeader(tc.status)
                _, _ = w.Write([]byte(tc.reply))
            }))
            defer game.Close()
            input := model.RoleControlRequest{Type: tc.action, RoleID: "r1", Duration: tc.duration}
            err := (&Standard{Client: &http.Client{Timeout: 2 * time.Second}}).Execute(
                context.Background(), model.Target{
                    RoleControlURL: game.URL, AppID: "110006", GameSecret: "game-secret",
                }, input)
            if (err != nil) != tc.wantErr {
                t.Fatalf("操作结果: %v, wantErr=%v", err, tc.wantErr)
            }
        })
    }
}

func TestRegistryCustom(t *testing.T) {
    standard, custom := &Standard{Client: &http.Client{}}, &Standard{Client: &http.Client{}}
    registry, err := NewRegistry(map[string]Adapter{"standard": standard, "game_c": custom})
    if err != nil { t.Fatal(err) }
    if got, err := registry.Get("game_c"); err != nil || got != custom {
        t.Fatalf("自定义适配器读取失败: %v", err)
    }
    if _, err := registry.Get("unknown"); err == nil {
        t.Fatal("找不到适配器时不允许自动回退")
    }
}

func numberPtr(value int64) *int64 { return &value }
