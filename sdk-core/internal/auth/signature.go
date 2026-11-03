package auth

import (
    "bytes"
    "crypto/sha1"
    "crypto/subtle"
    "encoding/hex"
    "encoding/json"
    "errors"
    "fmt"
    "net/http"
    "sort"
    "strconv"
    "strings"
    "time"
)

// SignValues 与平台算法一致：过滤空值、按值排序、用下划线拼接、求 SHA-1 小写十六进制。
func SignValues(values []string) string {
    filtered := make([]string, 0, len(values))
    for _, value := range values {
        if value != "" {
            filtered = append(filtered, value)
        }
    }
    sort.Strings(filtered)
    sum := sha1.Sum([]byte(strings.Join(filtered, "_")))
    return hex.EncodeToString(sum[:])
}

// bodyValues 读取所有顶层非空业务值；不排序字段名，排序的是最终的字符串值。
// 嵌套 JSON 压缩后作为一个值，保证与平台文档的 JSON.stringify 口径一致。
func bodyValues(rawBody []byte) ([]string, error) {
    var body map[string]json.RawMessage
    if err := json.Unmarshal(rawBody, &body); err != nil || body == nil {
        return nil, errors.New("签名请求体必须是 JSON 对象")
    }
    values := make([]string, 0, len(body))
    for _, raw := range body {
        trimmed := bytes.TrimSpace(raw)
        if bytes.Equal(trimmed, []byte("null")) {
            continue
        }
        if len(trimmed) == 0 {
            return nil, errors.New("签名请求字段不合法")
        }
        if trimmed[0] == '"' {
            var str string
            if err := json.Unmarshal(trimmed, &str); err != nil {
                return nil, err
            }
            if str != "" {
                values = append(values, str)
            }
            continue
        }
        var compact bytes.Buffer
        if err := json.Compact(&compact, trimmed); err != nil {
            return nil, err
        }
        values = append(values, compact.String())
    }
    return values, nil
}

// SignGame 与平台使用相同的值排序规则，签名只覆盖主密钥、AppID、时间戳、Nonce 和 Body 值。
// env 和业务操作不参与签名；环境绑定与接口权限须由业务层独立校验。
func SignGame(secret []byte, appID, timestamp, nonce string, rawBody []byte) (string, error) {
    if len(secret) == 0 {
        return "", ErrGameSecret
    }
    if appID == "" || timestamp == "" || nonce == "" {
        return "", ErrUnauthorized
    }
    values, err := bodyValues(rawBody)
    if err != nil {
        return "", fmt.Errorf("读取签名业务字段失败: %w", err)
    }
    values = append(values, string(secret), appID, timestamp, nonce)
    return SignValues(values), nil
}

// VerifyRegistration 校验订单登记签名及其路由业务归属。
func VerifyRegistration(r *http.Request, rawBody, secret []byte) error {
    if r.Method != http.MethodPost {
        return ErrUnauthorized
    }
    env, appID := r.PathValue("env"), r.PathValue("appid")
    ts := r.Header.Get("x-timestamp")
    nonce := r.Header.Get("x-nonce")
    signature := r.Header.Get("x-signature")
    if env == "" || appID == "" || r.Header.Get("x-app-id") != appID ||
        len(nonce) != 16 || strings.ToLower(nonce) != nonce ||
        len(signature) != 40 || strings.ToLower(signature) != signature {
        return ErrUnauthorized
    }
    random, err := hex.DecodeString(nonce)
    if err != nil || len(random) != 8 {
        return ErrUnauthorized
    }
    provided, err := hex.DecodeString(signature)
    if err != nil {
        return ErrUnauthorized
    }
    stamp, err := strconv.ParseInt(ts, 10, 64)
    if err != nil || time.Since(time.Unix(stamp, 0)) > 5*time.Minute ||
        time.Until(time.Unix(stamp, 0)) > 5*time.Minute {
        return ErrUnauthorized
    }
    expected, err := SignGame(secret, appID, ts, nonce, rawBody)
    if err != nil {
        return ErrUnauthorized
    }
    digest, _ := hex.DecodeString(expected)
    if subtle.ConstantTimeCompare(provided, digest) != 1 {
        return ErrUnauthorized
    }
    return nil
}
