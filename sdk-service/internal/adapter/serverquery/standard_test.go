package serverquery

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

func TestStandardServerQuery(t *testing.T) {
    valid := `{"original_server_id":"1000","original_server_name":"原区服","current_server_id":"1001","current_server_name":"新区服","master_server_id":"1001","master_server_name":"主服","status":"1","opened_at":1760000000,"open_days":30,"close_at":0,"role_total":100,"role_online_total":20}`
    cases := []struct {
        name, body string
        total int64
        count int
        fail bool
    }{
        {"正常列表", `{"code":0,"message":"ok","data":{"total":1,"list":[` + valid + `]}}`, 1, 1, false},
        {"空分页", `{"code":0,"message":"ok","data":{"total":0,"list":[]}}`, 0, 0, false},
        {"业务失败", `{"code":500,"message":"failed","data":{"total":0,"list":[]}}`, 0, 0, true},
        {"数据为空", `{"code":0,"message":"ok","data":null}`, 0, 0, true},
        {"列表为空值", `{"code":0,"message":"ok","data":{"total":0,"list":null}}`, 0, 0, true},
        {"状态非字符串", `{"code":0,"message":"ok","data":{"total":1,"list":[{"status":1}]}}`, 0, 0, true},
        {"缺少必填字段", `{"code":0,"message":"ok","data":{"total":1,"list":[{"original_server_id":"1000"}]}}`, 0, 0, true},
        {"总数小于列表长度", `{"code":0,"message":"ok","data":{"total":0,"list":[` + valid + `]}}`, 0, 0, true},
        {"非法 JSON", `invalid`, 0, 0, true},
    }
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            game := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                var in model.ServerQueryRequest
                if err := json.NewDecoder(r.Body).Decode(&in); err != nil ||
                    in.Page != 2 || in.PageSize != 1000 || in.Zonekey != "zone" {
                    t.Errorf("游戏区服查询字段错误: %v %+v", err, in)
                }
                if r.Method != http.MethodPost || r.Header.Get("x-app-id") != "110006" {
                    t.Error("请求方式或 AppID 错误")
                }
                values := append([]string{"game-secret", "110006", r.Header.Get("x-timestamp"), r.Header.Get("x-nonce")}, in.ForwardSignatureValues()...)
                if signature.SignValues(values...) != r.Header.Get("x-signature") {
                    t.Error("游戏区服查询签名错误")
                }
                _, _ = w.Write([]byte(tc.body))
            }))
            defer game.Close()
            adapter := &Standard{Client: &http.Client{Timeout: 2 * time.Second}}
            got, err := adapter.Query(context.Background(), model.Target{
                AppID: "110006", GameSecret: "game-secret", ServerQueryURL: game.URL,
            }, model.ServerQueryRequest{Page: 2, PageSize: 1000, Zonekey: "zone"})
            if (err != nil) != tc.fail || (!tc.fail && (got.Total != tc.total || len(got.List) != tc.count)) {
                t.Fatalf("区服查询结果错误: %+v, %v", got, err)
            }
        })
    }
}

func TestRegistryCustom(t *testing.T) {
    first := &Standard{Client: &http.Client{}}
    custom := &Standard{Client: &http.Client{}}
    registry, err := NewRegistry(map[string]Adapter{"standard": first, "game_a": custom})
    if err != nil { t.Fatal(err) }
    got, err := registry.Get("game_a")
    if err != nil || got != custom { t.Fatalf("自定义适配器未找到: %v", err) }
    if _, err := registry.Get("missing"); err == nil { t.Fatal("未注册适配器不应静默回退") }
}
