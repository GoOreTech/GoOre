// This file is in package player_test (external) and provides
// PlayerHooks implementations that test files in this package can
// install on a *Player. The mirror of this file for package
// player tests is persist_test.go (testSaveHooks).
//
// Phase 2.2: the four pre-Phase-2.2 callback fields on *Player
// (OnDisconnect / OnEnterPlay / OnLeavePlay / BroadcastFn) are
// gone, replaced by a single PlayerHooks interface. Tests that
// need to drive lifecycle / broadcast behavior install a small
// struct that implements the interface — usually we only care
// about one of the four methods, so the others are no-ops.
package player_test

import (
	"goore/internal/player"
)

// saveOnlyHooks implements player.PlayerHooks for the disconnect
// persistence tests. It only honors OnDisconnect (writes the
// player file via player.SavePlayer); the other three methods are
// no-ops. The saveDir is captured at construction so the test
// doesn't have to thread it through the closure.
type saveOnlyHooks struct {
	saveDir string
}

// Compile-time assertion: saveOnlyHooks must satisfy PlayerHooks.
var _ player.PlayerHooks = (*saveOnlyHooks)(nil)

func (*saveOnlyHooks) OnEnterPlay(*player.Player) {}
func (*saveOnlyHooks) OnLeavePlay(*player.Player) {}
func (*saveOnlyHooks) Broadcast([]byte) error     { return nil }
func (*saveOnlyHooks) BroadcastAll([]byte) error  { return nil }
func (h *saveOnlyHooks) OnDisconnect(p *player.Player) error {
	return player.SavePlayer(h.saveDir, p)
}
