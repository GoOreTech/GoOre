// Package player — Phase 5 command + respawn handlers. /gamemode is parsed
// from the chat_command packet (chat tab-complete / command dispatch is Phase 7;
// we only implement the survival-relevant slash commands here). perform_respawn
// arrives as client_command action 0 when the player clicks Respawn on the
// death screen.
package player

import (
	"log/slog"
	"strings"

	"goore/internal/protocol"
	v772 "goore/internal/protocol/v772"
)

// handleChatCommand parses a chat_command (0x06) payload. Wire format:
// command (string) + timestamp(u64) + salt(i64) + argCount(varint) +
// argName×argType signatures + signedPreview(bool) + ... We only need the
// leading command string; the rest is signature noise we skip.
//
// Supported commands:
//   - /gamemode <survival|creative|adventure|spectator|s|c|a|sp|0|1|2|3>
//   - /gamemode <mode> <player>          (target another player — best-effort,
//     only exact name match in the server's player set via BroadcastFn-free
//     lookup; for now only self)
//   - /heal                              (restore self to full — debug helper)
//   - /kill                              (deal lethal generic damage to self)
func (p *Player) handleChatCommand(data []byte) {
	r := protocol.NewWireReader(data)
	cmd := r.String()
	if r.Err() != nil {
		slog.Warn("chat_command parse failed", "name", p.Name, "err", r.Err())
		return
	}
	slog.Info("chat command", "name", p.Name, "command", cmd)

	fields := strings.Fields(cmd)
	if len(fields) == 0 {
		return
	}
	switch fields[0] {
	case "/gamemode", "/gm":
		if len(fields) < 2 {
			p.sendSystemChat("usage: /gamemode <survival|creative|adventure|spectator>")
			return
		}
		gm, ok := parseGamemode(fields[1])
		if !ok {
			p.sendSystemChat("unknown gamemode: " + fields[1])
			return
		}
		// /gamemode <mode> [player] — only self is supported.
		p.SetGamemode(gm)
	case "/heal":
		p.applyHeal(maxHealth)
	case "/kill":
		p.applyDamage(maxHealth, v772.DamageTypeGenericKill, false, 0, 0, 0)
	default:
		// Unknown command — silently ignore (no command framework yet).
	}
}

// handleClientCommand parses a client_command (0x0B) payload: actionId (varint).
// Action 0 = perform_respawn (player clicked Respawn on the death screen).
// Action 1 = request_stats — not implemented.
func (p *Player) handleClientCommand(data []byte) {
	r := protocol.NewWireReader(data)
	action := r.VarInt()
	if r.Err() != nil {
		return
	}
	switch action {
	case 0:
		p.Respawn()
	case 1:
		// request_stats — no-op
	default:
		slog.Debug("client_command action", "name", p.Name, "action", action)
	}
}

// parseGamemode maps a name or numeric alias to a gamemode constant.
func parseGamemode(s string) (uint8, bool) {
	switch strings.ToLower(s) {
	case "survival", "s", "0":
		return GamemodeSurvival, true
	case "creative", "c", "1":
		return GamemodeCreative, true
	case "adventure", "a", "2":
		return GamemodeAdventure, true
	case "spectator", "sp", "3":
		return GamemodeSpectator, true
	}
	return 0, false
}

// sendSystemChat sends a system chat message to this player only.
func (p *Player) sendSystemChat(msg string) {
	_ = p.SendPacket(p.Proto.WriteSystemChat(msg))
}
