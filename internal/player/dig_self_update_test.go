// Package player_test — regression for the user-reported bug
// "когда ломаю блок он снова появляется, но при перезаходе на сервер
// все корректно".
//
// Root cause (block prediction, 1.19.3+ / 1.21.8):
//
//	The vanilla client predicts a block break locally (sets it to air in
//	its own world) and records the prediction keyed by the `sequence`
//	carried in block_dig (0x28). It then waits for the server to confirm
//	the prediction with TWO packets, in order:
//	  1. block_update (0x08) for the broken position carrying the
//	     AUTHORITATIVE state (air) — this is what the client compares its
//	     prediction against.
//	  2. block_changed_ack / acknowledge_player_digging (0x04) echoing
//	     the sequence — this tells the client "resolve all predictions up
//	     to this sequence now".
//	On the ack, the client looks at the buffered block_update for each
//	pending predicted position: if the server's authoritative state
//	matches the prediction (air), the break commits; if NO block_update
//	was received for that position, the prediction is UNCONFIRMED and the
//	client REVERTS it → the block reappears client-side.
//
//	The GoOre world WAS correctly mutated (world.TryBreak set air), and
//	OnDisconnect / SIGINT saves the chunk, so on rejoin the block loads
//	from disk as air → "при перезаходе всё корректно". But LIVE, the
//	breaker only got the ack (serverPlayerHooks.Broadcast deliberately
//	EXCLUDES the originator), never the block_update for its own block,
//	so the client reverted the prediction → "блок снова появляется".
//
//	The standalone test TestPlayerActionDigCreative uses noOpHooks whose
//	Broadcast falls back to a self-send, so the breaker accidentally got
//	its own block_update in tests — the production divergence was
//	invisible to the unit test. This is exactly why this regression uses a
//	REAL server (serverPlayerHooks).
//
// Fix:
//
//	BroadcastAll (send to ALL players INCLUDING the originator) replaces
//	Broadcast on the dig/place block_update path. The breaker now receives
//	its own block_update, the prediction is confirmed, and the block stays
//	broken live.
package player_test

import (
	"context"
	"net"
	"testing"
	"time"

	"goore/internal/config"
	"goore/internal/protocol"
	"goore/internal/protocol/v772"
	"goore/internal/server"
)

// TestDiggerReceivesOwnBlockUpdate_OnRealServer is the regression test for
// the "блок снова появляется после копания" bug. On a REAL server
// (serverPlayerHooks, which excludes the originator from Broadcast), the
// breaker MUST still receive its own block_update (0x08) for the block it
// broke — otherwise the vanilla client reverts its break prediction and
// the block reappears client-side even though the server mutated the world.
//
// Reverting the fix (digging.go back to p.hooks.Broadcast) makes this test
// fail with "did not receive block_update (0x08) for own dig" — the exact
// user symptom.
func TestDiggerReceivesOwnBlockUpdate_OnRealServer(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.WorldDir = dir
	cfg.ViewDist = 2
	cfg.SaveOnDisconnect = true
	cfg.SaveInterval = 0

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
	defer func() {
		ln.Close()
		<-serveDone
	}()
	addr := ln.Addr().String()

	// Single player on a real server → serverPlayerHooks installed.
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	pkts := startPacketReader(conn)
	handshakeAndLogin(t, conn, cfg)
	configAckAndEnterPlay(t, conn, pkts)
	drainChunks(t, pkts)
	time.Sleep(100 * time.Millisecond)

	// Dig grass_block at (0, 3, 0) — status 0 (started, creative instant-break).
	// Wire: status(VarInt) + position(i64) + face(u8) + sequence(VarInt).
	const digSeq = int32(7)
	var w protocol.WireWriter
	w.VarInt(0)                 // status = started
	w.Int64(encodePos(0, 3, 0)) // (0, 3, 0)
	w.Byte(1)                   // face = +Y
	w.VarInt(digSeq)            // sequence
	if w.Err() != nil {
		t.Fatalf("build block_dig: %v", w.Err())
	}
	if _, err := conn.Write(protocol.MakePacket(v772.PlayPlayerAction, w.Bytes())); err != nil {
		t.Fatalf("write block_dig: %v", err)
	}

	// The breaker MUST receive its OWN block_update (0x08) for (0, 3, 0)
	// with stateID 0 (air). Under the bug (Broadcast excludes origin), this
	// times out → "блок снова появляется".
	blk, ok := waitForPacketID(pkts, v772.PlayBlockUpdate, 2*time.Second)
	if !ok {
		t.Fatal("BUG: digger did not receive block_update (0x08) for its own broken block " +
			"— the vanilla client reverts its break prediction and the block reappears " +
			"(\"когда ломаю блок он снова появляется\"). BroadcastAll must include the originator.")
	}
	r := protocol.NewWireReader(blk.data)
	packed := r.Int64()
	gotX := int32(packed >> 38)
	gotY := int32(packed << 52 >> 52)
	gotZ := int32(packed << 26 >> 38)
	if gotX != 0 || gotY != 3 || gotZ != 0 {
		t.Errorf("own block_update pos = (%d,%d,%d), want (0,3,0)", gotX, gotY, gotZ)
	}
	if stateID := r.VarInt(); stateID != 0 {
		t.Errorf("own block_update stateID = %d, want 0 (air)", stateID)
	}
	if r.Err() != nil {
		t.Errorf("block_update decode: %v", r.Err())
	}

	// The ack (0x04) MUST follow, echoing the sequence. The order matters:
	// block_update (authoritative state) THEN ack (resolve prediction) is
	// the vanilla contract; reversing it leaves the prediction unresolved
	// when the ack fires.
	ack, ok := waitForPacketID(pkts, v772.PlayAckPlayerDigging, 2*time.Second)
	if !ok {
		t.Fatal("did not receive block_changed_ack (0x04) after own block_update")
	}
	ra := protocol.NewWireReader(ack.data)
	if got := ra.VarInt(); got != digSeq {
		t.Errorf("ack sequence = %d, want %d", got, digSeq)
	}

	// The block_update MUST arrive BEFORE the ack. We consumed them in
	// order above via waitForPacketID which discards non-matches, so the
	// ordering is implicitly asserted (the first 0x08 was found before we
	// looked for the first 0x04). Nothing further needed here.
}
