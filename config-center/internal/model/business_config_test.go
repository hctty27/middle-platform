package model

import "testing"

func TestValidateBusinessConfig(t *testing.T) {
	tests := []struct {
		name     string
		nodeType string
		config   string
		wantErr  bool
	}{
		{"game", "game", `{"gameSecret":"中文密钥 !","custom":{"enabled":true}}`, true},
		{"region", "region", `{"payNotifyUrl":"http://127.0.0.1:8080/notify"}`, true},
		{"server group", "server_group", `{"extra":123}`, false},
		{"app", "channel_app", `{"tsKey":"","appSecret":"secret","extra":false}`, false},
		{"unset", "game", `{}`, false},
		{"wrong game field type", "game", `{"gameSecret":9}`, true},
		{"wrong region field type", "region", `{"payNotifyUrl":true}`, true},
		{"wrong app field type", "channel_app", `{"appSecret":{}}`, true},
		{"null", "game", `{"gameSecret":null}`, true},
		{"array root", "game", `[]`, true},
		{"invalid JSON", "region", `{`, true},
		{"unknown type", "other", `{}`, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateBusinessConfig(test.nodeType, test.config)
			if (err != nil) != test.wantErr {
				t.Fatalf("ValidateBusinessConfig(%q, %q) = %v, wantErr=%v", test.nodeType, test.config, err, test.wantErr)
			}
		})
	}
}
