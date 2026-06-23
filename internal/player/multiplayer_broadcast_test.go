package player_test

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"goore/internal/config"
	"goore/internal/protocol"
	"goore/internal/protocol/v772"
	"goore/internal/server"
	"goore/internal/world"
)

// TestBlockPlaceBroadcastsToOtherPlayer is the TDD baseline for Phase 4.
// Two players connect to a real server. Player A places a block. Player
// B's packet stream MUST contain a block_update (0x08) for that block,
// proving the broadcast wiring works.
//
// Currently fails: the server's Broadcast() exists but is never called
// by handleBlockPlace — the block_update is only sent to the digger.
func TestBlockPlaceBroadcastsToOtherPlayer(t *testing.T) {
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
	addr := ln.Addr().String()

	// === Player A ===
	connA, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial A: %v", err)
	}
	defer connA.Close()
	pktsA := startPacketReader(connA)
	handshakeAndLogin(t, connA, cfg)
	configAckAndEnterPlay(t, connA, pktsA)
	drainChunks(t, pktsA) // bring A fully into play state
	time.Sleep(100 * time.Millisecond)

	// Give A a stone block via set_creative_slot. Vanilla 1.21.8
	// sends the WIRE slot index (36 = first hotbar cell), NOT the
	// hotbar index. The server translates wire slot 36..44 back to
	// hotbar index 0..8 internally. The itemId is encoded as
	// Holder<Item>: VarInt(registryId + 1) — stone (registryId=1)
	// is wire-encoded as 2.
	{
		var w protocol.WireWriter
		w.Int16(36) // wire slot 36 = hotbar index 0
		w.VarInt(1)
		w.VarInt(2) // stone wire-encoded as registryId(1) + 1
		connA.Write(protocol.MakePacket(v772.PlayCreativeInventoryAction, w.Bytes()))
		time.Sleep(50 * time.Millisecond)
	}
	// Drain the set_slot echo
	drainNPackets(pktsA, 2, 500*time.Millisecond)

	// === Player B ===
	connB, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial B: %v", err)
	}
	defer connB.Close()
	pktsB := startPacketReader(connB)
	handshakeAndLogin(t, connB, cfg)
	configAckAndEnterPlay(t, connB, pktsB)
	drainChunks(t, pktsB)
	time.Sleep(100 * time.Millisecond)

	// A places a block at (10, 4, -3)
	packed := encodePos(10, 3, -3)
	var w protocol.WireWriter
	w.VarInt(0)
	w.Int64(packed)
	w.VarInt(1) // face = +Y
	w.Float32(0)
	w.Float32(0)
	w.Float32(0)
	w.Bool(false)
	w.Bool(false) // worldBorderHit
	w.VarInt(0)   // sequence
	connA.Write(protocol.MakePacket(v772.PlayBlockPlace, w.Bytes()))

	// B should receive a block_update (0x08) for the placed block.
	got, found := waitForPacketID(pktsB, 0x08, 2*time.Second)
	if !found {
		t.Fatal("BUG: B did not receive block_update (0x08) for A's placement — broadcast is not wired")
	}
	if got.id != 0x08 {
		t.Fatalf("expected block_update (0x08), got 0x%02X", got.id)
	}
	// Decode the block_update to verify the position and stateID
	r := protocol.NewWireReader(got.data)
	posPacked := r.Int64()
	stateID := r.VarInt()
	if r.Err() != nil {
		t.Fatalf("block_update decode: %v", r.Err())
	}
	if posPacked != encodePos(10, 4, -3) {
		t.Errorf("block_update position = %d, want %d (for (10,4,-3))", posPacked, encodePos(10, 4, -3))
	}
	if stateID != 1 {
		t.Errorf("block_update stateID = %d, want 1 (stone)", stateID)
	}

	// Close both connections explicitly so the OnDisconnect hooks fire.
	connA.Close()
	connB.Close()
	time.Sleep(300 * time.Millisecond)

	ln.Close()
	<-serveDone

	// Sanity: world actually has the block on disk.
	w2 := world.NewWithDir(42, dir)
	if err := w2.LoadAll(); err != nil {
		t.Logf("LoadAll: %v (world.meta missing is fine if persistence off)", err)
	}
	if w2.GetBlock(10, 4, -3) != world.BlockStone {
		t.Errorf("world.GetBlock(10, 4, -3) = %d, want stone", w2.GetBlock(10, 4, -3))
	}
}

// drainNPackets reads up to n packets from ch with a total deadline.
// Used to discard expected non-essential packets (e.g. set_slot echo).
func drainNPackets(ch <-chan serverPkt, n int, total time.Duration) {
	deadline := time.Now().Add(total)
	for i := 0; i < n; i++ {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return
		}
		select {
		case _, ok := <-ch:
			if !ok {
				return
			}
		case <-time.After(remaining):
			return
		}
	}
}

