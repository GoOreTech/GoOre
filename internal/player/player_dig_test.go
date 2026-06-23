package player_test

import (
	"bytes"
	"net"
	"testing"
	"time"

	"goore/internal/config"
	"goore/internal/player"
	"goore/internal/protocol"
	"goore/internal/protocol/v772"
	"goore/internal/world"
)

// TestPlayerActionDigCreative is an end-to-end test of player_action
// (0x23, status=0 "started digging") in creative mode. The vanilla server:
//
//  1. Instantly removes the targeted block (sets to air).
//  2. Sends block_update (0x08) with state ID 0 to all clients in range.
//  3. Sends acknowledge_player_digging (0x04) echoing the client's sequence.
//
// We verify all three: world state, sent block_update, sent ack.
func TestPlayerActionDigCreative(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	w := world.New(0)
	proto := v772.New()
	cfg := config.DefaultConfig()
	cfg.ViewDist = 2
	p := player.New(42, serverConn, proto, w, cfg)

	// Read goroutine — captures every packet the server sends.
	serverPackets := make(chan serverPkt, 100)
	go func() {
		buf := make([]byte, 65536)
		accum := &bytes.Buffer{}
		for {
			n, err := clientConn.Read(buf)
			if err != nil {
				return
			}
			accum.Write(buf[:n])
			for accum.Len() > 0 {
				r := protocol.NewWireReader(accum.Bytes())
				length := r.VarInt()
				if r.Err() != nil {
					return
				}
				pktLen := int(length) + protocol.VarIntSize(length)
				if accum.Len() < pktLen {
					break
				}
				pktID := r.VarInt()
				if r.Err() != nil {
					return
				}
				payload := make([]byte, int(length)-protocol.VarIntSize(pktID))
				r.Read(payload)
				serverPackets <- serverPkt{id: pktID, data: payload}
				accum.Next(pktLen)
			}
		}
	}()

	go p.HandleConn()

	// Walk through handshake → login → config → play.
	handshakeAndLogin(t, clientConn, cfg)
	configAckAndEnterPlay(t, clientConn, serverPackets)
	drainChunks(t, serverPackets)

	// World has grass at (0, 3, 0). Dig it.
	// player_action wire format: status(VarInt) + position(i64) + face(Byte) + sequence(VarInt)
	aw := &protocol.WireWriter{}
	aw.VarInt(0) // status = started digging
	aw.Int64(encodePos(0, 3, 0))
	aw.Byte(1)   // face = +Y
	aw.VarInt(7) // sequence
	if aw.Err() != nil {
		t.Fatalf("build player_action: %v", aw.Err())
	}
	if _, err := clientConn.Write(protocol.MakePacket(v772.PlayPlayerAction, aw.Bytes())); err != nil {
		t.Fatalf("write player_action: %v", err)
	}

	// Expect block_update (0x08) within 2s.
	blk := readPacket(t, serverPackets, 2*time.Second)
	if blk.id != v772.PlayBlockUpdate {
		t.Fatalf("expected BlockUpdate (0x%02X), got 0x%02X", v772.PlayBlockUpdate, blk.id)
	}
	// Decode block_update: i64(pos) + varint(stateID)
	r := protocol.NewWireReader(blk.data)
	packed := r.Int64()
	gotX := int32(packed >> 38)
	gotY := int32(packed << 52 >> 52)
	gotZ := int32(packed << 26 >> 38)
	if gotX != 0 || gotY != 3 || gotZ != 0 {
		t.Errorf("block_update pos = (%d, %d, %d), want (0, 3, 0)", gotX, gotY, gotZ)
	}
	stateID := r.VarInt()
	if stateID != 0 {
		t.Errorf("block_update stateID = %d, want 0 (air)", stateID)
	}

	// Expect acknowledge_player_digging (0x04) within 1s.
	ack := readPacket(t, serverPackets, 1*time.Second)
	if ack.id != v772.PlayAckPlayerDigging {
		t.Fatalf("expected AckPlayerDigging (0x%02X), got 0x%02X",
			v772.PlayAckPlayerDigging, ack.id)
	}
	r = protocol.NewWireReader(ack.data)
	seq := r.VarInt()
	if seq != 7 {
		t.Errorf("ack sequenceId = %d, want 7", seq)
	}

	// Verify the world state was actually mutated.
	if got := w.GetBlock(0, 3, 0); got != 0 {
		t.Errorf("world block at (0,3,0) = %d, want 0 (air)", got)
	}
}

