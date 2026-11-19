package webhook

import (
	"database/sql/driver"
	"errors"
	"testing"
)

func TestCheckWebhookUpdateResult(t *testing.T) {
	if err := checkWebhookUpdateResult(driver.RowsAffected(1), nil); err != nil {
		t.Fatalf("one updated row must be accepted: %v", err)
	}
	for _, count := range []int64{0, 2} {
		if err := checkWebhookUpdateResult(driver.RowsAffected(count), nil); !errors.Is(err, errWebhookLeaseNotApplied) {
			t.Fatalf("rows=%d must not be reported successful: %v", count, err)
		}
	}
	dbErr := errors.New("database error")
	if err := checkWebhookUpdateResult(nil, dbErr); !errors.Is(err, dbErr) {
		t.Fatalf("database errors must be preserved: %v", err)
	}
}
