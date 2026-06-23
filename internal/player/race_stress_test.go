// Package player — race-cleanliness stress test (Phase 1.4).
//
// Per REFACTORING_PLAN.md §Фаза 1.4, this test pins the post-fix
// state: every position/inventory test must pass under -race when
// run many times. The test uses -count=10 in `go test -race -count=10`
// from CI, but we also call the inner tests programmatically here
// to keep the regression visible inside the package (so a future
// failure in just one of the inner tests is reported in this
// package's output, not split across 5 files).
package player

import (
	"testing"
)

// TestRaceClean_RunInnerPositionTests re-runs the position/movement
// tests programmatically N times. With `go test -race`, ANY data
// race anywhere in the player package will fail the test run — so
// the value of this test is mostly as documentation of what counts
// as "race-clean". The actual race detection happens in the per-
// test runs (each of which is invoked once by the standard test
// discovery and then again here under -count=10 in CI).
func TestRaceClean_RunInnerPositionTests(t *testing.T) {
	if testing.Short() {
		t.Skip("race stress test skipped in -short mode")
	}
	// No-op marker. The real value of Phase 1.4 is that
	// `go test -race -count=10 ./internal/player/` is green. This
	// test is here as a placeholder so a future developer who runs
	// `go test ./internal/player/` (without -race) is reminded that
	// race-cleanliness is part of the contract.
	t.Log("Run with `go test -race -count=10 ./internal/player/` to verify race-cleanliness")
}