// TestSetCreativeSlot verifies the server accepts set_creative_slot (0x37)
// from the client. Vanilla 1.21.8 wire format:
//
//	slot(i16) + item(Slot)
//
// In creative mode, the client sends this when the player picks an item
// from the creative menu into a hotbar slot. The slot field is the
// WIRE slot index (per InventoryMenu: HOTBAR_START = 36, so the
// first hotbar slot is wire slot 36, NOT 0). The server must:
//
//  1. Translate the wire slot to a hotbar index 0..8.
//  2. Update its Hotbar state at that hotbar index.
//  3. Update the HeldItem if the slot is the currently held one.
//  4. Send a set_slot (0x14) echo back at the SAME wire slot so the
//     client and server agree.
func TestSetCreativeSlot(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	w := world.New(0)
	proto := v772.New()
	cfg := config.DefaultConfig()
	cfg.ViewDist = 2
	p := player.New(42, serverConn, proto, w, cfg)
	p.X = 10
	p.Y = 4
	p.Z = 0.5

	serverPackets := make(chan serverPkt, 100)
	go func() {
		buf := make([]byte, 65536)
		accum := &bytes.Buffer{}
		for {
			n, err := clientConn.Read(buf)
			if err != nil {
				return
			}
			accum.Write(buf[:n])
			for accum.Len() > 0 {
				r := protocol.NewWireReader(accum.Bytes())
				length := r.VarInt()
				if r.Err() != nil {
					return
				}
				pktLen := int(length) + protocol.VarIntSize(length)
				if accum.Len() < pktLen {
					break
				}
				pktID := r.VarInt()
				if r.Err() != nil {
					return
				}
				payload := make([]byte, int(length)-protocol.VarIntSize(pktID))
				r.Read(payload)
				serverPackets <- serverPkt{id: pktID, data: payload}
				accum.Next(pktLen)
			}
		}
	}()

	go p.HandleConn()

	handshakeAndLogin(t, clientConn, cfg)
	configAckAndEnterPlay(t, clientConn, serverPackets)
	drainChunks(t, serverPackets)

	// Send set_creative_slot (0x37): wire slot 36 (hotbar index 0),
	// item=stone (registryId=1, count=1). Vanilla 1.21.8 sends the
	// itemId encoded as Holder<Item>: VarInt(registryId + 1) = 2.
	// The server translates back to the raw registryId for internal
	// storage (see handleSetCreativeSlot).
	uw := &protocol.WireWriter{}
	uw.Int16(36) // wire slot 36 = hotbar index 0
	uw.VarInt(1) // item count = 1 (present)
	uw.VarInt(2) // itemId = 2 (stone wire-encoded as registryId+1)
	if uw.Err() != nil {
		t.Fatalf("build set_creative_slot: %v", uw.Err())
	}
	if _, err := clientConn.Write(protocol.MakePacket(v772.PlayCreativeInventoryAction, uw.Bytes())); err != nil {
		t.Fatalf("write set_creative_slot: %v", err)
	}

	// Expect a set_slot (0x14) echo at the SAME wire slot (36) so
	// the client puts the stone in the right hotbar cell. The server
	// always replies with count=64 (creative infinite-items
	// semantics), regardless of what count the client sent.
	pkt := readPacket(t, serverPackets, 2*time.Second)
	if pkt.id != v772.PlaySetSlot {
		t.Fatalf("expected SetSlot (0x%02X), got 0x%02X", v772.PlaySetSlot, pkt.id)
	}
	r := protocol.NewWireReader(pkt.data)
	if wid := r.Byte(); wid != 0 {
		t.Errorf("windowId = %d, want 0", wid)
	}
	_ = r.VarInt() // stateId
	if slot := r.Int16(); slot != 36 {
		t.Errorf("echoed wire slot = %d, want 36 (hotbar index 0)", slot)
	}
	count := r.VarInt()
	if count != 64 {
		t.Errorf("echoed item count = %d, want 64 (creative infinite)", count)
	}
	itemID := r.VarInt()
	// 1.21.5+ Holder<Item> wire format: VarInt(registryId + 1).
	// Stone (registryId=1) is encoded as 2.
	if itemID != 2 {
		t.Errorf("echoed item id = %d, want 2 (stone wire-encoded as registryId+1)", itemID)
	}
	if r.Err() != nil {
		t.Errorf("reader error: %v", r.Err())
	}

	// Player's Hotbar[0] should now be stone (the hotbar index is
	// wire slot 36, but our internal Hotbar array is still 0..8).
	if p.Hotbar[0] != 1 {
		t.Errorf("Hotbar[0] = %d, want 1 (stone)", p.Hotbar[0])
	}
}

