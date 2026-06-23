// Tests for the bug: "block in hand differs from block placed".
//
// Root cause: set_creative_slot (0x37) wire format uses Holder<Item>
// (registryId + 1) for the itemId, but the server stored the raw
// wire value as the internal item ID. So picking stone from the
// creative menu (wire value = 2) ended up as itemId=2 in Hotbar
// (granite's registryId), and placement looked up the wrong block.
//
// The fix: when reading itemId from the wire, subtract 1 to get the
// raw registryId (the internal representation used everywhere else,
// including world.ItemIDToBlockState and p.HeldItem).
//
// Regression tests live in this file. They exercise the full path
// that the user reported: client sends set_creative_slot with the
// vanilla wire value → server stores correct registryId → user
// right-clicks to place → correct block appears in the world.

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

// TestCreativeMenuPlaceStone_PutsStone is the direct regression for
// the user-reported symptom "held block differs from placed block".
//
// In vanilla 1.21.8 the client sends set_creative_slot with the
// itemId encoded as Holder<Item>: VarInt(registryId + 1). For stone
// (registryId=1) the wire value is 2. The server must translate it
// back to the raw registryId (1) for its internal Hotbar[]. Then
// when the user right-clicks to place, handleBlockPlace looks up
// world.ItemIDToBlockState[1] and gets stone (default state 1),
// NOT world.ItemIDToBlockState[2] which would be granite.
func TestCreativeMenuPlaceStone_PutsStone(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	w := world.New(0)
	proto := v772.New()
	cfg := config.DefaultConfig()
	cfg.ViewDist = 2
	p := player.New(42, serverConn, proto, w, cfg)
	// Move the player out of the placement target area.
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

	// Pick stone from the creative menu into hotbar slot 0.
	// Wire value: stone = Holder<Item> = registryId(1) + 1 = 2.
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

	// Drain the set_slot echo.
	pkt := readPacket(t, serverPackets, 2*time.Second)
	if pkt.id != v772.PlaySetSlot {
		t.Fatalf("expected SetSlot (0x%02X), got 0x%02X", v772.PlaySetSlot, pkt.id)
	}

	// CRITICAL: internal Hotbar[0] must be the RAW registryId (1 = stone),
	// NOT the wire value (2 = would be granite's registryId).
	if p.Hotbar[0] != 1 {
		t.Fatalf("Hotbar[0] = %d, want 1 (stone registryId). Wire value was 2 (stone = registryId+1); bug: server stored wire value as registryId → placement would use granite (registryId 2) instead of stone.", p.Hotbar[0])
	}
	if p.HeldItem != 1 {
		t.Errorf("HeldItem = %d, want 1 (stone registryId)", p.HeldItem)
	}

	// Now right-click +Y face of grass at (0, 3, 0) to place stone at (0, 4, 0).
	packed := int64(0)<<38 | int64(0)<<26 | int64(0)<<52 // x=0, y=3, z=0
	// Actually pack: x=0, z=0, y=3
	x, y, z := int32(0), int32(3), int32(0)
	packed = (int64(x)&0x3FFFFFF)<<38 | (int64(z)&0x3FFFFFF)<<12 | (int64(y) & 0xFFF)

	bp := &protocol.WireWriter{}
	bp.VarInt(0)     // hand = main
	bp.Int64(packed) // position
	bp.VarInt(1)     // face = +Y (top of grass)
	bp.Float32(0)    // cursorX
	bp.Float32(0)    // cursorY
	bp.Float32(0)    // cursorZ
	bp.Bool(false)   // insideBlock
	bp.Bool(false)   // worldBorderHit
	bp.VarInt(0)     // sequence
	if bp.Err() != nil {
		t.Fatalf("build block_place: %v", bp.Err())
	}
	if _, err := clientConn.Write(protocol.MakePacket(v772.PlayBlockPlace, bp.Bytes())); err != nil {
		t.Fatalf("write block_place: %v", err)
	}

	// Expect a block_update (0x08) at (0, 4, 0).
	blk := readPacket(t, serverPackets, 2*time.Second)
	if blk.id != v772.PlayBlockUpdate {
		t.Fatalf("expected BlockUpdate (0x%02X), got 0x%02X", v772.PlayBlockUpdate, blk.id)
	}
	r := protocol.NewWireReader(blk.data)
	upacked := r.Int64()
	gotX := int32(upacked >> 38)
	gotY := int32(upacked << 52 >> 52)
	gotZ := int32(upacked << 26 >> 38)
	if gotX != 0 || gotY != 4 || gotZ != 0 {
		t.Errorf("placed at (%d, %d, %d), want (0, 4, 0)", gotX, gotY, gotZ)
	}
	stateID := r.VarInt()
	// stone default state = 1 (stone block). If the bug is present
	// the server placed granite (registryId=2, default state=2) instead.
	if stateID != 1 {
		t.Errorf("placed stateID = %d, want 1 (stone). Bug: server placed a different block (granite=2, etc.) because it stored the wire value (registryId+1) as the raw itemId.", stateID)
	}

	// World state check.
	if got := w.GetBlock(0, 4, 0); got != 1 {
		t.Errorf("world block at (0,4,0) = %d, want 1 (stone)", got)
	}
}
