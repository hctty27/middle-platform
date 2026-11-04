package adapter

import "testing"

func TestNameForService(t *testing.T) {
    cases := []struct {
        name        string
        adapters    map[string]string
        serviceCode string
        want        string
    }{
        {"无配置", nil, "user_check", "standard"},
        {"空对象", map[string]string{}, "user_check", "standard"},
        {"空字符串", map[string]string{"user_check": ""}, "user_check", "standard"},
        {"空白字符串", map[string]string{"user_check": "  "}, "user_check", "standard"},
        {"明确指定默认", map[string]string{"user_check": "standard"}, "user_check", "standard"},
        {"特殊适配器", map[string]string{"user_check": " game_a "}, "user_check", "game_a"},
        {"未知适配器不回退", map[string]string{"user_check": "not_registered"}, "user_check", "not_registered"},
        {"未来角色接口未配置", map[string]string{"user_check": "game_a"}, "role_query", "standard"},
        {"未来区服接口未配置", nil, "server_query", "standard"},
        {"其他业务单独覆盖", map[string]string{"user_check": "game_a", "role_query": "game_b"}, "role_query", "game_b"},
    }
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            if got := NameForService(tc.adapters, tc.serviceCode); got != tc.want {
                t.Fatalf("NameForService(%q)=%q, want=%q", tc.serviceCode, got, tc.want)
            }
        })
    }
}
