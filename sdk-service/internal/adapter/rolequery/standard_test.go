package rolequery

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

func TestStandardRoleQuery(t *testing.T) {
    validRole := `{"role_id":"100","role_name":"角色","level":"88","original_server_id":"1","original_server_name":"原服","current_server_id":"2","current_server_name":"合服","created_at":0,"level_updated_at":0,"last_offline_at":0,"last_login_at":0}`
    cases := []struct {
        name, body string
        count int
        fail bool
    }{
        {"有结果", `{"code":0,"message":"ok","data":[` + validRole + `]}`, 1, false},
        {"空结果", `{"code":0,"message":"ok","data":[]}`, 0, false},
        {"业务失败", `{"code":500,"data":[]}`, 0, true},
        {"缺少必填", `{"code":0,"data":[{"role_id":"1"}]}`, 0, true},
        {"等级类型错误", `{"code":0,"data":[{"role_id":"1","level":88}]}`, 0, true},
        {"空数据", `{"code":0,"data":null}`, 0, true},
        {"非法 JSON", `not-json`, 0, true},
    }
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            game := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                if r.Method != http.MethodPost || r.Header.Get("x-app-id") != "110006" {
                    t.Error("请求方法或 AppID 错误")
                }
                var body model.RoleQueryRequest
                if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.RoleID != "100" {
                    t.Errorf("游戏请求字段不正确: %v %+v", err, body)
                }
                values := append([]string{"game-secret", "110006", r.Header.Get("x-timestamp"), r.Header.Get("x-nonce")}, body.SignatureValues()...)
                if signature.SignValues(values...) != r.Header.Get("x-signature") {
                    t.Error("角色查询签名错误")
                }
                _, _ = w.Write([]byte(tc.body))
            }))
            defer game.Close()
            adapter := &Standard{Client: &http.Client{Timeout: 2 * time.Second}}
            roles, err := adapter.Query(context.Background(), model.Target{
                AppID: "110006", GameSecret: "game-secret", RoleQueryURL: game.URL,
            }, model.RoleQueryRequest{RoleID: "100"})
            if (err != nil) != tc.fail || (!tc.fail && len(roles) != tc.count) {
                t.Fatalf("角色查询结果错误: len=%d err=%v", len(roles), err)
            }
        })
    }
}

func TestRegistryExtensible(t *testing.T) {
    standard := &Standard{Client: &http.Client{}}
    custom := &Standard{Client: &http.Client{}}
    registry, err := NewRegistry(map[string]Adapter{"standard": standard, "special_game": custom})
    if err != nil { t.Fatal(err) }
    got, err := registry.Get("special_game")
    if err != nil || got != custom {
        t.Fatalf("自定义适配器不能选择: %v", err)
    }
    if _, err := registry.Get("missing"); err == nil {
        t.Fatal("没有注册的适配器不能静默回退")
    }
}
