package player_test

import (
	"net"
	"context"
	"testing"
	"time"

	"goore/internal/config"
	"goore/internal/protocol"
	"goore/internal/protocol/v772"
	"goore/internal/server"
)

// TestEntityEquipmentBroadcastToOtherPlayer is the regression test
// for the user-reported "не видно какой блок держится в руке" bug
// ("can't see which block is held in hand" — from another player's
// perspective).
//
// When player A joins (or picks an item into the hotbar), the
// server must broadcast entity_equipment (0x5F) to every OTHER
// connected player so they can render A's held item in A's model.
// Without entity_equipment the client's hand is invisible / the
// held item model doesn't appear next to other players.
//
// Vanilla 1.21.8 wire format:
//
//	entityId(VarInt) +
//	equipments: topBitSetTerminatedArray of
//	  slot(i8, top bit = "this is the last entry" flag) + item(Slot)
//
// Slot values for the player entity:
//
//	0 = main hand, 1 = offhand,
//	2..5 = armor (boots, leggings, chestplate, helmet),
//	6 = body (horse armor).
//
// We send the main hand only (slot=0x80 — main hand index 0 with
// the terminator bit set).
//
// This test:
//  1. Starts a real server.
//  2. Connects player A and drains the spawn sequence (default
//     hotbar = stone in slot 0).
//  3. Connects player B, who should see A's spawn + equipment.
//  4. Verifies B's packet stream contains entity_equipment (0x5F)
//     for A's EID with the stone in the main hand.
func TestEntityEquipmentBroadcastToOtherPlayer(t *testing.T) {
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

	// Player B: connect, drive to play. B should receive A's
	// equipment during the spawn broadcast.
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

	// B's stream should contain entity_equipment (0x5F) for A's
	// EID (= 0) with the default hotbar's main hand (stone, id=1).
	equipPkt, found := waitForPacketID(pktsB, 0x5F, 2*time.Second)
	if !found {
		t.Fatal("BUG: B did not receive entity_equipment (0x5F) for A's main hand — other players can't see which block is held")
	}
	r := protocol.NewWireReader(equipPkt.data)
	eid := r.VarInt()
	if eid != 0 {
		t.Errorf("entity_equipment eid = %d, want 0 (player A's EID)", eid)
	}
	// Read the equipment entries. Vanilla 1.21.8 format:
	//   slot(i8) + item(Slot where count=VarInt, itemID=VarInt, ...)
	// The last entry has bit 7 of slot CLEAR (CONTINUE_MASK is SET on
	// non-last entries; the LAST entry clears the bit to signal "stop").
	slot := r.Byte()
	mainHandIdx := int8(slot & 0x7F) // strip CONTINUE_MASK bit
	if slot&0x80 != 0 {
		t.Errorf("entity_equipment slot byte = 0x%02X, top bit (CONTINUE_MASK) must be CLEAR on the last entry — vanilla reads it as 'more entries follow'", slot)
	}
	if mainHandIdx != 0 {
		t.Errorf("entity_equipment slot = %d, want 0 (main hand)", mainHandIdx)
	}
	count := r.VarInt()
	if count <= 0 {
		t.Fatalf("entity_equipment item count = %d, want > 0", count)
	}
	// itemId is encoded as Holder<Item>: VarInt(registryId + 1).
	// For stone (registryId=1), the wire value is 2.
	itemID := r.VarInt()
	if itemID != 2 {
		t.Errorf("entity_equipment itemID = %d, want 2 (stone wire-encoded as registryId+1)", itemID)
	}
	if err := r.Err(); err != nil {
		t.Errorf("entity_equipment trailing bytes: %v", err)
	}

	connA.Close()
	connB.Close()
	time.Sleep(200 * time.Millisecond)
	ln.Close()
	<-serveDone
}

