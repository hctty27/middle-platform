package usercheck

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"hitall.com/middle-platform/sdk-service/internal/model"
	"hitall.com/middle-platform/shared/signature"
)

func TestStandardUserCheck(t *testing.T) {
	for _, item := range []struct {
		name      string
		body      string
		exists    bool
		wantError bool
	}{
		{"存在", `{"code":0,"data":{"exists":true}}`, true, false},
		{"不存在", `{"code":0,"data":{"exists":false}}`, false, false},
		{"缺少字段", `{"code":0,"data":{}}`, false, true},
		{"游戏失败", `{"code":500,"data":{"exists":false}}`, false, true},
		{"非法响应", `not-json`, false, true},
	} {
		t.Run(item.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.Header.Get("x-app-id") != "110006" {
					t.Error("游戏请求方法或 AppID 错误")
				}
				raw, readErr := io.ReadAll(r.Body)
				if readErr != nil {
					t.Error(readErr)
				}
				var in struct {
					OpenID string `json:"open_id"`
				}
				if err := json.Unmarshal(raw, &in); err != nil || in.OpenID != "u100" {
					t.Errorf("游戏请求参数错误: %v, %+v", err, in)
				}
				if !signature.VerifyGameHMAC(r, []byte("game-secret"), "110006", raw, time.Now()) {
					t.Error("游戏请求签名错误")
				}
				_, _ = w.Write([]byte(item.body))
			}))
			defer server.Close()
			adapter := &Standard{Client: NewHTTPClient(0)}
			result, err := adapter.Check(context.Background(), model.Target{
				AppID: "110006", GameSecret: "game-secret", UserCheckURL: server.URL,
			}, model.UserCheckRequest{OpenID: "u100"})
			if (err != nil) != item.wantError || (!item.wantError && result.Exists != item.exists) {
				t.Fatalf("结果不正确: %+v, %v", result, err)
			}
		})
	}
}

func TestRegistryUnknown(t *testing.T) {
	registry, err := NewRegistry(map[string]Adapter{"standard": &Standard{Client: NewHTTPClient(0)}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Get("unknown"); err == nil {
		t.Fatal("未知适配器应该报错")
	}
}
