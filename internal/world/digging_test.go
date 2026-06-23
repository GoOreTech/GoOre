// Tests for the digging module (digging.go). Symmetric to
// placement_test.go: pins the 1 rejection rule (DigNothingThere)
// and the pure-on-failure invariant.
package world_test

import (
	"testing"

	"goore/internal/world"
)

// TestTryBreak_Success: dig stone at (0, 3, 0). Asserts:
// result.Broken=true, result.OldBlock=stone, world now has air.
func TestTryBreak_Success(t *testing.T) {
	w := world.New(42)
	w.SetBlock(0, 3, 0, world.BlockStone)
	got := world.TryBreak(w, world.DigRequest{Position: pos(0, 3, 0)})
	if !got.Broken {
		t.Fatalf("TryBreak: Broken = false, want true; Reason = %v", got.Reason)
	}
	if got.OldBlock != world.BlockStone {
		t.Errorf("TryBreak: OldBlock = %d, want %d (stone)", got.OldBlock, world.BlockStone)
	}
	if w.GetBlock(0, 3, 0) != world.BlockAir {
		t.Errorf("world at (0,3,0) = %d, want air (block should be removed)", w.GetBlock(0, 3, 0))
	}
}

// TestTryBreak_NothingThere: dig a position that's already air.
// Asserts: Broken=false, OldBlock=air, Reason=DigNothingThere,
// world UNCHANGED. We use y=4 (above the flat-world grass at y=3)
// so the position is air without any pre-seeding.
func TestTryBreak_NothingThere(t *testing.T) {
	w := world.New(42)
	// Pre-condition: target is air.
	if w.GetBlock(0, 4, 0) != world.BlockAir {
		t.Fatalf("precondition: (0,4,0) = %d, want air", w.GetBlock(0, 4, 0))
	}
	got := world.TryBreak(w, world.DigRequest{Position: pos(0, 4, 0)})
	if got.Broken {
		t.Errorf("TryBreak: Broken = true, want false (target was already air)")
	}
	if got.OldBlock != world.BlockAir {
		t.Errorf("TryBreak: OldBlock = %d, want air", got.OldBlock)
	}
	if got.Reason != world.DigNothingThere {
		t.Errorf("TryBreak: Reason = %v, want DigNothingThere", got.Reason)
	}
	// World MUST be unchanged on rejection.
	if w.GetBlock(0, 4, 0) != world.BlockAir {
		t.Errorf("world at (0,4,0) = %d, want air (rejection MUST NOT mutate world)", w.GetBlock(0, 4, 0))
	}
}

// TestTryBreak_StillAcksEvenOnRejection pins the protocol contract:
// the digger still sends WriteAckPlayerDigging(sequence) even when
// the position was empty, "for protocol politeness". The handler
// (player.handlePlayerAction) reads DigResult.Broken to decide
// whether to broadcast a block_update — but always acks. This
// test pins that TryBreak surfaces enough info for the handler
// to do this (Broken=false is the signal "skip broadcast, still ack").
func TestTryBreak_StillAcksEvenOnRejection(t *testing.T) {
	w := world.New(42)
	got := world.TryBreak(w, world.DigRequest{Position: pos(0, 4, 0)})
	// The handler will need to distinguish "was there a block to
	// broadcast?" from "did anything go wrong?". Broken=false
	// answers that — it's the boolean the handler uses.
	if got.Broken {
		t.Error("TryBreak: Broken = true on empty position, want false")
	}
}
