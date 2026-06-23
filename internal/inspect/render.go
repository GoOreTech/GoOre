// Package inspect — rendering helpers. Per-field rendering for the TUI. See docs/inspect.md.
package inspect

import (
	"fmt"
	"strings"

	"goore/internal/world"
)

// playerListItem implements list.Item for a PlayerInfo.
type playerListItem struct {
	info PlayerInfo
}

// FilterValue is required by bubbles/list.Item (filtering is disabled, but the interface still needs it).
func (p playerListItem) FilterValue() string { return p.info.Name }

func (p playerListItem) Title() string { return p.info.Name }

func (p playerListItem) Description() string {
	return fmt.Sprintf("(%.0f, %.0f, %.0f) held: %s",
		p.info.X, p.info.Y, p.info.Z,
		blockName(p.info.Hotbar[p.info.HeldSlot]))
}

// blockName returns a human-readable block name for a block state ID. Falls back to "id=N" if not in the registry.
func blockName(id int32) string {
	if id == 0 {
		return "air"
	}
	if _, ok := world.ItemIDToBlockState[id]; ok {
		return stateIDToName(id)
	}
	return fmt.Sprintf("id=%d", id)
}

// stateIDToName does a linear scan over world.DefaultStateByName. O(n) but n≈1100 and called ≤9 times per player.
func stateIDToName(state int32) string {
	for name, s := range world.DefaultStateByName {
		if int32(s) == state {
			return name
		}
	}
	return fmt.Sprintf("id=%d", state)
}

// uuidString formats a UUID as 8-4-4-4-12 hex groups.
func uuidString(uuid [16]byte) string {
	parts := make([]string, 5)
	parts[0] = hexN(uuid[0:4], "")   // 8
	parts[1] = hexN(uuid[4:6], "")   // 4
	parts[2] = hexN(uuid[6:8], "")   // 4
	parts[3] = hexN(uuid[8:10], "")  // 4
	parts[4] = hexN(uuid[10:16], "") // 12
	return strings.Join(parts, "-")
}

// hexN formats a byte slice as lowercase hex without separators.
func hexN(b []byte, sep string) string {
	const hexchars = "0123456789abcdef"
	out := make([]string, len(b))
	for i, by := range b {
		out[i] = string([]byte{hexchars[by>>4], hexchars[by&0x0F]})
	}
	return strings.Join(out, sep)
}

// renderPlayer builds the multi-line text shown in the right pane for a single player.
func renderPlayer(p PlayerInfo) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Player: %s\n", p.Name))
	b.WriteString(fmt.Sprintf("UUID:   %s\n", uuidString(p.UUID)))
	b.WriteString(fmt.Sprintf("Pos:    (%.1f, %.1f, %.1f)\n", p.X, p.Y, p.Z))
	b.WriteString(fmt.Sprintf("Yaw:    %.1f°    Pitch: %.1f°\n", p.Yaw, p.Pitch))
	b.WriteString(fmt.Sprintf("OnGr:   %s\n", boolStr(p.OnGround)))
	b.WriteString(fmt.Sprintf("Held:   slot %d → %d (%s)\n", p.HeldSlot, p.Hotbar[p.HeldSlot], blockName(p.Hotbar[p.HeldSlot])))
	b.WriteString("\n")
	b.WriteString("Hotbar:\n")
	for i := 0; i < 9; i++ {
		id := p.Hotbar[i]
		marker := "  "
		if i == p.HeldSlot {
			marker = "▶ "
		}
		b.WriteString(fmt.Sprintf("%sSlot %d: %d (%s)\n", marker, i, id, blockName(id)))
	}
	return b.String()
}

// boolStr returns "true" or "false" for a bool.
func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
