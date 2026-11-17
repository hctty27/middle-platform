package main

import "testing"

func TestPlatformFixtureKey(t *testing.T) {
	for _, tc := range []struct {
		app string
		key string
		ok  bool
	}{
		{"load-app", "runner-load-platform-only", true},
		{"load-app-0", "runner-load-platform-only-0", true},
		{"load-app-255", "runner-load-platform-only-255", true},
		{"load-app-256", "", false},
		{"load-app-01", "", false},
		{"load-app--1", "", false},
		{"other", "", false},
	} {
		key, ok := platformKey(tc.app)
		if key != tc.key || ok != tc.ok {
			t.Fatalf("invalid fixture lookup for %q", tc.app)
		}
	}
}
