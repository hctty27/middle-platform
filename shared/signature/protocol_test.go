package signature

import (
	"crypto/sha1"
	"encoding/hex"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestSignAllValuesPreservesEmptyFields(t *testing.T) {
	original := []string{"", "order1", "game1", "secret", "nonce"}
	copyValues := append([]string(nil), original...)
	sort.Strings(copyValues)
	sum := sha1.Sum([]byte(strings.Join(copyValues, "_")))
	want := hex.EncodeToString(sum[:])
	if got := SignAllValues(original...); got != want {
		t.Fatalf("包含空字段的原协议签名不一致: %s != %s", got, want)
	}
	if SignAllValues(original...) == SignValues(original...) {
		t.Fatal("严格保留空字段的签名不应和过滤空字段的签名一致")
	}
}

func TestBodyValuesCompatibility(t *testing.T) {
	cases := []struct{ body string; want string }{
		{`{"a":"","b":null,"c":0,"d":false,"e":{"x":1,"y":2}}`, "0"},
		{`{"a":"","b":null,"c":0,"d":false,"e": { "x": 1, "y": 2 }}`, "0"},
	}
	var reference string
	for _, tc := range cases {
		values, err := BodyValues([]byte(tc.body))
		if err != nil { t.Fatal(err) }
		got := SignValues(append([]string{"secret", "app", "time", "nonce"}, values...)...)
		if reference == "" { reference = got }
		if got != reference { t.Fatalf("嵌套 JSON 规范化不一致: %s != %s", got, reference) }
	}
	for _, raw := range []string{"null", "[]", "{}", ""} {
		_, err := BodyValues([]byte(raw))
		if raw == "{}" {
			if err != nil { t.Fatalf("空对象应该允许: %v", err) }
		} else if err == nil {
			t.Fatalf("无效 JSON 对象被接受: %q", raw)
		}
	}
}

func TestHeaderValidationAndSignatureCompare(t *testing.T) {
	now := time.Unix(1793800000, 0)
	nonce := "0011223344556677"
	sign := SignValues("secret", "body")
	if !ValidHeaders("1793800000", nonce, sign, now) { t.Fatal("有效 Header 未通过") }
	if !ValidHeaders("1793800000", nonce, sign, now.Add(5*time.Minute)) { t.Fatal("边界时间应通过") }
	if ValidHeaders("1793800000", nonce, sign, now.Add(6*time.Minute)) { t.Fatal("过期签名被接受") }
	if ValidHeaders("1793800000", "invalid", sign, now) { t.Fatal("无效 Nonce 被接受") }
	if ValidHeaders("1793800000", nonce, strings.ToUpper(sign), now) { t.Fatal("大写签名被接受") }
	if !EqualHex(sign, sign) || EqualHex(sign, strings.ToUpper(sign)) {
		t.Fatal("十六进制签名比较异常")
	}
}
