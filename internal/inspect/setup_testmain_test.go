// Manual test helper: run with `go test -run TestMain_SetupWorld -tags setup`
// or just `go test -run TestMain_SetupWorld ./internal/inspect/` (no tag needed).
//
// This is NOT a real test — it just creates a sample world directory
// for manual inspection with the inspect command. Use:
//
//	go test -run TestMain_SetupWorld -v ./internal/inspect/
//	./inspect /tmp/sampleworld
package inspect

import (
	"os"
	"path/filepath"
	"testing"

	"goore/internal/player"
	"goore/internal/world"
)

// TestMain_SetupWorld creates /tmp/sampleworld with a valid world.meta,
// 2 player files, and 1 chunk file. The test is allowed to fail
// silently (it just sets up files; the actual assertion is manual
// via the inspect command).
func TestMain_SetupWorld(t *testing.T) {
	dir := "/tmp/sampleworld"
	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("remove: %v", err)
	}

	// Create world with seed 42, save it.
	w := world.NewWithDir(42, dir)
	if err := w.SaveAll(); err != nil {
		t.Fatalf("SaveAll: %v", err)
	}
	// Force one chunk to be created (SaveAll on a fresh world
	// might not write anything if no chunks are dirty).
	c := w.GetChunk(world.ChunkPos{X: 0, Z: 0})
	_ = c
	if err := w.SaveAll(); err != nil {
		t.Fatalf("SaveAll: %v", err)
	}

	// Create 2 players.
	p1 := &player.Player{
		Name:     "Steve",
		UUID:     [16]byte{0x0a, 0x1b, 0x2c, 0x3d, 0x4e, 0x5f, 0x60, 0x70, 0x80, 0x90, 0xa0, 0xb0, 0xc0, 0xd0, 0xe0, 0xf0},
		X:        123.5,
		Y:        4.0,
		Z:        -42.5,
		Yaw:      90.0,
		Pitch:    0.0,
		OnGround: true,
		Hotbar:   [9]int32{1, 9, 0, 85, 0, 0, 0, 0, 0},
		HeldSlot: 0,
	}
	p1.HeldItem = p1.Hotbar[p1.HeldSlot]
	if err := player.SavePlayer(dir, p1); err != nil {
		t.Fatalf("SavePlayer Steve: %v", err)
	}

	p2 := &player.Player{
		Name:     "Alex",
		UUID:     [16]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10},
		X:        0.5,
		Y:        4.0,
		Z:        0.5,
		Yaw:      0.0,
		Pitch:    0.0,
		OnGround: true,
		Hotbar:   [9]int32{0, 0, 0, 0, 0, 0, 0, 0, 0},
		HeldSlot: 0,
	}
	p2.HeldItem = p2.Hotbar[p2.HeldSlot]
	if err := player.SavePlayer(dir, p2); err != nil {
		t.Fatalf("SavePlayer Alex: %v", err)
	}

	// Verify what was created.
	t.Logf("Created sample world at %s", dir)
	if err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			t.Logf("  %s (%d bytes)", path, info.Size())
		}
		return nil
	}); err != nil {
		t.Fatalf("walk: %v", err)
	}
}
