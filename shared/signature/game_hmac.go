package signature

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// SignGameRequest signs the exact encoded request path and body bytes.
func SignGameRequest(secret []byte, method, path, appID, timestamp, nonce string, body []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte("HITALK-HMAC-SHA256\n" + method + "\n" + path + "\n" + appID + "\n" + timestamp + "\n" + nonce + "\n"))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// VerifyGameHMAC authenticates a game request without trusting proxy headers.
func VerifyGameHMAC(r *http.Request, secret []byte, appID string, body []byte, now time.Time) bool {
	timestamp, nonce, signed := r.Header.Get("x-timestamp"), r.Header.Get("x-nonce"), r.Header.Get("x-signature")
	path := r.URL.EscapedPath()
	if len(secret) == 0 || r.Method != http.MethodPost || appID == "" || r.Header.Get("x-app-id") != appID ||
		strings.ContainsAny(appID, "\r\n") || path == "" || strings.ContainsAny(path, "\r\n") || r.URL.RawQuery != "" || r.URL.ForceQuery ||
		len(nonce) != 16 || len(signed) != 64 || strings.ToLower(nonce) != nonce || strings.ToLower(signed) != signed {
		return false
	}
	if _, err := hex.DecodeString(nonce); err != nil {
		return false
	}
	if _, err := hex.DecodeString(signed); err != nil {
		return false
	}
	stamp, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil || timestamp != strconv.FormatInt(stamp, 10) {
		return false
	}
	t := time.Unix(stamp, 0)
	if now.Sub(t) > requestWindow || t.Sub(now) > requestWindow {
		return false
	}
	expected := SignGameRequest(secret, r.Method, path, appID, timestamp, nonce, body)
	return hmac.Equal([]byte(expected), []byte(signed))
}
