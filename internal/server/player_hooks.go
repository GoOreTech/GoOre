// This file defines serverPlayerHooks, the server's implementation of player.PlayerHooks. Each *Player gets its own instance (originEID captured per-connection). See docs/server.md §serverPlayerHooks.

package server

import (
	"log/slog"

	"goore/internal/player"
)

// serverPlayerHooks implements player.PlayerHooks for one specific connection. originEID is captured so Broadcast can exclude the originator.
type serverPlayerHooks struct {
	server    *Server
	originEID int32
}

// Compile-time assertion: serverPlayerHooks must satisfy
// player.PlayerHooks. A missing method or signature mismatch
// surfaces here at build time.
var _ player.PlayerHooks = (*serverPlayerHooks)(nil)

// OnEnterPlay is called by the *Player after the 30+ packet first frame has been sent and the FSM is in statePlay.
func (h *serverPlayerHooks) OnEnterPlay(p *player.Player) {
	h.server.SpawnPlayerForOthers(p)
	h.server.registerPositionBroadcaster(p)
}

// OnLeavePlay is called from HandleConn's defer, BEFORE OnDisconnect. The ordering matters: the despawn goes out while the world/player state is still fully consistent.
func (h *serverPlayerHooks) OnLeavePlay(p *player.Player) {
	h.server.DespawnPlayerForOthers(p)
	h.server.unregisterPositionBroadcaster(p)
}

// OnDisconnect persists the player's state. The world save runs FIRST so a partial failure (player file write error) does not leave the world unsaved. See docs/regressions.md #5.
func (h *serverPlayerHooks) OnDisconnect(p *player.Player) error {
	if err := h.server.world.SaveAll(); err != nil {
		slog.Warn("save world on disconnect failed", "name", p.Name, "err", err)
	}
	return player.SavePlayer(h.server.cfg.WorldDir, p)
}

// Broadcast sends pkt to every connected player EXCEPT the originator. Used
// for packets the originator receives via a separate self-directed path
// (entity_teleport, equipment, damage_event, ...). Best-effort: a single send
// error is captured but does not stop the others.
func (h *serverPlayerHooks) Broadcast(pkt []byte) error {
	var firstErr error
	h.server.players.Range(func(key, value any) bool {
		other := value.(*player.Player)
		if other.EID == h.originEID {
			return true
		}
		if err := other.SendPacket(pkt); err != nil && firstErr == nil {
			firstErr = err
		}
		return true
	})
	return firstErr
}

// BroadcastAll sends pkt to every connected player INCLUDING the originator.
// Required for block_update on dig/place: the vanilla 1.19.3+
// server-authoritative block-prediction protocol makes the breaker/placer
// predict the change locally and waits for the server's authoritative
// block_update (0x08) — followed by block_changed_ack (0x04) — to confirm or
// revert the prediction. Skipping the originator leaves its prediction
// unconfirmed, so the client reverts it and the block reappears client-side
// even though the server mutated the world (the "блок снова появляется, но при
// перезаходе всё корректно" bug). Best-effort: a single send error is
// captured but does not stop the others.
func (h *serverPlayerHooks) BroadcastAll(pkt []byte) error {
	var firstErr error
	h.server.players.Range(func(key, value any) bool {
		other := value.(*player.Player)
		if err := other.SendPacket(pkt); err != nil && firstErr == nil {
			firstErr = err
		}
		return true
	})
	return firstErr
}
