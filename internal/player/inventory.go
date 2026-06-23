// This file owns the player's hotbar / inventory wire conversions and the two play-state handlers that mutate them. See docs/player.md §Hotbar / wire-slot conversion.

package player

import (
	"log/slog"

	"goore/internal/protocol"
)

// HotbarWireStart is the wire slot index of the first hotbar slot in 1.21.8 (vanilla InventoryMenu: HOTBAR_START = 36).
const (
	hotbarWireStart = 36
	hotbarWireEnd   = 44 // inclusive
)

// HotbarWire converts a hotbar index (0..8) to its wire slot (36..44) in 1.21.8.
func HotbarWire(i int) int16 {
	return int16(i) + hotbarWireStart
}

// HotbarFromWire converts a wire slot back to a hotbar index. Returns (index, true) for valid hotbar slots, (-1, false) otherwise.
func HotbarFromWire(slot int16) (int, bool) {
	if slot < hotbarWireStart || slot > hotbarWireEnd {
		return -1, false
	}
	return int(slot - hotbarWireStart), true
}

// handleHeldItemSlot processes a held_item_slot (0x34) packet. Updates HeldSlot + HeldItem, then broadcasts entity_equipment to all other players.
func (p *Player) handleHeldItemSlot(data []byte) {
	r := protocol.NewWireReader(data)
	slot := r.Int16()
	if slot < 0 || int(slot) >= 9 {
		slot = 0
	}
	p.hotbarMu.Lock()
	p.HeldSlot = int(slot)
	p.HeldItem = p.Hotbar[slot]
	heldItem := p.HeldItem
	p.hotbarMu.Unlock()

	// Broadcast new main-hand item to all OTHER players.
	equipItem := protocol.Slot{}
	if heldItem != 0 {
		equipItem = protocol.Slot{Present: true, ItemID: heldItem, Count: 1}
	}
	_ = p.hooks.Broadcast(p.Proto.WriteEntityEquipment(p.EID, 0, equipItem))
}

// handleSetCreativeSlot processes a set_creative_slot (0x37) packet.
// The `slot` field is the WIRE slot index (HOTBAR_START = 36). See docs/regressions.md #11.
func (p *Player) handleSetCreativeSlot(data []byte) {
	r := protocol.NewWireReader(data)
	wireSlot := r.Int16()
	count := r.VarInt()
	var itemID int32
	if count > 0 {
		itemID = r.VarInt()
	}
	if r.Err() != nil {
		slog.Warn("set_creative_slot parse failed", "err", r.Err())
		return
	}

	hotbarIdx, ok := HotbarFromWire(wireSlot)
	if !ok {
		// Out of hotbar range; not modeled in Phase 2.
		return
	}

	// Holder<Item>: VarInt(registryId + 1). Subtract 1 for our internal raw registryId.
	if count > 0 && itemID > 0 {
		itemID = itemID - 1
	}

	p.hotbarMu.Lock()
	if count > 0 {
		p.Hotbar[hotbarIdx] = itemID
		p.HotbarCount[hotbarIdx] = 64 // creative: infinite
	} else {
		p.Hotbar[hotbarIdx] = 0
		p.HotbarCount[hotbarIdx] = 0
	}
	if hotbarIdx == p.HeldSlot {
		if count > 0 {
			p.HeldItem = itemID
		} else {
			p.HeldItem = 0
		}
	}
	p.hotbarMu.Unlock()

	// Echo back via set_slot (count=64 for creative infinite-items).
	p.SendPacket(p.Proto.WriteSetSlot(0, 0, wireSlot,
		protocol.Slot{Present: count > 0, ItemID: itemID, Count: 64}))

	slog.Info("player set creative slot", "name", p.Name,
		"wire_slot", wireSlot, "hotbar_idx", hotbarIdx,
		"item_id", itemID, "count", count)
}
