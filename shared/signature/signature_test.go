package signature

import "testing"

func TestSignValues(t *testing.T) {
    got := SignValues("c", "", "a", "b")
    want := SignValues("b", "c", "a")
    if got != want { t.Fatalf("签名与输入顺序有关: %s != %s", got, want) }
    if len(got) != 40 { t.Fatalf("SHA-1 签名长度错误: %s", got) }
}