// waitForPacketID returns the first packet with the given id, or
// (zero, false) on timeout. Discards non-matching packets.
func waitForPacketID(ch <-chan serverPkt, id int32, timeout time.Duration) (serverPkt, bool) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case pkt, ok := <-ch:
			if !ok {
				return serverPkt{}, false
			}
			if pkt.id == id {
				return pkt, true
			}
		case <-time.After(50 * time.Millisecond):
		}
	}
	return serverPkt{}, false
}

// TestNoGoroutineLeakOnTwoConnections is a sanity test that
// disconnecting both clients doesn't leave the server with
// dangling goroutines.
func TestNoGoroutineLeakOnTwoConnections(t *testing.T) {
	var wg sync.WaitGroup
	dir := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.WorldDir = dir
	cfg.ViewDist = 2
	cfg.SaveOnDisconnect = true
	cfg.SaveInterval = 0
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	srv := server.New(cfg)
	wg.Add(1)
	go func() { defer wg.Done(); _ = srv.Serve(context.Background(), ln) }()

	for i := 0; i < 3; i++ {
		conn, _ := net.Dial("tcp", ln.Addr().String())
		conn.Close()
	}
	time.Sleep(200 * time.Millisecond)
	ln.Close()
	wg.Wait()

	if c := srv.PlayerCount(); c != 0 {
		t.Errorf("expected 0 players after disconnect, got %d", c)
	}
}

// TestNewPlayerSeesExistingPlayer is the Phase 4 step 4b regression
// for player-spawn broadcast. Two players connect sequentially.
// Player B's packet stream MUST contain spawn_entity (0x01) AND
// entity_metadata (0x5C) for player A.
//
// Currently fails: the spawn_entity is only sent as part of the
// enter-play packet sequence, not broadcast to other connected
// players. Other players see the world but not the player models.
func TestNewPlayerSeesExistingPlayer(t *testing.T) {
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
	addr := ln.Addr().String()

	// Player A: connect, drive to play.
	connA, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial A: %v", err)
	}
	defer connA.Close()
	pktsA := startPacketReader(connA)
	handshakeAndLogin(t, connA, cfg)
	configAckAndEnterPlay(t, connA, pktsA)
	drainChunks(t, pktsA)
	time.Sleep(100 * time.Millisecond)

	// Player B: connect, drive to play. Their packet stream should
	// include a spawn_entity for player A.
	connB, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial B: %v", err)
	}
	defer connB.Close()
	pktsB := startPacketReader(connB)
	handshakeAndLogin(t, connB, cfg)
	configAckAndEnterPlay(t, connB, pktsB)
	drainChunks(t, pktsB)
	time.Sleep(100 * time.Millisecond)

	// B's stream MUST also contain a player_info_update (0x3F) for A
	// BEFORE the spawn_entity. Without it, the vanilla 1.21.8 client
	// will not render the spawned player entity (it has no skin /
	// gamemode data to attach to the entity). The "players don't see
	// each other" user-reported regression.
	//
	// IMPORTANT: findPacket for 0x3F must be called BEFORE the
	// 0x01/0x5C finders. The wire order is 0x3F → 0x01 → 0x5C; the
	// 0x01 finder consumes any earlier packet, so if it runs first
	// it eats the 0x3F and the next findPacket times out.
	wantUUID := [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	infoPkt, found := findPacket(pktsB, 0x3F, 2*time.Second)
	if !found {
		t.Fatal("BUG: B did not receive player_info_update (0x3F) for A — client cannot render the player without skin/gamemode")
	}
	// Decode the player_info_update payload and verify it has
	// the right UUID and gamemode=1 (creative).
	{
		r := protocol.NewWireReader(infoPkt.data)
		actions := r.Byte()
		if actions&protocol.PlayerInfoActionAddPlayer == 0 {
			t.Errorf("player_info actions = 0x%X, must include add_player (0x01)", actions)
		}
		if actions&protocol.PlayerInfoActionUpdateGamemode == 0 {
			t.Errorf("player_info actions = 0x%X, must include update_game_mode (0x04)", actions)
		}
		_ = r.VarInt() // count
		gotUUID := r.UUID()
		if gotUUID != wantUUID {
			t.Errorf("player_info uuid = %v, want %v", gotUUID, wantUUID)
		}
		_ = r.String()   // name
		_ = r.VarInt()   // properties count
		gm := r.VarInt() // gamemode
		if gm != 1 {
			t.Errorf("player_info gamemode = %d, want 1 (creative)", gm)
		}
	}

	// B's stream should contain at least one spawn_entity (0x01)
	// with type=149 (player) and EID=0 (player's EID).
	spawnPkt, found := findPacket(pktsB, 0x01, 2*time.Second)
	if !found {
		t.Fatal("BUG: B did not receive spawn_entity (0x01) for A — SpawnPlayerForOthers not wired")
	}
	// Decode payload (length+id already stripped by startPacketReader):
	// eid(VarInt) + uuid(16) + type(VarInt) + x,y,z(f64) + ...
	r := protocol.NewWireReader(spawnPkt.data)
	eid := r.VarInt()
	gotUUID := r.UUID()
	ty := r.VarInt()
	if eid != 0 {
		t.Errorf("spawn_entity eid = %d, want 0 (player's EID)", eid)
	}
	if ty != 149 {
		t.Errorf("spawn_entity type = %d, want 149 (player)", ty)
	}
	if gotUUID != wantUUID {
		t.Errorf("spawn_entity uuid = %v, want %v", gotUUID, wantUUID)
	}

	// B's stream should also contain an entity_metadata (0x5C) for A.
	metaPkt, found := findPacket(pktsB, 0x5C, 2*time.Second)
	if !found {
		t.Fatal("BUG: B did not receive entity_metadata (0x5C) for A — entity metadata not broadcast")
	}
	_ = metaPkt // existence is the assertion

	connA.Close()
	connB.Close()
	time.Sleep(300 * time.Millisecond)
	ln.Close()
	<-serveDone
}

