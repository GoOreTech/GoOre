package player_test

import (
	"net"
	"os"
	"testing"
	"time"

	"goore/internal/config"
	"goore/internal/player"
	"goore/internal/protocol"
	"goore/internal/protocol/v772"
	"goore/internal/world"
)

// encodeBlockPos packs (x, y, z) into the 1.21.8 wire format.
func encodeBlockPos(x, y, z int32) int64 {
	return (int64(x)&0x3FFFFFF)<<38 | (int64(z)&0x3FFFFFF)<<12 | (int64(y) & 0xFFF)
}

// TestPlaceBlockRoundTripV3 is the regression test for the user-reported
// bug "поставил блоки, вышел, перезапустил сервер, зашел и поставленные
// блоки появились не в том месте". The root cause was a wrong floor-
// division formula in World.SetBlock/GetBlock: `if z < 0 { cz = (z-15)
// >> 4 }` is a Java/C idiom for floor-division with logical shift, but
// Go's >> is arithmetic, so `z >> 4` already does the right thing. The
// old formula pushed negative chunk indices one chunk too far in the
// -X/-Z direction, so a block at world Z=-3 was saved to chunk Z=-2
// (file `0_-2.chunk`) instead of Z=-1 (`0_-1.chunk`). On restart the
// block was either gone or appeared 16 blocks away.
//
// This test places blocks at negative and boundary world coordinates
// via a real block_place packet, saves the world, simulates a restart
// with a fresh World instance, and asserts the blocks come back at the
// same coordinates.
func TestPlaceBlockRoundTripV3(t *testing.T) {
	dir := t.TempDir()
	defer os.RemoveAll(dir)

	cfg := config.DefaultConfig()
	cfg.WorldDir = dir
	cfg.ViewDist = 2

	w := world.NewWithDir(42, dir)
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	proto := v772.New()
	p := player.New(42, serverConn, proto, w, cfg)

	done := make(chan struct{})
	go func() {
		defer close(done)
		p.HandleConn()
	}()

	fsmPackets := make(chan serverPkt, 256)
	go readServerPackets(clientConn, fsmPackets)

	handshakeAndLogin(t, clientConn, cfg)
	configAckAndEnterPlay(t, clientConn, fsmPackets)
	if !waitForPlay(t, p, 3*time.Second) {
		t.Fatal("not in play state")
	}

	// Park the player far from all test positions so intersectsPlayer
	// never rejects a placement. SetPositionForTest takes posMu.Lock
	// so we don't race with the server goroutine reading p.X in Pos().
	p.SetPositionForTest(1000.5, 4, 1000.5, 0, 0)

	type place struct{ x, y, z int32 }
	cases := []place{
		// Negative Z (the main bug — chunk (0,-1) vs (0,-2))
		{10, 4, -3}, {0, 4, -1}, {15, 4, -16}, {100, 4, -100},
		{1, 4, -1},
		// Negative X
		{-1, 4, 5}, {-17, 4, 5}, {-1, 4, 1}, {-100, 4, 100},
		// Both negative
		{-1, 4, -1},
		// Positive boundaries
		{16, 4, 16}, {0, 4, 0}, {15, 4, 15},
	}

	for _, c := range cases {
		p.SetHeldItemForTest(1) // stone (locks hotbarMu; the server's sendInventorySeed goroutine writes there too)
		// face=1 (+Y): target Y = clicked-Y + 1.
		// We click on the block below the target.
		packed := encodeBlockPos(c.x, c.y-1, c.z)
		var w2 protocol.WireWriter
		w2.VarInt(0)     // hand
		w2.Int64(packed) // position
		w2.VarInt(1)     // face = +Y
		w2.Float32(0)    // cursorX
		w2.Float32(0)    // cursorY
		w2.Float32(0)    // cursorZ
		w2.Bool(false)   // insideBlock
		w2.Bool(false)   // worldBorderHit (1.21.8)
		w2.VarInt(0)     // sequence (1.21.8)
		pkt := protocol.MakePacket(v772.PlayBlockPlace, w2.Bytes())
		if _, err := clientConn.Write(pkt); err != nil {
			t.Fatalf("write: %v", err)
		}
		deadline := time.Now().Add(2 * time.Second)
		for w.GetBlock(int(c.x), int(c.y), int(c.z)) == world.BlockAir {
			if time.Now().After(deadline) {
				t.Fatalf("block not placed at (%d, %d, %d) after 2s — chunk index bug?",
					c.x, c.y, c.z)
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	if err := w.SaveAll(); err != nil {
		t.Fatalf("SaveAll: %v", err)
	}

	// Sanity: dump what we wrote.
	entries, _ := os.ReadDir(dir + "/chunks")
	t.Logf("Wrote %d chunk files:", len(entries))
	for _, e := range entries {
		t.Logf("  %s", e.Name())
	}

	// Simulate server restart: fresh World, same dir.
	w2 := world.NewWithDir(42, dir)
	for _, c := range cases {
		got := w2.GetBlock(int(c.x), int(c.y), int(c.z))
		if got != world.BlockStone {
			t.Errorf("after restart: block at (%d,%d,%d) = %d, want stone (%d)",
				c.x, c.y, c.z, got, world.BlockStone)
		}
	}

	clientConn.Close()
	<-done
}
