// Package player_test — integration tests for player-initiated world changes
// (place / dig) with persistence enabled. Verifies that the world state
// changes triggered by player packets are actually saved to disk and
// reloaded correctly.
package player_test

import (
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"goore/internal/config"
	"goore/internal/player"
	"goore/internal/protocol"
	"goore/internal/protocol/v772"
	"goore/internal/world"
)

// TestUseItemOnPersistsAfterSaveAndReload simulates the user's real
// scenario: player joins a persistent world, places several blocks via
// use_item_on, digs one of them, the server is asked to save, and a
// fresh world (simulating server restart) is asked to load. The placed
// blocks MUST survive the dig AND the restart. The dug block MUST
// remain as air.
//
// This is a regression test for a real bug where placed blocks were
// silently lost on dig/restart because the world.SetBlock call in
// handleUseItemOn wasn't actually marking the chunk dirty (or the
// flusher wasn't picking it up).
func TestUseItemOnPersistsAfterSaveAndReload(t *testing.T) {
	dir := t.TempDir()

	// Phase 1: server session — player connects, places blocks, digs one.
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	w := world.NewWithDir(0, dir)
	proto := v772.New()
	cfg := config.DefaultConfig()
	cfg.ViewDist = 2
	p := player.New(42, serverConn, proto, w, cfg)
	p.X = 10
	p.Y = 4
	p.Z = 0.5 // out of the way

	serverPackets := startPacketReader(clientConn)

	go p.HandleConn()

	handshakeAndLogin(t, clientConn, cfg)
	configAckAndEnterPlay(t, clientConn, serverPackets)
	drainChunks(t, serverPackets)

	// Hold dirt (item id=28) in hotbar slot 0. Must come after
	// drainChunks because sendInventory (called from enterPlay)
	// resets p.Hotbar/p.HeldItem to the default hotbar.
	p.SetHeldItemForTest(28) // dirt
	p.HeldSlot = 0

	// Place 3 dirt blocks at distinct positions: (0, 4, 0), (1, 4, 0), (2, 4, 0).
	// All are in chunk (0, 0).
	placeBlocks := []struct{ x, y, z int32 }{
		{0, 4, 0}, {1, 4, 0}, {2, 4, 0},
	}
	// Each placement: right-click the +Y face of the block BELOW the target.
	// i.e. to place at (0, 4, 0) we right-click +Y on (0, 3, 0).
	clickTargets := []struct{ x, y, z, face int32 }{
		{0, 3, 0, 1}, // +Y of (0,3,0) → place at (0,4,0)
		{1, 3, 0, 1}, // +Y of (1,3,0) → place at (1,4,0)
		{2, 3, 0, 1}, // +Y of (2,3,0) → place at (2,4,0)
	}
	for i, click := range clickTargets {
		uw := &protocol.WireWriter{}
		uw.VarInt(0) // hand
		uw.Int64(encodePos(int(click.x), int(click.y), int(click.z)))
		uw.VarInt(click.face)
		uw.Float32(0.5)
		uw.Float32(0.5)
		uw.Float32(0.5)
		uw.Bool(false)
		uw.Bool(false) // worldBorderHit (1.21.8)
		uw.VarInt(0)   // sequence (1.21.8, block_update ack)
		if uw.Err() != nil {
			t.Fatalf("build block_place %d: %v", i, uw.Err())
		}
		// CRITICAL: use the literal vanilla 1.21.8 packet ID 0x3F, NOT
		// the v772.PlayBlockPlace constant. If the constant is
		// accidentally re-mapped to the wrong ID (e.g. the 1.20.x
		// use_item_on=0x34), this test should still catch the bug.
		// Regression test for the 1.21.8 packet ID migration.
		const vanillaBlockPlaceID = 0x3F
		if _, err := clientConn.Write(protocol.MakePacket(vanillaBlockPlaceID, uw.Bytes())); err != nil {
			t.Fatalf("write block_place %d: %v", i, err)
		}
		// Each placement sends 2 packets: block_update + set_slot.
		blk := readPacket(t, serverPackets, 2*time.Second)
		if blk.id != v772.PlayBlockUpdate {
			t.Fatalf("placement %d: expected BlockUpdate (0x%02X), got 0x%02X",
				i, v772.PlayBlockUpdate, blk.id)
			_ = blk
		}
		refill := readPacket(t, serverPackets, 2*time.Second)
		if refill.id != v772.PlaySetSlot {
			t.Fatalf("placement %d: expected SetSlot refill (0x%02X), got 0x%02X",
				i, v772.PlaySetSlot, refill.id)
		}
	}

	// Verify the in-memory world has all 3 placed blocks.
	for _, pt := range placeBlocks {
		got := w.GetBlock(int(pt.x), int(pt.y), int(pt.z))
		if got != world.BlockDirt {
			t.Errorf("after placement: world.GetBlock(%d,%d,%d) = %d, want %d (dirt)",
				pt.x, pt.y, pt.z, got, world.BlockDirt)
		}
	}

	// Verify the chunk is marked dirty (will be saved).
	if !w.IsDirty(world.ChunkPos{X: 0, Z: 0}) {
		t.Errorf("chunk (0,0) not marked dirty after placements")
	}

	// Dig the block at (1, 3, 0) — that's the GRASS, not the placed one.
	// The user-reported bug: digging wipes the placed blocks.
	dw := &protocol.WireWriter{}
	dw.VarInt(0) // status: started digging
	dw.Int64(encodePos(1, 3, 0))
	dw.Byte(1)   // face
	dw.VarInt(7) // sequence
	if _, err := clientConn.Write(protocol.MakePacket(v772.PlayPlayerAction, dw.Bytes())); err != nil {
		t.Fatalf("write player_action: %v", err)
	}
	// Expect 3 packets: block_update(0), ack_player_digging(0), and refill set_slot
	blk := readPacket(t, serverPackets, 2*time.Second)
	if blk.id != v772.PlayBlockUpdate {
		t.Fatalf("dig: expected BlockUpdate, got 0x%02X", blk.id)
	}
	ack := readPacket(t, serverPackets, 2*time.Second)
	if ack.id != v772.PlayAckPlayerDigging {
		t.Fatalf("dig: expected AckPlayerDigging, got 0x%02X", ack.id)
	}

	// In-memory check: placed blocks still present, dug block is air.
	for i, pt := range placeBlocks {
		got := w.GetBlock(int(pt.x), int(pt.y), int(pt.z))
		if got != world.BlockDirt {
			t.Errorf("after dig: world.GetBlock(%d,%d,%d) (placement %d) = %d, want %d (dirt) — placed block disappeared in memory!",
				pt.x, pt.y, pt.z, i, got, world.BlockDirt)
		}
	}
	if got := w.GetBlock(1, 3, 0); got != world.BlockAir {
		t.Errorf("after dig: grass at (1,3,0) = %d, want 0 (air)", got)
	}

	// Save the world (this is what SIGINT does in main.go).
	if err := w.SaveAll(); err != nil {
		t.Fatalf("SaveAll: %v", err)
	}
	// After save, dirty should be cleared.
	if w.IsDirty(world.ChunkPos{X: 0, Z: 0}) {
		t.Errorf("chunk still dirty after SaveAll")
	}
	// Chunk file must exist on disk.
	chunkPath := filepath.Join(dir, "chunks", "0_0.chunk")
	if _, err := os.Stat(chunkPath); err != nil {
		t.Fatalf("chunk file missing: %v", err)
	}

	// Phase 2: simulate server restart. Fresh world loads from disk.
	w2 := world.NewWithDir(0, dir)
	if err := w2.LoadAll(); err != nil {
		t.Fatalf("LoadAll: %v", err)
	}

	// After reload: ALL 3 placed blocks must still be there.
	for i, pt := range placeBlocks {
		got := w2.GetBlock(int(pt.x), int(pt.y), int(pt.z))
		if got != world.BlockDirt {
			t.Errorf("after reload: world.GetBlock(%d,%d,%d) (placement %d) = %d, want %d (dirt) — placed block was not saved!",
				pt.x, pt.y, pt.z, i, got, world.BlockDirt)
		}
	}
	// And the dug block is still air.
	if got := w2.GetBlock(1, 3, 0); got != world.BlockAir {
		t.Errorf("after reload: grass at (1,3,0) = %d, want 0 (air)", got)
	}
}
