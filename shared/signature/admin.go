package signature

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	adminWindow     = 5 * time.Minute
	adminBodyLimit  = 1 << 20
	adminNonceLimit = 10000
)

// SubscriberSecretLookup 根据接入端编码读取其启用状态和独立签名密钥。
type SubscriberSecretLookup func(context.Context, string) (secret string, enabled bool, err error)

// signHMACRequest 统一计算 HMAC；identity 模式使用独立协议域并绑定接入端编码。
func signHMACRequest(secret, identity, method, path, canonicalQuery, timestamp, nonce string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	prefix := "HITALK-HMAC-SHA256\n"
	if identity != "" {
		prefix += "SUBSCRIBER\n" + identity + "\n"
	}
	mac.Write([]byte(prefix + strings.ToUpper(method) + "\n" +
		path + "\n" + canonicalQuery + "\n" + timestamp + "\n" + nonce + "\n"))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// SignHMACRequest 对方法、转义路径、规范化查询串、时间戳、nonce 和原始 Body 计算 HMAC-SHA256。
func SignHMACRequest(secret, method, path, canonicalQuery, timestamp, nonce string, body []byte) string {
	return signHMACRequest(secret, "", method, path, canonicalQuery, timestamp, nonce, body)
}

// SignSubscriberHMACRequest 在标准请求字段之外绑定接入端编码，防止身份头被替换。
func SignSubscriberHMACRequest(secret, code, method, path, canonicalQuery, timestamp, nonce string, body []byte) string {
	return signHMACRequest(secret, code, method, path, canonicalQuery, timestamp, nonce, body)
}

// SignAdmin 保留管理签名入口，底层统一使用 HMAC-SHA256。
func SignAdmin(secret, method, path, canonicalQuery, timestamp, nonce string, body []byte) string {
	return SignHMACRequest(secret, method, path, canonicalQuery, timestamp, nonce, body)
}

func verifyHMACRequest(r *http.Request, secret, identity string, body []byte, now time.Time) bool {
	if secret == "" || r == nil || r.URL == nil || r.Method == "" {
		return false
	}
	timestamp := r.Header.Get("X-Timestamp")
	nonce := r.Header.Get("X-Nonce")
	provided := r.Header.Get("X-Signature")
	if len(nonce) != 16 || strings.ToLower(nonce) != nonce ||
		len(provided) != 64 || strings.ToLower(provided) != provided {
		return false
	}
	if _, err := hex.DecodeString(nonce); err != nil {
		return false
	}
	actual, err := hex.DecodeString(provided)
	if err != nil {
		return false
	}
	stamp, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil || strconv.FormatInt(stamp, 10) != timestamp {
		return false
	}
	signedAt := time.Unix(stamp, 0)
	if signedAt.Before(now.Add(-adminWindow)) || signedAt.After(now.Add(adminWindow)) {
		return false
	}
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return false
	}
	path := r.URL.EscapedPath()
	if path == "" {
		path = "/"
	}
	expected, err := hex.DecodeString(signHMACRequest(
		secret, identity, r.Method, path, query.Encode(), timestamp, nonce, body,
	))
	if err != nil {
		return false
	}
	return hmac.Equal(actual, expected)
}

// VerifyHMACRequest 验证完整 HTTP 请求签名。body 必须是实际接收的原始请求字节。
func VerifyHMACRequest(r *http.Request, secret string, body []byte, now time.Time) bool {
	return verifyHMACRequest(r, secret, "", body, now)
}

// VerifySubscriberHMACRequest 验证绑定接入端编码的完整 HTTP 请求签名。
func VerifySubscriberHMACRequest(r *http.Request, secret string, body []byte, now time.Time) bool {
	if r == nil {
		return false
	}
	code := r.Header.Get("X-Subscriber-Code")
	if code == "" || len(code) > 128 || strings.TrimSpace(code) != code ||
		strings.ContainsAny(code, "\r\n") {
		return false
	}
	return verifyHMACRequest(r, secret, code, body, now)
}

// Store fixed-size keys and group expirations by second. Full caches reject new
// requests without evicting live nonces; cleanup never scans the entire key map.
type adminNonceCache struct {
	mu          sync.Mutex
	used        map[[32]byte]int64
	expirations map[int64][][32]byte
	lastCleanup int64
	limit       int
}

func (c *adminNonceCache) use(nonce string, expiresAt int64, now time.Time) bool {
	key := sha256.Sum256([]byte(nonce))
	second := now.Unix()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.used == nil {
		c.used = make(map[[32]byte]int64)
		c.expirations = make(map[int64][][32]byte)
	}
	limit := c.limit
	if limit <= 0 {
		limit = adminNonceLimit
	}
	if second != c.lastCleanup {
		for until, keys := range c.expirations {
			if until <= second {
				for _, expiredKey := range keys {
					if c.used[expiredKey] == until {
						delete(c.used, expiredKey)
					}
				}
				delete(c.expirations, until)
			}
		}
		c.lastCleanup = second
	}
	if _, ok := c.used[key]; ok {
		return false
	}
	if len(c.used) >= limit {
		return false
	}
	c.used[key] = expiresAt
	c.expirations[expiresAt] = append(c.expirations[expiresAt], key)
	return true
}

