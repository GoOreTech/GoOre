// This file owns the per-tick position/rotation handlers. All three parse the
// wire payload and write X/Y/Z/Yaw/Pitch under posMu.Lock(); readers use Pos()
// (posMu.RLock). Phase 5: onGround is now stored and feeds fall-damage
// accumulation on landing (see survival.go).

package player

import (
	"goore/internal/protocol"
	v772 "goore/internal/protocol/v772"
)

// handlePosition processes a set_player_position (0x1D) packet. 1.21.8 wire
// format: x(f64) + y(f64) + z(f64) + onGround(bool).
func (p *Player) handlePosition(data []byte) {
	r := protocol.NewWireReader(data)
	x := r.Float64()
	y := r.Float64()
	z := r.Float64()
	onGround := r.Bool()
	p.updatePosition(x, y, z, p.Yaw, p.Pitch, onGround)
}

// handlePositionAndRotation processes a set_player_position_and_rotation (0x1E) packet.
func (p *Player) handlePositionAndRotation(data []byte) {
	r := protocol.NewWireReader(data)
	x := r.Float64()
	y := r.Float64()
	z := r.Float64()
	yaw := r.Float32()
	pitch := r.Float32()
	onGround := r.Bool()
	p.updatePosition(x, y, z, yaw, pitch, onGround)
}

// handleRotation processes a set_player_rotation (0x1F) packet. No position
// delta → no fall-damage accounting, just yaw/pitch + onGround.
func (p *Player) handleRotation(data []byte) {
	r := protocol.NewWireReader(data)
	yaw := r.Float32()
	pitch := r.Float32()
	onGround := r.Bool()
	p.posMu.Lock()
	p.Yaw, p.Pitch = yaw, pitch
	p.OnGround = onGround
	p.posMu.Unlock()
}

// updatePosition writes the new pose under posMu and accounts for fall damage.
// Fall-damage accounting only runs for players who take damage (survival /
// adventure); creative/spectator players keep FallDistance pinned at 0. The
// vanilla rules modelled here:
//   - descending while airborne → accumulate fallDistance
//   - ascending (jump / teleport up) → reset fallDistance to 0
//   - landing (onGround true) with fallDistance > 3 → apply ceil(fall-3) damage,
//     then reset fallDistance
func (p *Player) updatePosition(x, y, z float64, yaw, pitch float32, onGround bool) {
	takesDamage := p.TakesDamage()

	p.posMu.Lock()
	oldY := p.Y
	wasAirborne := !p.OnGround
	p.X, p.Y, p.Z = x, y, z
	p.Yaw, p.Pitch = yaw, pitch
	p.OnGround = onGround
	p.posMu.Unlock()

	if !takesDamage {
		// Creative/spectator: never accumulate fall damage.
		if !onGround {
			// no-op
		}
		return
	}

	// Fall-damage accounting. We mutate FallDistance under vitalsMu.
	p.vitalsMu.Lock()
	delta := oldY - y // > 0 when descending
	switch {
	case y > oldY:
		// Ascending (jump, slab step-up, teleport up): vanilla resets.
		p.FallDistance = 0
		p.vitalsMu.Unlock()
	case !onGround && delta > 0:
		// Descending in air: accumulate.
		p.FallDistance += float32(delta)
		p.vitalsMu.Unlock()
	case onGround && wasAirborne:
		// Just landed. Count the final descent segment too (the landing packet
		// itself usually reports a lower Y than the previous airborne tick),
		// then apply damage for the full accumulated fall and reset.
		if delta > 0 {
			p.FallDistance += float32(delta)
		}
		fd := p.FallDistance
		p.FallDistance = 0
		p.vitalsMu.Unlock()
		if dmg := FallDamageFor(fd); dmg > 0 {
			// Fall damage source position = where the player landed.
			p.applyDamage(dmg, v772.DamageTypeFall, true, x, y, z)
		}
		return
	default:
		p.vitalsMu.Unlock()
		return
	}
}
