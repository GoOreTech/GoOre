// This file owns the block-breaking (dig) play-state handler. Thin wrapper around world.TryBreak — see docs/CONTEXT.md §Block Digging.

package player

import (
	"log/slog"

	"goore/internal/protocol"
	"goore/internal/world"
)

// handlePlayerAction processes a player_action (0x28 in 1.21.8) packet. Status enum: 0=started, 1=cancelled, 2=finished, 3=drop_stack, 4=drop_item, 5=swap_item. Creative mode only (survival is Phase 5+).
func (p *Player) handlePlayerAction(data []byte) {
	r := protocol.NewWireReader(data)
	status := r.VarInt()
	packed := r.Int64()
	_ = r.Byte() // face
	sequence := r.VarInt()
	if r.Err() != nil {
		slog.Warn("player_action parse failed", "err", r.Err())
		return
	}

	x := int32(packed >> 38)
	z := int32(packed << 26 >> 38)
	y := int32(packed << 52 >> 52)
	pos := protocol.BlockPos{X: x, Y: y, Z: z}

	switch status {
	case world.DigStatusStarted, world.DigStatusFinished:
		res := world.TryBreak(p.World, world.DigRequest{Position: pos})
		if res.Broken {
			// BroadcastAll — NOT Broadcast: the vanilla 1.19.3+ client predicts
			// the break locally and reconciles against the server's authoritative
			// block_update (0x08) for THIS position when block_changed_ack (0x04)
			// arrives. If the breaker is skipped (Broadcast excludes originator),
			// the prediction is unconfirmed and the client reverts it → the block
			// reappears client-side even though the world was correctly mutated.
			// The ack MUST follow the block_update so the client pairs them.
			updPkt := p.Proto.WriteBlockUpdate(pos, 0)
			_ = p.hooks.BroadcastAll(updPkt)
			slog.Info("player dug block", "name", p.Name, "x", x, "y", y, "z", z, "was", res.OldBlock)
		}
		// Always ack (vanilla protocol politeness), even on DigNothingThere.
		// Sent AFTER the block_update so the client resolves the prediction with
		// the authoritative state already in hand.
		p.SendPacket(p.Proto.WriteAckPlayerDigging(sequence))
	default:
		// Status 1/3/4/5: not a dig, just ack.
		p.SendPacket(p.Proto.WriteAckPlayerDigging(sequence))
	}
}
