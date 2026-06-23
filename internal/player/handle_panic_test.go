// Phase 3.9 regression test: HandleConn must NOT propagate panics
// from hooks. The recover defer (added in Phase 3.9) catches a
// panic from any handler/hook and logs it. Without it, a single
// buggy hook would crash the entire server process.
//
// This test installs a PlayerHooks bundle whose OnEnterPlay
// panics, then drives the FSM to the play state. The panic
// fires after the spawn sequence completes (OnEnterPlay is the
// last thing the FSM does before settling). The assertions:
//
//  1. HandleConn returns normally (the test goroutine completes
//     instead of crashing the test process).
//  2. The OnEnterPlay hook WAS actually called (proving the
//     test seam is firing the panic at the right time — without
//     this, the recover would be a no-op for the wrong reason).
package player_test

import (
	"net"
	"sync/atomic"
	"testing"
	"time"

	"goore/internal/config"
	"goore/internal/player"
	"goore/internal/protocol/v772"
	"goore/internal/world"
)

// panickingHooks is a PlayerHooks impl whose OnEnterPlay panics.
// The atomic counter lets the test verify the hook actually ran
// (otherwise the test would silently pass with an unused
// recover — the wrong reason for being green).
type panickingHooks struct {
	enterPlayCalls *atomic.Int32
}

func (h *panickingHooks) OnEnterPlay(*player.Player) {
	h.enterPlayCalls.Add(1)
	panic("simulated handler bug — Phase 3.9 recover must catch this")
}

func (*panickingHooks) OnLeavePlay(*player.Player)        {}
func (*panickingHooks) OnDisconnect(*player.Player) error { return nil }
func (*panickingHooks) Broadcast(pkt []byte) error        { return nil }
func (*panickingHooks) BroadcastAll(pkt []byte) error     { return nil }

func TestHandleConn_PanicInHookIsRecovered(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	w := world.New(0)
	proto := v772.New()
	cfg := config.DefaultConfig()
	cfg.WorldDir = ""
	cfg.ViewDist = 2

	p := player.New(99, serverConn, proto, w, cfg)

	var enterPlayCalls atomic.Int32
	p.SetHooks(&panickingHooks{enterPlayCalls: &enterPlayCalls})

	// Standard packet reader — same helper other integration
	// tests use. It feeds serverPackets with decoded packets.
	serverPackets := startPacketReader(clientConn)

	// Run HandleConn. With the recover in place, the panic in
	// OnEnterPlay will be caught and HandleConn will return
	// without propagating the panic to the test goroutine.
	handleDone := make(chan struct{})
	var propagated atomic.Bool
	go func() {
		defer close(handleDone)
		defer func() {
			// If the recover in HandleConn is missing, the
			// panic propagates here. We catch it and turn it
			// into a t.Errorf via the propagated flag (the
			// actual t.Errorf is done from the test goroutine
			// below, not from this goroutine — avoid the
			// race between t.Errorf and the test's main flow).
			if r := recover(); r != nil {
				propagated.Store(true)
			}
		}()
		p.HandleConn()
	}()

	// Drive the FSM all the way to the play state. OnEnterPlay
	// is the LAST thing the FSM does in enterPlay, so the
	// panic fires after drainChunks returns.
	handshakeAndLogin(t, clientConn, cfg)
	configAckAndEnterPlay(t, clientConn, serverPackets)
	drainChunks(t, serverPackets)

	// Give HandleConn a moment to reach OnEnterPlay and panic.
	// 200 ms is enough for the FSM to process configAck → spawn
	// → enterPlay → OnEnterPlay.
	time.Sleep(200 * time.Millisecond)

	// Force HandleConn to exit. The panic should have already
	// fired and been recovered, so this is just cleanup.
	clientConn.Close()

	select {
	case <-handleDone:
		// Good — HandleConn returned.
	case <-time.After(3 * time.Second):
		t.Fatal("HandleConn did not return within 3 s — likely the recover defer is missing or broken")
	}

	// Assert the hook was called (otherwise the recover was a
	// no-op for the wrong reason — the panic never fired).
	if enterPlayCalls.Load() == 0 {
		t.Error("OnEnterPlay was never called — the panic test seam is broken; the recover may be untested")
	}
	// Assert the panic did NOT propagate to the test goroutine.
	if propagated.Load() {
		t.Error("HandleConn propagated the panic — the recover defer is missing or in the wrong position")
	}
}
