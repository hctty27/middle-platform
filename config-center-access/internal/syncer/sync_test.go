package syncer

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"hitall.com/middle-platform/config-center-access/internal/remote"
)

// TestContiguous 覆盖版本连续、断档、重复及空事件等同步边界。
func TestContiguous(t *testing.T) {
	cases := []struct {
		name   string
		after  int64
		events []remote.Event
		want   bool
	}{
		{"valid", 7, []remote.Event{{Revision: 8}, {Revision: 9}}, true},
		{"empty", 7, nil, false},
		{"gap", 7, []remote.Event{{Revision: 9}}, false},
		{"duplicate", 7, []remote.Event{{Revision: 8}, {Revision: 8}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if actual := contiguous(tc.after, tc.events); actual != tc.want {
				t.Fatalf("got %v", actual)
			}
		})
	}
}

func TestSyncAppliedRevisionLog(t *testing.T) {
	for _, tc := range []struct {
		mode, message, countKey string
	}{
		{"incremental", "配置增量同步完成", "event_count"},
		{"snapshot", "配置全量同步完成", "node_count"},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			var out bytes.Buffer
			old := slog.Default()
			slog.SetDefault(slog.New(slog.NewJSONHandler(&out, nil)))
			defer slog.SetDefault(old)
			logSyncApplied(tc.mode, 7, 12, 5, 83*time.Millisecond)
			var log map[string]any
			if err := json.Unmarshal(out.Bytes(), &log); err != nil {
				t.Fatal(err)
			}
			if log["msg"] != tc.message || log["mode"] != tc.mode ||
				log["old_revision"] != float64(7) || log["new_revision"] != float64(12) ||
				log[tc.countKey] != float64(5) || log["duration_ms"] != float64(83) {
				t.Fatalf("unexpected sync log: %v", log)
			}
		})
	}
}
