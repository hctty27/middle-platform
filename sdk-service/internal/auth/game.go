// Package auth 验证游戏服务器向 SDK Service 发起的业务请求。
package auth

import (
    "bytes"
    "crypto/subtle"
    "encoding/hex"
    "encoding/json"
    "errors"
    "net/http"
    "strconv"
    "strings"
    "time"

    "github.com/hctty27/middle-platform/shared/signature"
)

var ErrUnauthorized = errors.New("游戏请求签名验证失败")

func SignGame(secret, appID, timestamp, nonce string, body []byte) (string, error) {
    if secret == "" || appID == "" || timestamp == "" || nonce == "" {
        return "", ErrUnauthorized
    }
    var input map[string]json.RawMessage
    if err := json.Unmarshal(body, &input); err != nil || input == nil {
        return "", ErrUnauthorized
    }
    values := []string{secret, appID, timestamp, nonce}
    for _, raw := range input {
        value := bytes.TrimSpace(raw)
        if len(value) == 0 || bytes.Equal(value, []byte("null")) {
            continue
        }
        if value[0] == '"' {
            var text string
            if json.Unmarshal(value, &text) != nil {
                return "", ErrUnauthorized
            }
            values = append(values, text)
            continue
        }
        var compact bytes.Buffer
        if json.Compact(&compact, value) != nil {
            return "", ErrUnauthorized
        }
        values = append(values, compact.String())
    }
    return signature.SignValues(values...), nil
}

func VerifyGameRequest(r *http.Request, appID, secret string, body []byte, now time.Time) error {
    timestamp := r.Header.Get("x-timestamp")
    nonce := r.Header.Get("x-nonce")
    provided := r.Header.Get("x-signature")
    if r.Method != http.MethodPost || r.Header.Get("x-app-id") != appID ||
        len(nonce) != 16 || strings.ToLower(nonce) != nonce ||
        len(provided) != 40 || strings.ToLower(provided) != provided {
        return ErrUnauthorized
    }
    nonceBytes, nonceErr := hex.DecodeString(nonce)
    sigBytes, sigErr := hex.DecodeString(provided)
    ts, timeErr := strconv.ParseInt(timestamp, 10, 64)
    if nonceErr != nil || len(nonceBytes) != 8 || sigErr != nil || timeErr != nil ||
        now.Sub(time.Unix(ts, 0)) > 5*time.Minute ||
        time.Unix(ts, 0).Sub(now) > 5*time.Minute {
        return ErrUnauthorized
    }
    expected, err := SignGame(secret, appID, timestamp, nonce, body)
    if err != nil {
        return ErrUnauthorized
    }
    digest, _ := hex.DecodeString(expected)
    if subtle.ConstantTimeCompare(digest, sigBytes) != 1 {
        return ErrUnauthorized
    }
    return nil
}
