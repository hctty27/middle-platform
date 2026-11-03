package auth

import (
    "bytes"
    "encoding/json"
    "testing"
)

func TestParseGameSecretUsesRawUTF8(t *testing.T) {
    secrets := []string{"short", "  secret  ", "自定义密钥-123!@#", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="}
    for _, secret := range secrets {
        raw, err := json.Marshal(map[string]string{"gameSecret":secret})
        if err != nil {t.Fatal(err)}
        parsed, err := ParseGameSecret(raw)
        if err != nil || !bytes.Equal(parsed, []byte(secret)) {
            t.Fatalf("parse %q = %q, error=%v", secret, parsed, err)
        }
    }
    invalid := []string{`{}`, `{"gameSecret":""}`, `{"gameSecret":"  "}`, `{"gameSecret":null}`, `{"gameSecret":42}`, `["invalid"]`}
    for _, raw := range invalid {
        if _, err := ParseGameSecret([]byte(raw)); err == nil {
            t.Errorf("accepted invalid config: %s", raw)
        }
    }
}
