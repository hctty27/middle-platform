package signature

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestGameHMACVector(t *testing.T) {
	body := []byte(`{"event":"登录","empty":"","absent":null,"ext":{"name":"角色","numbers":[1,false]}}`)
	got := SignGameRequest([]byte("secret"), "POST", "/api/service/events/test", "110006", "1793700000", "0123456789abcdef", body)
	const want = "26800b27ae8f852c0e423aaa8b3f5a78d20b98b25c23ad5a1f9d30cefc78b47c"
	if got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestGameHMACEncodedPath(t *testing.T) {
	now := time.Now()
	ts := strconv.FormatInt(now.Unix(), 10)
	body := []byte(`{"event":"登录"}`)
	r := httptest.NewRequest("POST", "/prefix%20space/api/service/events/test", nil)
	r.Header.Set("x-app-id", "110006")
	r.Header.Set("x-timestamp", ts)
	r.Header.Set("x-nonce", "0123456789abcdef")
	r.Header.Set("x-signature", SignGameRequest([]byte("secret"), r.Method, r.URL.EscapedPath(), "110006", ts, "0123456789abcdef", body))
	if !VerifyGameHMAC(r, []byte("secret"), "110006", body, now) {
		t.Fatal("encoded path rejected")
	}
	r.URL.Path = "/api/service/events/test"
	r.URL.RawPath = ""
	if VerifyGameHMAC(r, []byte("secret"), "110006", body, now) {
		t.Fatal("rewritten encoded path accepted")
	}
}

func TestGameHMACBindsRequest(t *testing.T) {
	now := time.Now()
	ts := strconv.FormatInt(now.Unix(), 10)
	nonce := "0123456789abcdef"
	body := []byte(`{"amount":100,"ext":{"角色":"一"}}`)
	makeRequest := func() *http.Request {
		r := httptest.NewRequest("POST", "/api/service/events/test", nil)
		r.Header.Set("x-app-id", "110006")
		r.Header.Set("x-timestamp", ts)
		r.Header.Set("x-nonce", nonce)
		r.Header.Set("x-signature", SignGameRequest([]byte("secret"), "POST", r.URL.EscapedPath(), "110006", ts, nonce, body))
		return r
	}
	r := makeRequest()
	if !VerifyGameHMAC(r, []byte("secret"), "110006", body, now) {
		t.Fatal("valid request rejected")
	}
	cases := []struct {
		name    string
		mutate  func(*http.Request)
		payload []byte
		app     string
		when    time.Time
	}{
		{"method", func(r *http.Request) { r.Method = "GET" }, body, "110006", now},
		{"path", func(r *http.Request) { r.URL.Path = "/api/service/events/prod" }, body, "110006", now},
		{"query", func(r *http.Request) { r.URL.RawQuery = "x=1" }, body, "110006", now},
		{"app", func(r *http.Request) { r.Header.Set("x-app-id", "other") }, body, "other", now},
		{"nonce", func(r *http.Request) { r.Header.Set("x-nonce", "1123456789abcdef") }, body, "110006", now},
		{"timestamp", func(r *http.Request) { r.Header.Set("x-timestamp", strconv.FormatInt(now.Unix()-1, 10)) }, body, "110006", now},
		{"field", func(r *http.Request) {}, []byte(`{"quantity":100,"ext":{"角色":"一"}}`), "110006", now},
		{"whitespace", func(r *http.Request) {}, []byte(`{ "amount":100,"ext":{"角色":"一"}}`), "110006", now},
		{"order", func(r *http.Request) {}, []byte(`{"ext":{"角色":"一"},"amount":100}`), "110006", now},
		{"expired", func(r *http.Request) {}, body, "110006", now.Add(6 * time.Minute)},
		{"old signature", func(r *http.Request) { r.Header.Set("x-signature", strings.Repeat("a", 40)) }, body, "110006", now},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := makeRequest()
			tc.mutate(r)
			if VerifyGameHMAC(r, []byte("secret"), tc.app, tc.payload, tc.when) {
				t.Fatal("tampered request accepted")
			}
		})
	}
}
