package store

import (
	"database/sql/driver"
	"errors"
	"testing"

	"hitall.com/middle-platform/sdk-core/internal/model"
)

func TestCheckNotifyUpdateResult(t *testing.T) {
	if err := checkNotifyUpdateResult(driver.RowsAffected(1), nil); err != nil {
		t.Fatalf("one updated row must be accepted: %v", err)
	}
	for _, count := range []int64{0, 2} {
		if err := checkNotifyUpdateResult(driver.RowsAffected(count), nil); !errors.Is(err, model.ErrNotifyResultNotApplied) {
			t.Fatalf("rows=%d must not be reported successful: %v", count, err)
		}
	}
	dbErr := errors.New("database error")
	if err := checkNotifyUpdateResult(nil, dbErr); !errors.Is(err, dbErr) {
		t.Fatalf("database errors must be preserved: %v", err)
	}
}
