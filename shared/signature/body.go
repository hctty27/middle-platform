package signature

import (
	"bytes"
	"encoding/json"
	"errors"
)

// BodyValues 提取 JSON 对象的顶层非空业务值，保持现有游戏协议的值签名规则。
// null 和空字符串不参与最终的 SignValues；嵌套 JSON 按紧凑文本参与签名。
func BodyValues(rawBody []byte) ([]string, error) {
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
