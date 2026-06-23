// Tests for the placement module (placement.go). Pins:
//  1. Face enum → target offset (6 cases)
//  2. Rejection rules: not_placeable, intersects_player, target_occupied
//  3. World is mutated ONLY on success
//  4. AABB is a public, reusable geometric operation
package world_test

import (
	"testing"

	"goore/internal/protocol"
	"goore/internal/world"
)

// pos is a tiny helper for readable test tables.
func pos(x, y, z int32) protocol.BlockPos {
	return protocol.BlockPos{X: x, Y: y, Z: z}
}

// TestTargetForFace pins the 6-element face→target mapping. The
// placement handler in player.go uses this indirectly via TryPlace;
// the pure helper is exposed so callers that need to compute
// targets without running the full placement pipeline (e.g. UI
// previews, debug commands) can use it.
func TestTargetForFace(t *testing.T) {
	cases := []struct {
		face  int32
		want  protocol.BlockPos
		label string
	}{
		{world.FaceDown, pos(0, 2, 0), "face=0 → Y-1"},
		{world.FaceUp, pos(0, 4, 0), "face=1 → Y+1"},
		{world.FaceNorth, pos(0, 3, -1), "face=2 → Z-1"},
		{world.FaceSouth, pos(0, 3, 1), "face=3 → Z+1"},
		{world.FaceWest, pos(-1, 3, 0), "face=4 → X-1"},
		{world.FaceEast, pos(1, 3, 0), "face=5 → X+1"},
	}
	for _, c := range cases {
		got := world.TargetForFace(pos(0, 3, 0), c.face)
		if got != c.want {
			t.Errorf("%s: TargetForFace = %+v, want %+v", c.label, got, c.want)
		}
	}
}

// TestTargetForFace_OutOfRange pins the defensive default: an
// unknown face value returns the clicked block unchanged. The
// vanilla client only sends 0..5, but a malicious or bugged peer
// could send any int32; we'd rather no-op than crash.
func TestTargetForFace_OutOfRange(t *testing.T) {
	clicked := pos(7, 11, 13)
	for _, face := range []int32{-1, 6, 100, 1 << 30} {
		got := world.TargetForFace(clicked, face)
		if got != clicked {
			t.Errorf("face=%d: TargetForFace = %+v, want %+v (unchanged on unknown face)", face, got, clicked)
		}
	}
}

// TestTryPlace_Success: place stone (HeldItem=1) at +Y of (0,3,0).
// Asserts: result.Placed, result.StateID, world block at target.
func TestTryPlace_Success(t *testing.T) {
	w := world.New(42)
	// Spawn at (1000.5, 4, 1000.5) so AABB never triggers.
	req := world.PlaceRequest{
		Clicked:  pos(0, 3, 0),
		Face:     world.FaceUp,
		HeldItem: 1, // stone
		PlayerX:  1000.5, PlayerY: 4, PlayerZ: 1000.5,
	}
	got := world.TryPlace(w, req)
	if !got.Placed {
		t.Fatalf("TryPlace: Placed = false, want true; Reason = %v", got.Reason)
	}
	if got.StateID != 1 {
		t.Errorf("TryPlace: StateID = %d, want 1 (stone)", got.StateID)
	}
	if got.Target != pos(0, 4, 0) {
		t.Errorf("TryPlace: Target = %+v, want (0,4,0)", got.Target)
	}
	if w.GetBlock(0, 4, 0) != 1 {
		t.Errorf("world at (0,4,0) = %d, want 1 (stone)", w.GetBlock(0, 4, 0))
	}
}

// TestTryPlace_RejectsNonPlaceable: held item not in
// ItemIDToBlockState (e.g. a tool, food, or unknown item).
// Asserts: result.Placed=false, Reason=PlaceNotPlaceable,
// world UNCHANGED. We use itemID=999999 which is outside the
// minecraft-data range entirely, so the map lookup is
// guaranteed to miss.
func TestTryPlace_RejectsNonPlaceable(t *testing.T) {
	w := world.New(42)
	// Pre-condition: target is air.
	if w.GetBlock(0, 4, 0) != world.BlockAir {
		t.Fatalf("precondition: (0,4,0) = %d, want air", w.GetBlock(0, 4, 0))
	}
	// 999999 = out-of-range (minecraft-data max item ID is ~4000).
	req := world.PlaceRequest{
		Clicked:  pos(0, 3, 0),
		Face:     world.FaceUp,
		HeldItem: 999999,
		PlayerX:  1000.5, PlayerY: 4, PlayerZ: 1000.5,
	}
	got := world.TryPlace(w, req)
	if got.Placed {
		t.Errorf("TryPlace: Placed = true, want false (stick is not placeable)")
	}
	if got.Reason != world.PlaceNotPlaceable {
		t.Errorf("TryPlace: Reason = %v, want PlaceNotPlaceable", got.Reason)
	}
	// World MUST be unchanged on rejection.
	if w.GetBlock(0, 4, 0) != world.BlockAir {
		t.Errorf("world at (0,4,0) = %d, want air (rejection MUST NOT mutate world)", w.GetBlock(0, 4, 0))
	}
}