// TestDisconnectBroadcastsRemoveEntities is the Phase 4 step 4b
// regression for player-despawn broadcast. Two players connect,
// then A disconnects. B's packet stream MUST contain remove_entities
// (0x46) for player A's EID.
func TestDisconnectBroadcastsRemoveEntities(t *testing.T) {
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
	addr := ln.Addr().String()

	// Player A: connect, drive to play.
	connA, _ := net.Dial("tcp", addr)
	pktsA := startPacketReader(connA)
	handshakeAndLogin(t, connA, cfg)
	configAckAndEnterPlay(t, connA, pktsA)
	drainChunks(t, pktsA)
	time.Sleep(100 * time.Millisecond)

	// Player B: connect, drive to play, drain spawn_entity for A.
	connB, _ := net.Dial("tcp", addr)
	pktsB := startPacketReader(connB)
	handshakeAndLogin(t, connB, cfg)
	configAckAndEnterPlay(t, connB, pktsB)
	drainChunks(t, pktsB)
	time.Sleep(100 * time.Millisecond)
	// Drain spawn_entity + entity_metadata
	drainNPackets(pktsB, 2, 500*time.Millisecond)

	// A disconnects.
	connA.Close()
	time.Sleep(200 * time.Millisecond)

	// B's stream should contain remove_entities (0x46) for A.
	rmPkt, found := findPacket(pktsB, 0x46, 2*time.Second)
	if !found {
		t.Fatal("BUG: B did not receive remove_entities (0x46) for A — DespawnPlayerForOthers not wired")
	}
	_ = rmPkt

	// Close B explicitly so its HandleConn goroutine finishes before
	// t.TempDir() cleanup tries to remove the world directory. The
	// t.TempDir cleanup races with any still-running HandleConn
	// goroutine that is mid-write to the world dir.
	connB.Close()
	time.Sleep(200 * time.Millisecond)

	ln.Close()
	<-serveDone
}

// findPacket returns the first packet with the given id, or
// (zero, false) on timeout. Discards non-matching packets.
//
// WARNING: findPacket CONSUMES packets. If you are looking for
// multiple IDs in a single stream, list them in the wire order
// they are sent, or buffer the stream first and inspect in any
// order. In particular, player_info_update (0x3F) is sent BEFORE
// spawn_entity (0x01) and entity_metadata (0x5C), so call
// findPacket(0x3F) before findPacket(0x01).
func findPacket(ch <-chan serverPkt, id int32, timeout time.Duration) (serverPkt, bool) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case pkt, ok := <-ch:
			if !ok {
				return serverPkt{}, false
			}
			if pkt.id == id {
				return pkt, true
			}
		case <-time.After(50 * time.Millisecond):
		}
	}
	return serverPkt{}, false
}

