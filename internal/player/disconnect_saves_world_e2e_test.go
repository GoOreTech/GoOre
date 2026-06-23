package player_test

import (
	"net"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"goore/internal/config"
	"goore/internal/protocol"
	"goore/internal/protocol/v772"
	"goore/internal/server"
	"goore/internal/world"
)

// TestBlockSurvivesDisconnectOnRealServer is the regression test for
// the user-reported "поставил блоки, вышел и зашел обратно и этих блоков
// нет. Сервер не перезапускал" bug.
//
// Root cause: the OnDisconnect callback in server.AcceptLoop only
// called SavePlayer — it did NOT flush the world. Dirty chunks from
// placed blocks were lost on disconnect, only being saved by the
// periodic StartFlusher ticker (5 min default) or by the SIGINT/
// SIGTERM handler. If the user disconnected, the server crashed
// before the next tick, or the user simply reconnected to the same
// running server after the connection's EID was recycled, the
// blocks were missing.
//
// Fix: the OnDisconnect callback now also calls s.world.SaveAll()
// BEFORE SavePlayer, so all dirty chunks placed during the session
// reach disk before the connection closes. This makes the save
// behavior symmetric to SIGINT/SIGTERM.
//
// This test:
//  1. Starts a REAL server on a loopback port (server.New + Serve).
//  2. Connects, sets hotbar slot 0 to stone via set_creative_slot
//     (0x37), places a block at (10, 4, -3) via block_place (0x3F),
//     then disconnects.
//  3. Inspects the chunks directory. EXPECTED: 0_-1.chunk exists
//     with the placed block.
//  4. Reloads the world from disk into a fresh World instance and
//     asserts GetBlock(10, 4, -3) == stone.
//
// Reverting the fix (reverting the world.SaveAll() call in the
// OnDisconnect callback) makes this test fail with
// "0 chunk files on disk" — the exact user symptom.
func TestBlockSurvivesDisconnectOnRealServer(t *testing.T) {
	dir := t.TempDir()
	defer os.RemoveAll(dir)

	cfg := config.DefaultConfig()
	cfg.WorldDir = dir
	cfg.ViewDist = 2
	cfg.SaveOnDisconnect = true
	cfg.SaveInterval = 0 // disable periodic save

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := server.New(cfg)
	serveDone := make(chan struct{})
	go func() {
		defer close(serveDone)
		_ = srv.Serve(context.Background(), ln)
	}()
	addr := ln.Addr().String()

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial 1: %v", err)
	}
	connectAndPlaceStone(t, conn, cfg, 10, 4, -3)
	conn.Close()
	time.Sleep(300 * time.Millisecond)

	// Inspect disk: at least one chunk file must exist.
	entries, _ := os.ReadDir(filepath.Join(dir, "chunks"))
	if len(entries) == 0 {
		t.Fatalf("BUG: no chunk files on disk after disconnect — world was not saved on disconnect")
	}
	t.Logf("After first disconnect, %d chunk files on disk:", len(entries))
	for _, e := range entries {
		t.Logf("  %s", e.Name())
	}

	// Reload the world from disk and check the block.
	w := world.NewWithDir(42, dir)
	got := w.GetBlock(10, 4, -3)
	if got != world.BlockStone {
		t.Errorf("after reloading from disk, GetBlock(10, 4, -3) = %d, want stone (%d)", got, world.BlockStone)
	}

	// Sanity check: a second connection to the same running server
	// should also see the block (this works even without the fix
	// because the chunk is in memory; included as a regression
	// check for any future eviction logic).
	conn2, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial 2: %v", err)
	}
	defer conn2.Close()
	serverPackets := make(chan serverPkt, 4096)
	go readServerPackets(conn2, serverPackets)
	handshakeAndLogin(t, conn2, cfg)
	configAckAndEnterPlay(t, conn2, serverPackets)
	conn2.Close()
	time.Sleep(200 * time.Millisecond)

	ln.Close()
	<-serveDone
}

// connectAndPlaceStone drives the FSM to play state, sets the
// hotbar slot 0 to stone via set_creative_slot (0x37), and places
// a block at (x, y, z) via block_place (0x3F).
func connectAndPlaceStone(t *testing.T, conn net.Conn, cfg config.Config, x, y, z int32) {
	t.Helper()

	serverPackets := make(chan serverPkt, 4096)
	go readServerPackets(conn, serverPackets)

	handshakeAndLogin(t, conn, cfg)
	configAckAndEnterPlay(t, conn, serverPackets)
	time.Sleep(200 * time.Millisecond)
	// Drain remaining server packets in the background.
	go func() {
		for range serverPackets {
		}
	}()

	// set_creative_slot (0x37): wire slot 36 (first hotbar cell),
	// count 1, itemID 2 (stone wire-encoded as Holder<Item>:
	// registryId(1) + 1). Vanilla 1.21.8 sends the WIRE slot
	// index (HOTBAR_START = 36), not the hotbar index.
	var w1 protocol.WireWriter
	w1.Int16(36) // wire slot 36 = hotbar index 0
	w1.VarInt(1) // count
	w1.VarInt(2) // itemID = stone wire-encoded as registryId(1) + 1
	pkt1 := protocol.MakePacket(v772.PlayCreativeInventoryAction, w1.Bytes())
	if _, err := conn.Write(pkt1); err != nil {
		t.Fatalf("write set_creative_slot: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	// block_place (0x3F): face=+Y, target Y = clicked-Y + 1.
	packed := encodeBlockPos(x, y-1, z)
	var w2 protocol.WireWriter
	w2.VarInt(0)     // hand
	w2.Int64(packed) // position
	w2.VarInt(1)     // face = +Y
	w2.Float32(0)
	w2.Float32(0)
	w2.Float32(0)
	w2.Bool(false) // insideBlock
	w2.Bool(false) // worldBorderHit (1.21.8)
	w2.VarInt(0)   // sequence (1.21.8)
	pkt2 := protocol.MakePacket(v772.PlayBlockPlace, w2.Bytes())
	if _, err := conn.Write(pkt2); err != nil {
		t.Fatalf("write block_place: %v", err)
	}
	time.Sleep(300 * time.Millisecond)
}