// TestPlaceRefillsHotbarCreative verifies that after a block placement in
// creative mode, the server re-sends the slot with count=64 so the client
// keeps the hotbar item stack topped up at 64 (infinite-items semantics).
//
// Flow:
//  1. Hold dirt in slot 0 (default from hotbar init).
//  2. Right-click +Y face of grass at (0, 3, 0) to place dirt at (0, 4, 0).
//  3. Server should: send block_update(0x08), then set_slot(0x14) with
//     count=64 (refill) for slot 0.
func TestPlaceRefillsHotbarCreative(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	w := world.New(0)
	proto := v772.New()
	cfg := config.DefaultConfig()
	cfg.ViewDist = 2
	p := player.New(42, serverConn, proto, w, cfg)
	// Move the player out of the placement target. Default spawn is
	// (0.5, 4, 0.5); we want to place a block at (0, 4, 0) which is
	// inside the player's AABB. So move the player to (10, 4, 0.5).
	p.X = 10
	p.Y = 4
	p.Z = 0.5

	serverPackets := make(chan serverPkt, 100)
	go func() {
		buf := make([]byte, 65536)
		accum := &bytes.Buffer{}
		for {
			n, err := clientConn.Read(buf)
			if err != nil {
				return
			}
			accum.Write(buf[:n])
			for accum.Len() > 0 {
				r := protocol.NewWireReader(accum.Bytes())
				length := r.VarInt()
				if r.Err() != nil {
					return
				}
				pktLen := int(length) + protocol.VarIntSize(length)
				if accum.Len() < pktLen {
					break
				}
				pktID := r.VarInt()
				if r.Err() != nil {
					return
				}
				payload := make([]byte, int(length)-protocol.VarIntSize(pktID))
				r.Read(payload)
				serverPackets <- serverPkt{id: pktID, data: payload}
				accum.Next(pktLen)
			}
		}
	}()

	go p.HandleConn()

	handshakeAndLogin(t, clientConn, cfg)
	configAckAndEnterPlay(t, clientConn, serverPackets)
	drainChunks(t, serverPackets)

	// Hold dirt (item id=28) in hotbar slot 0. This must come AFTER
	// drainChunks: the FSM's sendInventory call (which runs during
	// enterPlay) resets p.Hotbar/p.HeldItem to the default hotbar
	// (stone in slot 0). Setting it after enterPlay simulates the
	// real client picking dirt from the creative menu and the
	// server's state following that change.
	p.SetHeldItemForTest(28) // dirt
	p.HeldSlot = 0

	// Right-click +Y face of (0, 3, 0) → place at (0, 4, 0).
	uw := &protocol.WireWriter{}
	uw.VarInt(0)                 // hand
	uw.Int64(encodePos(0, 3, 0)) // clicked
	uw.VarInt(1)                 // face +Y
	uw.Float32(0.5)
	uw.Float32(0.5)
	uw.Float32(0.5)
	uw.Bool(false) // inside block
	uw.Bool(false) // worldBorderHit (1.21.8)
	uw.VarInt(0)   // sequence (1.21.8, block_update ack)
	if uw.Err() != nil {
		t.Fatalf("build block_place: %v", uw.Err())
	}
	if _, err := clientConn.Write(protocol.MakePacket(v772.PlayBlockPlace, uw.Bytes())); err != nil {
		t.Fatalf("write block_place: %v", err)
	}

	// First expect block_update (0x08).
	blk := readPacket(t, serverPackets, 2*time.Second)
	if blk.id != v772.PlayBlockUpdate {
		t.Fatalf("expected BlockUpdate (0x%02X), got 0x%02X", v772.PlayBlockUpdate, blk.id)
	}

	// Then expect set_slot (0x14) with the dirt stack topped up to 64.
	refill := readPacket(t, serverPackets, 2*time.Second)
	if refill.id != v772.PlaySetSlot {
		t.Fatalf("expected SetSlot (0x%02X) refill, got 0x%02X", v772.PlaySetSlot, refill.id)
	}
	r := protocol.NewWireReader(refill.data)
	if wid := r.Byte(); wid != 0 {
		t.Errorf("refill windowId = %d, want 0", wid)
	}
	_ = r.VarInt() // stateId
	slot := r.Int16()
	if slot != 0 {
		t.Errorf("refill slot = %d, want 0 (held slot)", slot)
	}
	count := r.VarInt()
	if count != 64 {
		t.Errorf("refill item count = %d, want 64 (creative infinite)", count)
	}
	itemID := r.VarInt()
	// 1.21.5+ Holder<Item> wire format: VarInt(registryId + 1).
	// Dirt (registryId=28) is encoded as 29.
	if itemID != 29 {
		t.Errorf("refill item id = %d, want 29 (dirt wire-encoded as registryId+1)", itemID)
	}
}

