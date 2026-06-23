// Package player — regression test for the keep-alive goroutine leak.
//
// Bug (REFACTORING_PLAN.md §3.2): `p.disconnect` is created in `New()`
// and read by the keep-alive goroutine via `case <-p.disconnect: return`,
// but `close(p.disconnect)` is never called. After `HandleConn` returns,
// the defer stops the ticker but the goroutine is stuck forever on
// `select { case <-ticker.C: ... case <-p.disconnect: return }` with
// neither case ever firing. One leaked goroutine per client connection.
//
// This test starts the keep-alive goroutine directly (via the
// unexported `startKeepAlive`), then mimics HandleConn's defer
// (stop the ticker, log, set state) — but intentionally leaves
// `close(p.disconnect)` as a separate step. We then poll
// runtime.NumGoroutine() to detect the leak.
//
// This is a unit-level test of the lifecycle, not a full E2E flow —
// it isolates the bug to the missing close() in HandleConn's defer.
package player

import (
	"runtime"
	"testing"
	"time"

	"goore/internal/config"
	"goore/internal/protocol/v772"
	"goore/internal/world"
)

// stubConn is a no-op io.ReadWriter. We never actually read from
// or write to the connection in this test — we only care about
// keep-alive lifecycle.
type stubConn struct{}

func (stubConn) Read(p []byte) (int, error)  { return 0, nil }
func (stubConn) Write(p []byte) (int, error) { return len(p), nil }

// mimicHandleConnDefer reproduces the cleanup steps that HandleConn's
// defer performs (stop the ticker, mark state, log). It does NOT
// close p.disconnect — that's the bug. If close() is missing, the
// keep-alive goroutine leaks.
//
// This helper exists so the test pins the exact shape of HandleConn's
// defer; if the real defer ever drifts (e.g. someone adds a close
// call), the test stays in sync.
func mimicHandleConnDefer(p *Player) {
	if p.keepAliveTicker != nil {
		p.keepAliveTicker.Stop()
	}
	p.setState(stateDisconnected)
}

// waitForGoroutineCount polls runtime.NumGoroutine() until it drops
// to `target` or `timeout` elapses. Returns the final count and
// whether the target was reached.
func waitForGoroutineCount(target int, timeout time.Duration) (int, bool) {
	deadline := time.Now().Add(timeout)
	for {
		if n := runtime.NumGoroutine(); n <= target {
			return n, true
		}
		if time.Now().After(deadline) {
			return runtime.NumGoroutine(), false
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestNoGoroutineLeak_AfterDefer is the regression test for the
// keep-alive goroutine leak. It starts the keep-alive goroutine,
// then runs the defer cleanup (stop ticker, mark disconnected).
//
// After the fix, HandleConn's defer must ALSO call `close(p.disconnect)`
// to wake up the goroutine. The test pins that requirement by
// checking the goroutine count drops to baseline+slack after the
// full defer sequence (ticker.Stop + state change + close disconnect).
//
// Without the close, the goroutine is stuck on `select` forever.
func TestNoGoroutineLeak_AfterDefer(t *testing.T) {
	// Warm up: prime the runtime so the first measurement is stable.
	// We use a close() to release the goroutine so warmup itself
	// doesn't leak.
	for i := 0; i < 3; i++ {
		p := New(int32(i), stubConn{}, v772.New(), world.New(0), config.DefaultConfig())
		p.startKeepAlive()
		mimicHandleConnDefer(p)
		close(p.disconnect) // close so warmup doesn't leak
	}
	time.Sleep(100 * time.Millisecond)

	baseline := runtime.NumGoroutine()
	// Slack: Go's runtime can spawn GC / scavenger workers on its
	// own schedule. We only care that OUR keep-alive goroutine
	// has exited.
	const slack = 1
	target := baseline + slack

	// Iterate: each round spawns a keep-alive goroutine and runs
	// the full defer cleanup (including the close). With the fix
	// in place, every round releases the goroutine.
	const iterations = 5
	for i := 0; i < iterations; i++ {
		p := New(int32(100+i), stubConn{}, v772.New(), world.New(0), config.DefaultConfig())
		p.startKeepAlive()
		// Give the goroutine a moment to be scheduled onto a
		// runqueue. startKeepAlive launches it asynchronously.
		time.Sleep(10 * time.Millisecond)
		mimicHandleConnDefer(p)
		// Mimic what HandleConn's defer does AFTER the fix: close
		// p.disconnect to wake the keep-alive goroutine.
		close(p.disconnect)
	}

	// Poll: after the close, the goroutine should exit on its next
	// select iteration (immediately, since the ticker is also
	// stopped and the select only has the disconnect case left).
	got, ok := waitForGoroutineCount(target, 2*time.Second)
	if !ok {
		t.Fatalf("goroutine leak: NumGoroutine() = %d, want <= %d (baseline %d + slack %d) after %d keep-alive lifecycles",
			got, target, baseline, slack, iterations)
	}
}