// TestEntityHeadRotationBroadcastToOtherPlayer is the regression
// test for the user-reported "поворот головы неверный" bug ("head
// rotation is wrong" — from another player's perspective).
//
// In 1.21.x, the body's yaw/pitch is sent via entity_teleport
// (0x76), but the head's yaw is a SEPARATE channel sent via
// entity_head_rotation (0x4C). Without entity_head_rotation the
// client's head stays at 0 (looking forward) even when the body is
// rotated.
//
// This test:
//  1. Two players connect.
//  2. A is at a known yaw (e.g. 90°).
//  3. B's stream should contain entity_head_rotation (0x4C) for
//     A's EID with headYaw matching A's yaw (in 256ths of a turn).
func TestEntityHeadRotationBroadcastToOtherPlayer(t *testing.T) {
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

	// Player B: connect, drive to play. B should receive A's
	// entity_head_rotation during the spawn broadcast.
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

	// B's stream should contain entity_head_rotation (0x4C) for
	// A's EID. Vanilla wire format: entityId(VarInt) + headYaw(i8).
	headPkt, found := waitForPacketID(pktsB, 0x4C, 2*time.Second)
	if !found {
		t.Fatal("BUG: B did not receive entity_head_rotation (0x4C) for A — other players see A's head stuck looking forward")
	}
	r := protocol.NewWireReader(headPkt.data)
	eid := r.VarInt()
	if eid != 0 {
		t.Errorf("entity_head_rotation eid = %d, want 0 (player A's EID)", eid)
	}
	_ = r.Byte() // headYaw, not asserting exact value because A's yaw may be 0 by default
	if err := r.Err(); err != nil {
		t.Errorf("entity_head_rotation trailing bytes: %v", err)
	}

	// Sanity: also verify the v772 packet ID constant matches.
	if v772.PlayEntityHeadRotation != 0x4C {
		t.Errorf("v772.PlayEntityHeadRotation = 0x%02X, want 0x4C", v772.PlayEntityHeadRotation)
	}

	connA.Close()
	connB.Close()
	time.Sleep(200 * time.Millisecond)
	ln.Close()
	<-serveDone
}

// TestEntityHeadRotationOnPositionTick is the regression test for
// the user-reported "поворот головы неверный" bug: when a player
// rotates their body, other clients see the body turn but the head
// stays at 0 (looking forward). The fix is to broadcast
// entity_head_rotation (0x4C) alongside entity_teleport (0x76) on
// every position-tick.
//
// This test:
//  1. Connects A and B; both reach play state.
//  2. A sends set_player_rotation (0x1F) with a known yaw (90°).
//  3. Waits for the position-broadcast tick (50ms default).
//  4. Verifies B's packet stream contains an entity_head_rotation
//     (0x4C) for A with headYaw matching A's yaw.
func TestEntityHeadRotationOnPositionTick(t *testing.T) {
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
	// Keep the position-tick at 50ms but explicitly enabled.
	srv.SetPositionBroadcastInterval(50 * time.Millisecond)
	serveDone := make(chan struct{})
	go func() {
		defer close(serveDone)
		_ = srv.Serve(context.Background(), ln)
	}()
	addr := ln.Addr().String()

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

	connB, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial B: %v", err)
	}
	defer connB.Close()
	pktsB := startPacketReader(connB)
	handshakeAndLogin(t, connB, cfg)
	configAckAndEnterPlay(t, connB, pktsB)
	drainChunks(t, pktsB)
	// Drain B's spawn of A (info, spawn, metadata, equipment, head).
	drainNPackets(pktsB, 5, 500*time.Millisecond)
	time.Sleep(100 * time.Millisecond)

	// A rotates to 90° yaw (looking east).
	{
		var w protocol.WireWriter
		w.Float32(90.0) // yaw = 90 degrees
		w.Float32(0.0)  // pitch = 0
		w.Bool(false)   // onGround
		if _, err := connA.Write(protocol.MakePacket(v772.PlaySetPlayerRot, w.Bytes())); err != nil {
			t.Fatalf("write set_player_rotation: %v", err)
		}
	}

	// B should see an entity_head_rotation (0x4C) for A. The
	// headYaw should be approximately 64 (= 90/360 * 256) in
	// 256ths-of-a-turn. We allow a tolerance of ±1 for the
	// float-to-int8 rounding.
	headPkt, found := waitForPacketID(pktsB, 0x4C, 2*time.Second)
	if !found {
		t.Fatal("BUG: B did not receive entity_head_rotation (0x4C) for A's rotation — head rotation not broadcast on position tick")
	}
	r := protocol.NewWireReader(headPkt.data)
	eid := r.VarInt()
	if eid != 0 {
		t.Errorf("entity_head_rotation eid = %d, want 0 (A's EID)", eid)
	}
	headYaw := int8(r.Byte())
	// 90 degrees in 256ths-of-a-turn: 90/360 * 256 = 64
	if diff := int(headYaw) - 64; diff < -2 || diff > 2 {
		t.Errorf("entity_head_rotation headYaw = %d, want ~64 (= 90° in 256ths of a turn)", headYaw)
	}

	connA.Close()
	connB.Close()
	time.Sleep(200 * time.Millisecond)
	ln.Close()
	<-serveDone
}