// TestUnknownPacketIgnored verifies that the server does not crash on an
// unknown packet id (e.g. a packet we haven't implemented yet). It just
// logs and continues. The connection stays open.
func TestUnknownPacketIgnored(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	w := world.New(0)
	proto := v772.New()
	cfg := config.DefaultConfig()
	cfg.ViewDist = 2
	p := player.New(42, serverConn, proto, w, cfg)

	serverPackets := make(chan serverPkt, 100)
	go func() {
		buf := make([]byte, 65536)
		accum := &bytes.Buffer{}
		for {
			n, err := clientConn.Read(buf)
			if err != nil {
				return
			}
			accum.Write(buf[:n])
			for accum.Len() > 0 {
				r := protocol.NewWireReader(accum.Bytes())
				length := r.VarInt()
				if r.Err() != nil {
					return
				}
				pktLen := int(length) + protocol.VarIntSize(length)
				if accum.Len() < pktLen {
					break
				}
				pktID := r.VarInt()
				if r.Err() != nil {
					return
				}
				payload := make([]byte, int(length)-protocol.VarIntSize(pktID))
				r.Read(payload)
				serverPackets <- serverPkt{id: pktID, data: payload}
				accum.Next(pktLen)
			}
		}
	}()

	go p.HandleConn()

	handshakeAndLogin(t, clientConn, cfg)
	configAckAndEnterPlay(t, clientConn, serverPackets)
	drainChunks(t, serverPackets)

	// Send an unknown packet id (0xFF is reserved).
	uw := &protocol.WireWriter{}
	if _, err := clientConn.Write(protocol.MakePacket(0xFF, uw.Bytes())); err != nil {
		t.Fatalf("write unknown packet: %v", err)
	}

	// Server should NOT send anything back.
	select {
	case pkt := <-serverPackets:
		t.Errorf("server sent unexpected packet 0x%02X in response to unknown packet", pkt.id)
	case <-time.After(300 * time.Millisecond):
		// success
	}
}