// TestTryPlace_RejectsPlayerIntersect: target block is inside the
// player's AABB. Asserts: Placed=false, Reason=PlaceIntersectsPlayer,
// world UNCHANGED. This is the "player would be stuck inside the
// block" guard.
func TestTryPlace_RejectsPlayerIntersect(t *testing.T) {
	w := world.New(42)
	// Player at (0.5, 4, 0.5) — feet inside the unit cube [0,1)×[4,5)×[0,1)
	// because [0.5-0.3, 0.5+0.3]×[4, 5.8]×[0.5-0.3, 0.5+0.3] overlaps [0,1)×[4,5)×[0,1).
	// Try to place at (0, 4, 0) — inside the AABB.
	req := world.PlaceRequest{
		Clicked:  pos(0, 3, 0),
		Face:     world.FaceUp,
		HeldItem: 1, // stone
		PlayerX:  0.5, PlayerY: 4, PlayerZ: 0.5,
	}
	got := world.TryPlace(w, req)
	if got.Placed {
		t.Errorf("TryPlace: Placed = true, want false (target intersects player AABB)")
	}
	if got.Reason != world.PlaceIntersectsPlayer {
		t.Errorf("TryPlace: Reason = %v, want PlaceIntersectsPlayer", got.Reason)
	}
	if w.GetBlock(0, 4, 0) != world.BlockAir {
		t.Errorf("world at (0,4,0) = %d, want air (rejection MUST NOT mutate world)", w.GetBlock(0, 4, 0))
	}
}

// TestTryPlace_RejectsOccupied: target is not air (e.g. stone
// already there). Asserts: Placed=false, Reason=PlaceTargetOccupied,
// world UNCHANGED.
func TestTryPlace_RejectsOccupied(t *testing.T) {
	w := world.New(42)
	// Pre-seed the target with stone.
	w.SetBlock(0, 4, 0, world.BlockStone)
	req := world.PlaceRequest{
		Clicked:  pos(0, 3, 0),
		Face:     world.FaceUp,
		HeldItem: 1, // stone
		PlayerX:  1000.5, PlayerY: 4, PlayerZ: 1000.5,
	}
	got := world.TryPlace(w, req)
	if got.Placed {
		t.Errorf("TryPlace: Placed = true, want false (target occupied)")
	}
	if got.Reason != world.PlaceTargetOccupied {
		t.Errorf("TryPlace: Reason = %v, want PlaceTargetOccupied", got.Reason)
	}
	if w.GetBlock(0, 4, 0) != world.BlockStone {
		t.Errorf("world at (0,4,0) = %d, want stone (existing block MUST be preserved)", w.GetBlock(0, 4, 0))
	}
}

// TestIntersectsAABB: pins the geometric operation. 6 boundary
// cases: block at (0,4,0) and player at various positions.
func TestIntersectsAABB(t *testing.T) {
	cases := []struct {
		label      string
		px, py, pz float64
		bx, by, bz float64
		want       bool
	}{
		// Player feet AT the block's Y — definitely intersects (Y ranges overlap at 4).
		{"feet-at-block-y", 0.5, 4, 0.5, 0, 4, 0, true},
		// Player standing on the block — feet at Y=5, head at Y=6.8, block at Y=4 (3-4-5). No overlap.
		{"standing-on-block", 0.5, 5, 0.5, 0, 4, 0, false},
		// Player far away.
		{"far-away", 100, 100, 100, 0, 4, 0, false},
		// Player's feet adjacent — half block to the side. Still no overlap (player x ∈ [0.2, 0.8], block x ∈ [0, 1) — OVERLAPS at x).
		// Actually 0.5 is in the middle of the block, so this is the center case. Pick a more interesting one:
		{"player-edge-touches-block-edge", 1.0, 4, 0.5, 0, 4, 0, true}, // player x in [0.7, 1.3], block x in [0, 1) — overlap at [0.7, 1)
		// Player's head is at Y=5.8. Block at Y=5 (5-6-7). Player head overlaps block Y range [5, 6).
		{"head-into-block", 0.5, 4.5, 0.5, 0, 5, 0, true}, // feet at 4.5, head at 6.3, block y in [5, 6) — overlap
		// Player's feet just below a block, head reaches it.
		{"head-just-reaches", 0.5, 3.5, 0.5, 0, 5, 0, true}, // feet 3.5, head 5.3, block y in [5, 6) — overlap
	}
	for _, c := range cases {
		got := world.IntersectsAABB(c.px, c.py, c.pz, c.bx, c.by, c.bz)
		if got != c.want {
			t.Errorf("%s: IntersectsAABB(%v,%v,%v at %v,%v,%v) = %v, want %v",
				c.label, c.px, c.py, c.pz, c.bx, c.by, c.bz, got, c.want)
		}
	}
}
