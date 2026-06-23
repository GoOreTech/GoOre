// This file owns the block-placement play-state handler. Thin wrapper around world.TryPlace — see docs/CONTEXT.md §Block Placement.

package player

import (
	"log/slog"

	"goore/internal/protocol"
	"goore/internal/world"
)

// handleBlockPlace processes a block_place (0x3F) packet — right-click on a block face. See docs/protocol.md §Use Item On and docs/regressions.md #8.
func (p *Player) handleBlockPlace(data []byte) {
	r := protocol.NewWireReader(data)
	_ = r.VarInt() // hand (0=main, 1=off)
	packed := r.Int64()
	face := r.VarInt()
	_ = r.Float32() // cursorX
	_ = r.Float32() // cursorY
	_ = r.Float32() // cursorZ
	_ = r.Bool()    // insideBlock
	_ = r.Bool()    // worldBorderHit (1.21.8: informational)
	_ = r.VarInt()  // sequence (1.21.8: for block_update ack)
	if r.Err() != nil {
		slog.Warn("block_place parse failed", "err", r.Err())
		return
	}

	x := int32(packed >> 38)
	z := int32(packed << 26 >> 38)
	y := int32(packed << 52 >> 52)

	px, py, pz, _, _, _ := p.Pos()
	p.hotbarMu.RLock()
	heldItem := p.HeldItem
	heldSlot := p.HeldSlot
	p.hotbarMu.RUnlock()

	res := world.TryPlace(p.World, world.PlaceRequest{
		Clicked:  protocol.BlockPos{X: x, Y: y, Z: z},
		Face:     face,
		HeldItem: heldItem,
		PlayerX:  px, PlayerY: py, PlayerZ: pz,
	})
	if !res.Placed {
		// Silent no-op; vanilla does the same.
		return
	}

	// BroadcastAll — NOT Broadcast: the placer predicts the placement locally
	// and reconciles against the server's authoritative block_update (0x08) for
	// the target position. Skipping the placer leaves its prediction unconfirmed
	// and the client reverts it → the block vanishes client-side despite the
	// server placing it. Symmetric to the dig path in digging.go.
	updPkt := p.Proto.WriteBlockUpdate(res.Target, int32(res.StateID))
	_ = p.hooks.BroadcastAll(updPkt)

	// Creative mode: refill slot to 64 (infinite-items).
	if heldSlot >= 0 && heldSlot < 9 {
		p.SendPacket(p.Proto.WriteSetSlot(0, 0, int16(heldSlot),
			protocol.Slot{Present: true, ItemID: heldItem, Count: 64}))
	}

	slog.Info("player placed block", "name", p.Name, "x", res.Target.X, "y", res.Target.Y, "z", res.Target.Z,
		"item_id", heldItem, "state_id", res.StateID)
}