func protectedPath(path string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if prefix != "" && (path == prefix || strings.HasPrefix(path, prefix+"/")) {
			return true
		}
	}
	return false
}

func readSignedBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	var body []byte
	var err error
	if r.Body != nil {
		body, err = io.ReadAll(io.LimitReader(r.Body, adminBodyLimit+1))
		_ = r.Body.Close()
		if err != nil {
			rejectAdminSignature(w)
			return nil, false
		}
		if len(body) > adminBodyLimit {
			http.Error(w, "签名请求体过大", http.StatusRequestEntityTooLarge)
			return nil, false
		}
	}
	return body, true
}

func restoreSignedBody(r *http.Request, body []byte) {
	r.Body = io.NopCloser(bytes.NewReader(body))
	r.ContentLength = int64(len(body))
}

// RequestHMACMiddleware 对指定路径前缀执行固定密钥 HMAC-SHA256 验签和进程内 nonce 去重。
func RequestHMACMiddleware(next http.Handler, prefixes []string, secret string) http.Handler {
	cache := &adminNonceCache{}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !protectedPath(r.URL.Path, prefixes) {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		if len([]byte(secret)) < 32 {
			http.Error(w, "HMAC-SHA256 密钥未配置或强度不足", http.StatusServiceUnavailable)
			return
		}
		timestamp := r.Header.Get("X-Timestamp")
		nonce := r.Header.Get("X-Nonce")
		stamp, err := strconv.ParseInt(timestamp, 10, 64)
		if err != nil || strconv.FormatInt(stamp, 10) != timestamp {
			rejectAdminSignature(w)
			return
		}
		body, ok := readSignedBody(w, r)
		if !ok {
			return
		}
		now := time.Now()
		if !VerifyHMACRequest(r, secret, body, now) {
			rejectAdminSignature(w)
			return
		}
		expiresAt := now.Unix() + int64(adminWindow.Seconds())
		if futureExpiry := stamp + int64(adminWindow.Seconds()); futureExpiry > expiresAt {
			expiresAt = futureExpiry
		}
		if !cache.use(nonce, expiresAt, now) {
			http.Error(w, "请求 nonce 已使用或过于频繁", http.StatusConflict)
			return
		}
		restoreSignedBody(r, body)
		next.ServeHTTP(w, r)
	})
}

// RequestSubscriberHMACMiddleware 按 X-Subscriber-Code 查询独立密钥并验签。
func RequestSubscriberHMACMiddleware(next http.Handler, prefixes []string, lookup SubscriberSecretLookup) http.Handler {
	return RequestSubscriberHMACMiddlewareWithNonceLimit(next, prefixes, lookup, adminNonceLimit)
}

// RequestSubscriberHMACMiddlewareWithNonceLimit allows a bounded per-process
// replay cache sized for the receiving service. A non-positive limit keeps the default.
func RequestSubscriberHMACMiddlewareWithNonceLimit(next http.Handler, prefixes []string, lookup SubscriberSecretLookup, limit int) http.Handler {
	cache := &adminNonceCache{limit: limit}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !protectedPath(r.URL.Path, prefixes) {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		code := r.Header.Get("X-Subscriber-Code")
		if code == "" || len(code) > 128 || strings.TrimSpace(code) != code ||
			strings.ContainsAny(code, "\r\n") || lookup == nil {
			rejectAdminSignature(w)
			return
		}
		secret, enabled, err := lookup(r.Context(), code)
		if err != nil {
			http.Error(w, "签名认证服务暂不可用", http.StatusServiceUnavailable)
			return
		}
		if !enabled || len([]byte(secret)) < 32 || strings.TrimSpace(secret) != secret {
			rejectAdminSignature(w)
			return
		}
		timestamp := r.Header.Get("X-Timestamp")
		nonce := r.Header.Get("X-Nonce")
		stamp, err := strconv.ParseInt(timestamp, 10, 64)
		if err != nil || strconv.FormatInt(stamp, 10) != timestamp {
			rejectAdminSignature(w)
			return
		}
		body, ok := readSignedBody(w, r)
		if !ok {
			return
		}
		now := time.Now()
		if !VerifySubscriberHMACRequest(r, secret, body, now) {
			rejectAdminSignature(w)
			return
		}
		expiresAt := now.Unix() + int64(adminWindow.Seconds())
		if futureExpiry := stamp + int64(adminWindow.Seconds()); futureExpiry > expiresAt {
			expiresAt = futureExpiry
		}
		if !cache.use(code+"\x00"+nonce, expiresAt, now) {
			http.Error(w, "请求 nonce 已使用或过于频繁", http.StatusConflict)
			return
		}
		restoreSignedBody(r, body)
		next.ServeHTTP(w, r)
	})
}

// AdminMiddleware 保护管理 API；GET、POST、PUT、PATCH、DELETE 均须签名。
func AdminMiddleware(next http.Handler, prefix, secret string) http.Handler {
	return RequestHMACMiddleware(next, []string{prefix}, secret)
}

func rejectAdminSignature(w http.ResponseWriter) {
	http.Error(w, "HMAC-SHA256 签名验证失败", http.StatusUnauthorized)
}
