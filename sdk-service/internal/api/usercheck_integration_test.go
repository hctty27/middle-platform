package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"hitall.com/middle-platform/sdk-service/internal/model"
	"hitall.com/middle-platform/sdk-service/internal/platform"
	"hitall.com/middle-platform/sdk-service/internal/service"
	"hitall.com/middle-platform/shared/signature"
)

type checkRoutes struct{ missing, skip bool }

func (s *checkRoutes) Lookup(_ context.Context, appID, env string) (model.Target, error) {
	if s.missing {
		return model.Target{}, model.ErrNotFound
	}
	return model.Target{AppID: appID, Env: env, GameSecret: "game-secret", AppSecret: "app-secret", SkipUserCheck: s.skip}, nil
}

func TestUserCheckGameToPlatform(t *testing.T) {
	body := ` { "open_id": "user-1" } `
	result := `{"code":0,"data":true}`
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		raw, _ := io.ReadAll(r.Body)
		if r.URL.Path != "/cp/user/check" || string(raw) != body {
			t.Errorf("unexpected upstream request: %s %s", r.URL.Path, raw)
		}
		want := signature.SignValues("app-secret", "app1", r.Header.Get("x-timestamp"), r.Header.Get("x-nonce"))
		if r.Header.Get("x-signature") != want {
			t.Error("incorrect platform signature")
		}
		_, _ = w.Write([]byte(result))
	}))
	defer upstream.Close()
	publisher, err := platform.NewPublisher("", "", upstream.URL+"/cp/user/check")
	if err != nil {
		t.Fatal(err)
	}
	routes := &checkRoutes{}
	handler := Router(&service.UserCheckService{Routes: routes, Sender: publisher}, fakeDB{})
	request := func(valid bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/api/service/user/check/test", strings.NewReader(body))
		ts, nonce := strconv.FormatInt(time.Now().Unix(), 10), "0011223344556677"
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("x-app-id", "app1")
		r.Header.Set("x-timestamp", ts)
		r.Header.Set("x-nonce", nonce)
		sig := signature.SignGameRequest([]byte("game-secret"), r.Method, r.URL.EscapedPath(), "app1", ts, nonce, []byte(body))
		if !valid {
			sig = strings.Repeat("0", len(sig))
		}
		r.Header.Set("x-signature", sig)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	w := request(true)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"data":true`) || calls != 1 {
		t.Fatalf("success: %d %s calls=%d", w.Code, w.Body.String(), calls)
	}
	result = `{"code":1021003,"message":"用户信息不存在","data":null}`
	w = request(true)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"code":1,`) || calls != 2 {
		t.Fatalf("missing: %d %s calls=%d", w.Code, w.Body.String(), calls)
	}
	result = `{"code":500,"data":null}`
	w = request(true)
	if w.Code != 502 || calls != 3 {
		t.Fatalf("upstream error: %d %s", w.Code, w.Body.String())
	}
	result = `{"code":0,"data":false}`
	w = request(true)
	if w.Code != 502 || calls != 4 {
		t.Fatalf("invalid response: %d %s", w.Code, w.Body.String())
	}
	w = request(false)
	if w.Code != 401 || calls != 4 {
		t.Fatalf("invalid signature: %d calls=%d", w.Code, calls)
	}
	routes.skip = true
	invalidBody := httptest.NewRequest(http.MethodPost, "/api/service/user/check/test", strings.NewReader(`{"open_id":""}`))
	invalidBody.Header.Set("Content-Type", "application/json")
	invalidBody.Header.Set("x-app-id", "app1")
	invalidResponse := httptest.NewRecorder()
	handler.ServeHTTP(invalidResponse, invalidBody)
	if invalidResponse.Code != 400 || calls != 4 {
		t.Fatalf("skipped with invalid body: %d calls=%d", invalidResponse.Code, calls)
	}
	w = request(true)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"data":true`) || calls != 4 {
		t.Fatalf("skipped: %d %s calls=%d", w.Code, w.Body.String(), calls)
	}
	w = request(false)
	if w.Code != 401 || calls != 4 {
		t.Fatalf("skipped with invalid signature: %d calls=%d", w.Code, calls)
	}
	routes.missing = true
	w = request(true)
	if w.Code != 404 || calls != 4 {
		t.Fatalf("missing channel: %d calls=%d", w.Code, calls)
	}
}