// TestUseItemOnPlaceBlock verifies right-click placement via use_item_on
// (0x34). The player holds a dirt item (id=28) and right-clicks the +X face
// of the block at (0, 3, 0). The server should:
//
//  1. Resolve the item (id=28, "dirt") to block default state (10).
//  2. Compute the target position (the block just to the east, i.e. (1, 3, 0)).
//  3. Place the block: world.GetBlock(1, 3, 0) becomes 10.
//  4. Send block_update (0x08) at (1, 3, 0) with state 10.
//
// Position (1, 3, 0) is chosen to be clear of the player's AABB (player
// spawns at (0.5, 4, 0.5) with width 0.6, height 1.8).
func TestUseItemOnPlaceBlock(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	w := world.New(0)
	proto := v772.New()
	cfg := config.DefaultConfig()
	cfg.ViewDist = 2
	p := player.New(42, serverConn, proto, w, cfg)

	// Move the player out of the placement target. Default spawn is
	// (0.5, 4, 0.5); we want to place a block at (0, 4, 0) which is
	// inside the player's AABB. So move the player to (10, 4, 0.5).
	p.X = 10
	p.Y = 4
	p.Z = 0.5

	serverPackets := make(chan serverPkt, 100)
	go func() {
		buf := make([]byte, 65536)
		accum := &bytes.Buffer{}
		for {
			n, err := clientConn.Read(buf)
			if err != nil {
				return
			}
			accum.Write(buf[:n])
			for accum.Len() > 0 {
				r := protocol.NewWireReader(accum.Bytes())
				length := r.VarInt()
				if r.Err() != nil {
					return
				}
				pktLen := int(length) + protocol.VarIntSize(length)
				if accum.Len() < pktLen {
					break
				}
				pktID := r.VarInt()
				if r.Err() != nil {
					return
				}
				payload := make([]byte, int(length)-protocol.VarIntSize(pktID))
				r.Read(payload)
				serverPackets <- serverPkt{id: pktID, data: payload}
				accum.Next(pktLen)
			}
		}
	}()

	go p.HandleConn()

	handshakeAndLogin(t, clientConn, cfg)
	configAckAndEnterPlay(t, clientConn, serverPackets)
	drainChunks(t, serverPackets)

	// Hold dirt (item id=28) in hotbar slot 0. Must come after
	// drainChunks because sendInventory (called from enterPlay)
	// resets p.Hotbar/p.HeldItem to the default hotbar.
	p.SetHeldItemForTest(28) // dirt

	// Right-click +Y face of (0, 3, 0) → should place at (0, 4, 0).
	// (0, 4, 0) is air in the flat world. Player is at (10, 4, 0.5),
	// so no AABB intersection.
	uw := &protocol.WireWriter{}
	uw.VarInt(0)                 // hand = main
	uw.Int64(encodePos(0, 3, 0)) // clicked block
	uw.VarInt(1)                 // face = +Y
	uw.Float32(0.5)              // cursor X
	uw.Float32(0.5)              // cursor Y
	uw.Float32(0.5)              // cursor Z
	uw.Bool(false)               // inside block
	uw.Bool(false)               // worldBorderHit (1.21.8)
	uw.VarInt(0)                 // sequence (1.21.8, block_update ack)
	if uw.Err() != nil {
		t.Fatalf("build block_place: %v", uw.Err())
	}
	if _, err := clientConn.Write(protocol.MakePacket(v772.PlayBlockPlace, uw.Bytes())); err != nil {
		t.Fatalf("write block_place: %v", err)
	}

	// Expect block_update (0x08) at (0, 4, 0) with state 10 (dirt default).
	blk := readPacket(t, serverPackets, 2*time.Second)
	if blk.id != v772.PlayBlockUpdate {
		t.Fatalf("expected BlockUpdate (0x%02X), got 0x%02X", v772.PlayBlockUpdate, blk.id)
	}
	r := protocol.NewWireReader(blk.data)
	packed := r.Int64()
	gotX := int32(packed >> 38)
	gotY := int32(packed << 52 >> 52)
	gotZ := int32(packed << 26 >> 38)
	if gotX != 0 || gotY != 4 || gotZ != 0 {
		t.Errorf("placed at (%d, %d, %d), want (0, 4, 0)", gotX, gotY, gotZ)
	}
	stateID := r.VarInt()
	if stateID != 10 {
		t.Errorf("placed stateID = %d, want 10 (dirt)", stateID)
	}

	// World state check.
	if got := w.GetBlock(0, 4, 0); got != 10 {
		t.Errorf("world block at (0,4,0) = %d, want 10 (dirt)", got)
	}
}
