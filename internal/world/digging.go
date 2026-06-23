// Block digging domain logic. Symmetric to placement.go. Creative-mode digging only: client sends Status=0 (started) and assumes instant break. Survival-mode mining is Phase 5+.
// Vanilla 1.21.8 player_action.status enum: 0=started, 1=cancelled, 2=finished, 3=drop_stack, 4=drop_item, 5=swap_item.
package world

import "goore/internal/protocol"

// DigStatus is the action enum for player_action (0x28 in 1.21.8) packet. Only Started/Finished are actual digs.
const (
	DigStatusStarted   int32 = 0
	DigStatusCancelled int32 = 1
	DigStatusFinished  int32 = 2
	DigStatusDropStack int32 = 3
	DigStatusDropItem  int32 = 4
	DigStatusSwapItem  int32 = 5
)

// DigRequest is the input to TryBreak. Position is the integer block coordinate of the block being broken.
type DigRequest struct {
	Position protocol.BlockPos
}

// DigReason explains why a dig was rejected. DigOK (zero value) = success.
type DigReason int

const (
	DigOK           DigReason = iota // success: Broken=true, OldBlock set
	DigNothingThere                  // position was already air; handler still acks the sequence but skips broadcast
)

// DigResult is the output of TryBreak. OldBlock is always populated (BlockAir on DigNothingThere).
type DigResult struct {
	// Broken is true iff the world was mutated by this call.
	Broken bool
	// OldBlock is the state that was at req.Position before the
	// dig (BlockAir if the position was empty). Populated even
	// on failure so the caller can log it.
	OldBlock Block
	// Reason is DigOK on success, or DigNothingThere on failure.
	Reason DigReason
}

// TryBreak removes the block at req.Position. Mutates w iff a non-air block was there. The vanilla client sometimes sends dig packets for positions the server has already cleared (double-dig, unload) — silently no-op + ack the sequence.
func TryBreak(w *World, req DigRequest) DigResult {
	old := w.GetBlock(int(req.Position.X), int(req.Position.Y), int(req.Position.Z))
	if old == BlockAir {
		return DigResult{OldBlock: old, Reason: DigNothingThere}
	}
	w.SetBlock(int(req.Position.X), int(req.Position.Y), int(req.Position.Z), BlockAir)
	return DigResult{
		Broken:   true,
		OldBlock: old,
		Reason:   DigOK,
	}
}
