package model

import (
	"encoding/json"
	"testing"
	"time"
)

func TestLocalDateTimeUTC(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value time.Time
		want  string
	}{
		{"UTC seconds", time.Date(2026, 11, 16, 4, 5, 6, 0, time.UTC), "2026-11-16T04:05:06Z"},
		{"positive offset cross day", time.Date(2026, 11, 16, 2, 5, 6, 123456000, time.FixedZone("east", 8*3600)), "2026-11-15T18:05:06.123456Z"},
		{"negative offset milliseconds", time.Date(2026, 11, 16, 22, 5, 6, 123000000, time.FixedZone("west", -5*3600)), "2026-11-17T03:05:06.123Z"},
		{"nanoseconds", time.Date(2026, 11, 16, 4, 5, 6, 123456789, time.UTC), "2026-11-16T04:05:06.123456789Z"},
		{"zero", time.Time{}, "0001-01-01T00:00:00Z"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := json.Marshal(LocalDateTime(tc.value))
			if err != nil {
				t.Fatal(err)
			}
			var got string
			if err := json.Unmarshal(out, &got); err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("got %s, want %s", got, tc.want)
			}
			parsed, err := time.Parse(time.RFC3339Nano, got)
			if err != nil || !parsed.Equal(tc.value) {
				t.Fatalf("instant changed: %s (%v)", got, err)
			}
		})
	}
}
