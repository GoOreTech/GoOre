// This file defines the PlayerHooks interface and a no-op default. The server implements this via serverPlayerHooks (internal/server/player_hooks.go). See docs/player.md §PlayerHooks interface.

package player

// PlayerHooks is the lifecycle + broadcast surface that the *Server (or a test mock) implements for each *Player. See docs/player.md §PlayerHooks interface.
//
// Two broadcast primitives with distinct vanilla semantics:
//   - Broadcast(pkt): send to OTHER players only (never the originator). Used for
//     entity_teleport/equipment/hurt/death/info_update — the originator gets its
//     own view via a different (self-directed) packet path.
//   - BroadcastAll(pkt): send to ALL players INCLUDING the originator. Used for
//     block_update on dig/place — the vanilla 1.19.3+ server-authoritative
//     block-prediction protocol REQUIRES the breaker/placer to receive the
//     authoritative block_update for its own changed block so the client can
//     reconcile its local prediction against the server's state when the
//     matching block_changed_ack (0x04) arrives. If the originator is skipped,
//     the client reverts its prediction and the block reappears client-side
//     even though the server mutated the world correctly (the bug:
//     "когда ломаю блок он снова появляется, но при перезаходе всё корректно").
type PlayerHooks interface {
	OnEnterPlay(p *Player)
	OnLeavePlay(p *Player)
	OnDisconnect(p *Player) error
	Broadcast(pkt []byte) error
	BroadcastAll(pkt []byte) error
}

// noOpHooks is the default PlayerHooks. Lifecycle is no-op; Broadcast and
// BroadcastAll both fall back to a self-send so standalone-mode tests (no real
// server) still see the player's own packets. BroadcastAll == Broadcast here
// because the only "player" in standalone mode is the originator itself.
type noOpHooks struct {
	p *Player
}

// Compile-time assertion: noOpHooks must satisfy PlayerHooks.
var _ PlayerHooks = (*noOpHooks)(nil)

func (h *noOpHooks) OnEnterPlay(*Player)        {}
func (h *noOpHooks) OnLeavePlay(*Player)        {}
func (h *noOpHooks) OnDisconnect(*Player) error { return nil }
func (h *noOpHooks) Broadcast(pkt []byte) error {
	if h.p == nil {
		return nil
	}
	return h.p.SendPacket(pkt) // self-send for standalone tests
}

// BroadcastAll has the same self-send fallback as Broadcast in standalone
// mode: there are no other players, so the only recipient is the originator.
// This preserves the existing single-player test contract (the breaker gets
// exactly ONE block_update). Under serverPlayerHooks it additionally reaches
// the originator, which is the whole point — see the interface doc above.
func (h *noOpHooks) BroadcastAll(pkt []byte) error {
	if h.p == nil {
		return nil
	}
	return h.p.SendPacket(pkt)
}
