package player_test

import (
	"net"
	"context"
	"sort"
	"testing"
	"time"

	"goore/internal/config"
	"goore/internal/protocol"
	"goore/internal/protocol/v772"
	"goore/internal/server"
)

// TestHotbarPrePopulatedOnEnterPlay is the regression test for the
// user-reported "inventory empty / blocks in armor slots / always
// stone on placement" bug cluster.
//
// Vanilla 1.21.8 player inventory wire layout (per the protocol
// wiki and InventoryMenu constants):
//
//	0       crafting result
//	1..4    2x2 crafting input
//	5..8    armor (helmet, chest, legs, boots)
//	9..35   main inventory
//	36..44  hotbar   ← we need to write items HERE
//	45      offhand
//
// The previous code wrote hotbar items to wire slots 0..8 (which
// the client interprets as crafting + armor), so the user saw the
// items in the armor slots and the hotbar rendered empty. The
// "always stone on placement" symptom followed because the client,
// with an empty hotbar, never sent a useful held_item_slot.
//
// The fix: sendInventory must write the hotbar items at wire slots
// 36..44. It also belt-and-suspenders sends a set_slot (0x14) for
// each hotbar item at the SAME wire slot, in case the 1.21.8 client
// drops hotbar updates from container_set_content.
//
// This test verifies that after enterPlay the server's stream
// contains a set_slot (0x14) packet at wire slot 36 (the first
// hotbar slot) with itemID=1 (stone). It also verifies that the
// complete hotbar is published (TestHotbarFullCoverage covers the
// other 7 slots).
func TestHotbarPrePopulatedOnEnterPlay(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.WorldDir = dir
	cfg.ViewDist = 1
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

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	pkts := startPacketReader(conn)
	handshakeAndLogin(t, conn, cfg)
	configAckAndEnterPlay(t, conn, pkts)
	// Don't drain chunks — we want to inspect the set_slot packets
	// that come AFTER chunks but BEFORE held_item_slot.

	// Look for set_slot (0x14) packets in the spawn sequence. We
	// need to find the one for the FIRST HOTBAR slot (wire slot
	// 36 in 1.21.8) with itemID=1 (stone). Wire format:
	// windowId(VarInt) + stateId(VarInt) + slot(i16) +
	// item(Slot where count=VarInt and itemID=VarInt).
	//
	// IMPORTANT: readPacket consumes packets, so we must do this
	// check before the held_item_slot check.
	const hotbarWireStart = 36 // 1.21.8: hotbar is at wire slots 36..44
	deadline := time.Now().Add(5 * time.Second)
	foundStone := false
	for time.Now().Before(deadline) && !foundStone {
		pkt := readPacket(t, pkts, 500*time.Millisecond)
		if pkt.data == nil {
			break
		}
		if pkt.id == 0x14 {
			// Decode set_slot payload.
			r := protocol.NewWireReader(pkt.data)
			_ = r.VarInt() // windowId
			_ = r.VarInt() // stateId
			slot := r.Int16()
			// Slot wire format: count(VarInt), then if > 0: itemID(VarInt) + ...
			count := r.VarInt()
			if count > 0 {
				itemID := r.VarInt()
				// 1.21.5+ Holder<Item> wire format: VarInt(registryId + 1).
				// Stone (registryId=1) is wire-encoded as 2.
				if slot == hotbarWireStart && itemID == 2 {
					foundStone = true
				}
			}
		}
		if pkt.id == v772.PlayHeldItemSlot {
			break
		}
	}
	if !foundStone {
		t.Fatalf("BUG: server did not send set_slot (0x14) for hotbar wire slot %d (first hotbar slot) with itemID=1 (stone) — hotbar is empty on the client", hotbarWireStart)
	}

	// Verify server's internal state too (white-box check).
	// We use FindPlayerByName to look up the player via the server.
	// For now, this is a black-box check; the server's p.Hotbar[0]
	// is set to 1 by sendInventory.
	conn.Close()
	time.Sleep(100 * time.Millisecond)
	ln.Close()
	<-serveDone
}