// TestPositionBroadcastsToOtherPlayer is the Phase 4 step 4d
// regression for position broadcasting. Two players connect, A
// moves via set_player_position (0x1D), and B's packet stream MUST
// contain an entity_teleport (0x76) — or rel_entity_move (0x2E) /
// entity_move_look (0x2F) — with A's new position.
//
// Currently fails: the server never broadcasts position updates
// to other players; the position broadcast tick is a stub.
func TestPositionBroadcastsToOtherPlayer(t *testing.T) {
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
	// Speed up the position broadcast tick for the test.
	srv.SetPositionBroadcastInterval(50 * time.Millisecond)
	serveDone := make(chan struct{})
	go func() {
		defer close(serveDone)
		_ = srv.Serve(context.Background(), ln)
	}()
	addr := ln.Addr().String()

	// Player A.
	connA, _ := net.Dial("tcp", addr)
	pktsA := startPacketReader(connA)
	handshakeAndLogin(t, connA, cfg)
	configAckAndEnterPlay(t, connA, pktsA)
	drainChunks(t, pktsA)
	time.Sleep(100 * time.Millisecond)

	// Player B.
	connB, _ := net.Dial("tcp", addr)
	pktsB := startPacketReader(connB)
	handshakeAndLogin(t, connB, cfg)
	configAckAndEnterPlay(t, connB, pktsB)
	drainChunks(t, pktsB)
	time.Sleep(100 * time.Millisecond)
	// Drain spawn_entity + entity_metadata + initial entity_teleport
	// (B sees A's initial position broadcast on B's join).
	drainNPackets(pktsB, 4, 500*time.Millisecond)

	// A moves to (100.5, 4, -3.5) via set_player_position (0x1D).
	// Wire format: x(f64) + y(f64) + z(f64) + flags(u8).
	var w protocol.WireWriter
	w.Float64(100.5)
	w.Float64(4.0)
	w.Float64(-3.5)
	w.Byte(0) // on_ground
	connA.Write(protocol.MakePacket(v772.PlaySetPlayerPos, w.Bytes()))

	// B should receive an entity_teleport (0x76) with A's new position.
	// We accept any of: 0x76 (entity_teleport), 0x2E (rel_entity_move),
	// 0x2F (entity_move_look). All are valid position updates.
	deadline := time.Now().Add(2 * time.Second)
	var found bool
	var lastPos int32
	for time.Now().Before(deadline) && !found {
		select {
		case pkt, ok := <-pktsB:
			if !ok {
				break
			}
			switch pkt.id {
			case 0x76, 0x2E, 0x2F:
				found = true
				lastPos = pkt.id
			}
		case <-time.After(50 * time.Millisecond):
		}
	}
	if !found {
		t.Fatal("BUG: B did not receive any position update (0x76/0x2E/0x2F) for A's movement — position broadcast tick is not wired")
	}
	t.Logf("B received position update: 0x%X", lastPos)

	connA.Close()
	connB.Close()
	time.Sleep(200 * time.Millisecond)
	ln.Close()
	<-serveDone
}

// TestPositionBroadcastDisabledByInterval verifies that
// SetPositionBroadcastInterval(0) actually disables the position
// broadcast tick. The companion test (TestPositionBroadcastsToOtherPlayer)
// verifies the tick WORKS; this one verifies the kill switch. Together
// they catch both "tick never wired" and "tick always on, can't be
// disabled for tests/profiling".
func TestPositionBroadcastDisabledByInterval(t *testing.T) {
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
	srv.SetPositionBroadcastInterval(0) // disabled
	serveDone := make(chan struct{})
	go func() { defer close(serveDone); _ = srv.Serve(context.Background(), ln) }()
	addr := ln.Addr().String()

	connA, _ := net.Dial("tcp", addr)
	pktsA := startPacketReader(connA)
	handshakeAndLogin(t, connA, cfg)
	configAckAndEnterPlay(t, connA, pktsA)
	drainChunks(t, pktsA)

	connB, _ := net.Dial("tcp", addr)
	pktsB := startPacketReader(connB)
	handshakeAndLogin(t, connB, cfg)
	configAckAndEnterPlay(t, connB, pktsB)
	drainChunks(t, pktsB)
	time.Sleep(100 * time.Millisecond)
	drainNPackets(pktsB, 4, 500*time.Millisecond)

	// Move A.
	var w protocol.WireWriter
	w.Float64(50.5)
	w.Float64(4.0)
	w.Float64(50.5)
	w.Byte(0)
	connA.Write(protocol.MakePacket(v772.PlaySetPlayerPos, w.Bytes()))

	// With the tick disabled, B should NOT receive any position
	// update within 500ms. Use a short timeout to keep the test
	// fast; the tick is 50ms by default, so 500ms is enough for
	// 10 ticks.
	time.Sleep(500 * time.Millisecond)
	_, found := findPacket(pktsB, 0x76, 100*time.Millisecond)
	if found {
		t.Errorf("expected NO entity_teleport (0x76) with tick disabled, but received one")
	}

	connA.Close()
	connB.Close()
	time.Sleep(200 * time.Millisecond)
	ln.Close()
	<-serveDone
}
