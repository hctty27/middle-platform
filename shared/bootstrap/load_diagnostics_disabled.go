//go:build !loadtest

package bootstrap

import "database/sql"

// StartLoadDiagnostics is a no-op in normal builds.
func StartLoadDiagnostics(_ *sql.DB) (func(), error) {
	return func() {}, nil
}
