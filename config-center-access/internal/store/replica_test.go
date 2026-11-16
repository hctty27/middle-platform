package store

import (
	"hitall.com/middle-platform/config-center-access/internal/remote"
	"testing"
	"time"
)

func TestParseDateUTC(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  string
	}{
		{"2026-11-16T04:05:06Z", "2026-11-16T04:05:06Z"},
		{"2026-11-16T04:05:06.123456789Z", "2026-11-16T04:05:06.123456789Z"},
		{"2026-11-16T02:05:06.123456+08:00", "2026-11-15T18:05:06.123456Z"},
		{"2026-11-16T22:05:06.123-05:00", "2026-11-17T03:05:06.123Z"},
		{"2026-11-16T04:05:06", "2026-11-16T04:05:06Z"},
		{"2026-11-16T04:05:06.123456", "2026-11-16T04:05:06.123456Z"},
	} {
		t.Run(tc.input, func(t *testing.T) {
			got, err := parseDate(tc.input)
			if err != nil {
				t.Fatal(err)
			}
			if got.Location() != time.UTC || got.Format(time.RFC3339Nano) != tc.want {
				t.Fatalf("got %s (%s), want %s (UTC)", got.Format(time.RFC3339Nano), got.Location(), tc.want)
			}
			node := remote.Node{ID: 1, NodeType: "game", Code: "g", Name: "Game", Config: "{}", CreatedAt: tc.input, UpdatedAt: tc.input}
			if err := validateNode(node); err != nil {
				t.Fatalf("valid sync node rejected: %v", err)
			}
		})
	}
	for _, input := range []string{"", "not-a-time", "2026-02-30T00:00:00Z", "2026-11-16T24:00:00Z", "2026-11-16T04:05:06+08", "2026-11-16 04:05:06"} {
		if _, err := parseDate(input); err == nil {
			t.Errorf("invalid time accepted: %q", input)
		}
	}
}

// TestValidateNodeEnvironment 验证副本节点只允许大区设置合法的环境类型。
func TestValidateNodeEnvironment(t *testing.T) {
	now := time.Now().UTC().Format("2006-01-02T15:04:05.999999")
	env := "review"
	node := remote.Node{ID: 1, NodeType: "region", EnvType: &env, Code: "r", Name: "Region",
		Config: "{}", CreatedAt: now, UpdatedAt: now}
	if err := validateNode(node); err != nil {
		t.Fatal(err)
	}
	env = "prod"
	if err := validateNode(node); err != nil {
		t.Fatal(err)
	}
	env = "production"
	if err := validateNode(node); err == nil {
		t.Fatal("accepted deprecated production environment")
	}
	env = "bad"
	if err := validateNode(node); err == nil {
		t.Fatal("accepted bad environment")
	}
	env = "review"
	node.NodeType = "game"
	if err := validateNode(node); err == nil {
		t.Fatal("accepted environment on game")
	}
	node.EnvType = nil
	if err := validateNode(node); err != nil {
		t.Fatal(err)
	}
}
