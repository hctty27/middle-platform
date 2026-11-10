package signature

import (
	"bytes"
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

// SignHMACRequest 对方法、转义路径、规范化查询串、时间戳、nonce 和原始 Body 计算 HMAC-SHA256。
func SignHMACRequest(secret, method, path, canonicalQuery, timestamp, nonce string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte("HITALK-HMAC-SHA256\n" + strings.ToUpper(method) + "\n" +
		path + "\n" + canonicalQuery + "\n" + timestamp + "\n" + nonce + "\n"))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// SignAdmin 保留管理签名入口，底层统一使用 HMAC-SHA256。
func SignAdmin(secret, method, path, canonicalQuery, timestamp, nonce string, body []byte) string {
	return SignHMACRequest(secret, method, path, canonicalQuery, timestamp, nonce, body)
}

// VerifyHMACRequest 验证完整 HTTP 请求签名。body 必须是实际接收的原始请求字节。
func VerifyHMACRequest(r *http.Request, secret string, body []byte, now time.Time) bool {
	if secret == "" || r == nil || r.URL == nil || r.Method == "" || r.URL.EscapedPath() == "" {
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
	expected, err := hex.DecodeString(SignHMACRequest(
		secret, r.Method, r.URL.EscapedPath(), query.Encode(), timestamp, nonce, body,
	))
	if err != nil {
		return false
	}
	return hmac.Equal(actual, expected)
}

type adminNonceCache struct {
	mu          sync.Mutex
	used        map[string]int64
	lastCleanup time.Time
}

func (c *adminNonceCache) use(nonce string, expiresAt int64, now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.used == nil {
		c.used = make(map[string]int64)
	}
	if now.Sub(c.lastCleanup) >= time.Minute || len(c.used) >= adminNonceLimit {
		for key, until := range c.used {
			if until <= now.Unix() {
				delete(c.used, key)
			}
		}
		c.lastCleanup = now
	}
	if until, ok := c.used[nonce]; ok && until > now.Unix() {
		return false
	}
	if len(c.used) >= adminNonceLimit {
		return false
	}
	c.used[nonce] = expiresAt
	return true
}

// RequestHMACMiddleware 对指定路径前缀执行 HMAC-SHA256 验签和进程内 nonce 去重。
func RequestHMACMiddleware(next http.Handler, prefixes []string, secret string) http.Handler {
	cache := &adminNonceCache{}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		protected := false
		for _, prefix := range prefixes {
			if prefix != "" && (r.URL.Path == prefix || strings.HasPrefix(r.URL.Path, prefix+"/")) {
				protected = true
				break
			}
		}
		if !protected {
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
		now := time.Now()
		var body []byte
		if r.Body != nil {
			body, err = io.ReadAll(io.LimitReader(r.Body, adminBodyLimit+1))
			_ = r.Body.Close()
			if err != nil {
				rejectAdminSignature(w)
				return
			}
			if len(body) > adminBodyLimit {
				http.Error(w, "签名请求体过大", http.StatusRequestEntityTooLarge)
				return
			}
		}
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
		r.Body = io.NopCloser(bytes.NewReader(body))
		r.ContentLength = int64(len(body))
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