// expectedHotbar is the canonical 1.21.8 set of hotbar items the
// server pre-populates on join. Wire slot = hotbar index + 36
// (1.21.8 InventoryMenu: HOTBAR_START = 36).
var expectedHotbar = []struct {
	wireSlot int16
	itemID   int32
}{
	{36, 1},   // stone
	{37, 27},  // grass_block
	{38, 28},  // dirt
	{39, 35},  // cobblestone
	{40, 36},  // oak_planks
	{41, 58},  // bedrock
	{42, 59},  // sand
	{43, 195}, // glass
	// slot 44 (hotbar index 8) intentionally empty
}

// TestHotbarFullCoverage is the strict regression test for the
// "blocks in armor slots" bug. It scans the full post-LoginPlay
// stream and verifies that ALL hotbar items appear via set_slot
// (0x14) at wire slots 36..44 (1.21.8 InventoryMenu.HOTBAR_START =
// 36), and that NO set_slot packet targets a wire slot in the
// crafting / armor region (0..8) for one of our hotbar item IDs.
//
// If the server regresses to writing hotbar items at slots 0..8
// the user sees "blocks in armor slots" because the client renders
// wire slots 5..8 as the four armor slots. This test would catch
// that with a precise "no set_slot in 0..8 with our hotbar itemID"
// assertion.
func TestHotbarFullCoverage(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.WorldDir = dir
	cfg.ViewDist = 1
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

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	pkts := startPacketReader(conn)
	handshakeAndLogin(t, conn, cfg)
	configAckAndEnterPlay(t, conn, pkts)

	// Track every set_slot packet we see in the spawn sequence.
	// Map: wireSlot -> itemID (last write wins).
	type setSlotEntry struct {
		wireSlot int16
		itemID   int32
	}
	gotSlots := make(map[int16]int32)
	hotbarItemSet := make(map[int32]bool)
	for _, h := range expectedHotbar {
		hotbarItemSet[h.itemID] = true
	}

	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		pkt := readPacket(t, pkts, 500*time.Millisecond)
		if pkt.data == nil {
			break
		}
		if pkt.id == v772.PlayHeldItemSlot {
			break
		}
		if pkt.id != 0x14 {
			continue
		}
		r := protocol.NewWireReader(pkt.data)
		_ = r.VarInt() // windowId
		_ = r.VarInt() // stateId
		slot := r.Int16()
		count := r.VarInt()
		if count <= 0 {
			gotSlots[slot] = 0
			continue
		}
		itemID := r.VarInt()
		// 1.21.5+ Holder<Item> wire format: VarInt(registryId + 1).
		// Subtract 1 to compare against the raw registryId that the
		// test author wrote down (e.g. stone=1, dirt=28).
		gotSlots[slot] = itemID - 1
	}

	// Assertion 1: every expected hotbar item is present at its
	// wire slot. Missing items render as empty slots in the hotbar.
	missing := []int16{}
	for _, h := range expectedHotbar {
		if gotSlots[h.wireSlot] != h.itemID {
			missing = append(missing, h.wireSlot)
		}
	}
	if len(missing) > 0 {
		sort.Slice(missing, func(i, j int) bool { return missing[i] < missing[j] })
		t.Fatalf("hotbar items missing at wire slots %v — server wrote them somewhere else (likely 0..8 = armor/crafting). Got: %v", missing, gotSlots)
	}

	// Assertion 2: NO set_slot packet put one of our hotbar item
	// IDs at a wire slot in the crafting/armor region (0..8). This
	// is the direct user-symptom check: "blocks in armor slots".
	for slot, itemID := range gotSlots {
		if slot >= 0 && slot <= 8 && hotbarItemSet[itemID] {
			t.Errorf("hotbar item %d was written to wire slot %d (crafting/armor region) instead of the hotbar — this is the user-reported 'blocks in armor slots' bug", itemID, slot)
		}
	}

	conn.Close()
	time.Sleep(100 * time.Millisecond)
	ln.Close()
	<-serveDone
}
