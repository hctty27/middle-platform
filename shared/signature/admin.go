package signature

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	adminWindow = 5 * time.Minute
	adminBodyLimit = 1 << 20
	adminNonceLimit = 10000
)

// SignAdmin 使用现有的排序 SHA-1 协议，为管理请求绑定方法、路径、参数与请求体。
// canonicalQuery 应通过 url.Values.Encode() 生成；path 为服务端看到的转义路径。
func SignAdmin(secret, method, path, canonicalQuery, timestamp, nonce string, body []byte) string {
	digest := sha256.Sum256(body)
	return SignValues(
		"admin-v1",
		secret,
		"method:"+strings.ToUpper(method),
		"path:"+path,
		"query:"+canonicalQuery,
		"body:"+hex.EncodeToString(digest[:]),
		"timestamp:"+timestamp,
		"nonce:"+nonce,
	)
}

type adminNonceCache struct {
	mu sync.Mutex
	used map[string]int64
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

// AdminMiddleware 只保护指定 /admin 前缀，其它业务、同步和健康检查接口不受影响。
// Nonce 缓存是进程内的；多实例场景下不能替代共享去重与业务幂等。
func AdminMiddleware(next http.Handler, prefix, secret string) http.Handler {
	cache := &adminNonceCache{}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != prefix && !strings.HasPrefix(r.URL.Path, prefix+"/") {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		if len(secret) < 32 {
			http.Error(w, "管理接口签名密钥未配置或强度不足", http.StatusServiceUnavailable)
			return
		}
		timestamp := r.Header.Get("X-Timestamp")
		nonce := r.Header.Get("X-Nonce")
		provided := r.Header.Get("X-Signature")
		if len(nonce) != 16 || strings.ToLower(nonce) != nonce ||
			len(provided) != 40 || strings.ToLower(provided) != provided {
			rejectAdminSignature(w)
			return
		}
		if _, err := hex.DecodeString(nonce); err != nil {
			rejectAdminSignature(w)
			return
		}
		actual, err := hex.DecodeString(provided)
		if err != nil {
			rejectAdminSignature(w)
			return
		}
		stamp, err := strconv.ParseInt(timestamp, 10, 64)
		if err != nil || strconv.FormatInt(stamp, 10) != timestamp {
			rejectAdminSignature(w)
			return
		}
		now := time.Now()
		t := time.Unix(stamp, 0)
		if t.Before(now.Add(-adminWindow)) || t.After(now.Add(adminWindow)) {
			rejectAdminSignature(w)
			return
		}
		query, err := url.ParseQuery(r.URL.RawQuery)
		if err != nil {
			rejectAdminSignature(w)
			return
		}
		var body []byte
		if r.Body != nil {
			body, err = io.ReadAll(io.LimitReader(r.Body, adminBodyLimit+1))
			_ = r.Body.Close()
			if err != nil {
				rejectAdminSignature(w)
				return
			}
			if len(body) > adminBodyLimit {
				http.Error(w, "管理接口请求体过大", http.StatusRequestEntityTooLarge)
				return
			}
		}
		expected, _ := hex.DecodeString(SignAdmin(secret, r.Method, r.URL.EscapedPath(), query.Encode(), timestamp, nonce, body))
		if subtle.ConstantTimeCompare(actual, expected) != 1 {
			rejectAdminSignature(w)
			return
		}
		if !cache.use(nonce, stamp+int64(adminWindow.Seconds()), now) {
			http.Error(w, "管理接口请求已使用或过于频繁", http.StatusConflict)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		r.ContentLength = int64(len(body))
		next.ServeHTTP(w, r)
	})
}

func rejectAdminSignature(w http.ResponseWriter) {
	http.Error(w, fmt.Sprintf("%s", "管理接口签名验证失败"), http.StatusUnauthorized)
}
