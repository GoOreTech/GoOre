// Package player — Phase 5 survival mechanics. The pure math (fall damage,
// food/exhaustion/regen/starvation, eating) lives in pure functions below and
// is unit-tested without a connection. The Player methods applyDamage / tick /
// eat / setGamemode / respawn wrap the math and add the wire side-effects
// (DamageEvent, HurtAnimation, UpdateHealth, EntityStatus, Respawn, ...).
//
// Concurrency: the survival tick goroutine (started in sendPostSpawn, stopped
// by p.disconnect) and the movement handler both touch vitals. All read-modify-
// writes go through vitalsMu; readers use Vitals(). See docs/player.md §Vitals.
package player

import (
	"log/slog"
	"math"
	"time"

	"goore/internal/protocol"
	v772 "goore/internal/protocol/v772"
	"goore/internal/world"
)

// minY is the overworld minimum build height. A player whose feet drop below
// this takes void (out_of_world) damage. Vanilla overworld: -64.
const minY float64 = -64

// eatDurationTicks is how long a player must hold right-click to eat an item.
// Vanilla: 1.6s = 32 ticks at 20 Hz.
const eatDurationTicks int32 = 32

// regenIntervalTicks / starveIntervalTicks are the vanilla 4-second (80-tick)
// cadences for natural regeneration and starvation damage respectively.
const (
	regenIntervalTicks  int32 = 80
	starveIntervalTicks int32 = 80
)

// maxHealth is the player's max HP (10 hearts = 20 half-hearts).
const maxHealth float32 = 20

// FallDamageFor returns the fall damage for an accumulated fall distance,
// matching vanilla: ceil(fallDistance - 3). Returns 0 for falls of 3 blocks
// or fewer (the safe fall height). The first 3 blocks are free.
func FallDamageFor(fallDistance float32) float32 {
	if fallDistance <= 3 {
		return 0
	}
	return float32(math.Ceil(float64(fallDistance) - 3))
}

// TickFoodState advances the food/exhaustion/regen/starvation simulation by
// one 20-Hz tick. It mutates v in place and returns the heal amount and the
// starvation damage to apply this tick (both 0 most ticks). regenEnabled
// should be false for creative/spectator players.
//
// Vanilla rules modelled (simplified, no sprint/jump exhaustion — that is
// added by addExhaustion from the movement handler):
//
//   - If Exhaustion >= 4: drain 4 exhaustion; reduce Saturation by 1, or Food
//     by 1 if Saturation is already 0. (Food/Saturation clamp at 0.)
//   - Regen: Food >= 18 and Health < 20 → FoodTick++; every 80 ticks heal 1 HP
//     and add 3 exhaustion (the cost of regen).
//   - Starvation: Food == 0 → FoodTick++; every 80 ticks deal 1 damage.
//     On GoOre starvation can kill (hard-difficulty behaviour) to make "starve"
//     a meaningful damage source per the Phase 5 scope.
//   - Otherwise FoodTick resets to 0.
func TickFoodState(v *Vitals, regenEnabled bool, currentHealth float32) (heal, starve float32) {
	// 1) Exhaustion drain.
	if v.Exhaustion >= 4 {
		v.Exhaustion -= 4
		if v.Saturation > 0 {
			v.Saturation--
			if v.Saturation < 0 {
				v.Saturation = 0
			}
		} else if v.Food > 0 {
			v.Food--
		}
	}

	regen := regenEnabled && currentHealth < maxHealth
	switch {
	case v.Food >= 18 && regen:
		v.FoodTick++
		if v.FoodTick >= regenIntervalTicks {
			v.FoodTick = 0
			heal = 1
			v.Exhaustion += 3 // regen is exhausting
		}
	case v.Food <= 0:
		v.FoodTick++
		if v.FoodTick >= starveIntervalTicks {
			v.FoodTick = 0
			starve = 1
		}
	default:
		v.FoodTick = 0
	}
	return heal, starve
}

// ApplyEat returns the vitals after consuming one FoodInfo. Vanilla: Food
// increases by foodPoints (clamped to 20); Saturation increases by the food's
// saturation but is clamped to the new Food level (you can't bank more
// saturation than you have food).
func ApplyEat(v *Vitals, info world.FoodInfo) {
	v.Food += info.FoodPoints
	if v.Food > 20 {
		v.Food = 20
	}
	v.Saturation += info.Saturation
	if v.Saturation > float32(v.Food) {
		v.Saturation = float32(v.Food)
	}
}

// addExhaustion adds exhaustion from an action (sprint, jump, attack, ...).
// Clamped at 0 from below (negative exhaustion is not a thing).
func (p *Player) addExhaustion(amount float32) {
	if amount <= 0 {
		return
	}
	p.vitalsMu.Lock()
	p.Exhaustion += amount
	p.vitalsMu.Unlock()
}

// sendHealth sends an Update Health packet with the current vitals snapshot.
// Caller must NOT hold vitalsMu.
func (p *Player) sendHealth() {
	v := p.Vitals()
	pkt := p.Proto.WriteHealth(v.Health, v.Food, v.Saturation)
	if err := p.SendPacket(pkt); err != nil {
		slog.Warn("send health failed", "name", p.Name, "err", err)
	}
}

// applyDamage reduces the player's HP by amount, sends DamageEvent + HurtAnimation
// to the player, broadcasts the hurt animation to others, and transitions to the
// Dead state if HP drops to 0. No-op for players who don't take damage (creative,
// spectator, already dead). sourceType is a v772.DamageType* constant.
func (p *Player) applyDamage(amount float32, sourceType int32,
	hasSourcePos bool, sx, sy, sz float64) {
	if amount <= 0 || !p.TakesDamage() {
		return
	}

	var died bool
	var newHealth float32
	p.vitalsMu.Lock()
	if p.Dead {
		p.vitalsMu.Unlock()
		return
	}
	p.Health -= amount
	if p.Health <= 0 {
		p.Health = 0
		p.Dead = true
		p.FallDistance = 0
		died = true
	}
	newHealth = p.Health
	p.vitalsMu.Unlock()

	// DamageEvent is per-player (only the damaged client consumes it for the
	// hurt sound + death screen); HurtAnimation is broadcast so nearby players
	// see the red flash + tilt.
	pkt := p.Proto.WriteDamageEvent(p.EID, sourceType, -1, -1, hasSourcePos, sx, sy, sz)
	_ = p.SendPacket(pkt)

	hurt := p.Proto.WriteHurtAnimation(p.EID, currentYaw(p))
	_ = p.SendPacket(hurt)
	_ = p.hooks.Broadcast(hurt)

	if newHealth < maxHealth {
		p.sendHealth()
	}

	if died {
		p.die()
	}
}

// die transitions the player to the dead state on the wire: Health is already 0
// (set by applyDamage); we send UpdateHealth(0,...) so the client shows the death
// screen, then broadcast EntityStatus op 3 so other players see the death tilt.
func (p *Player) die() {
	p.sendHealth()
	// Other players see the body play the death animation. The dying player
	// themselves gets the death screen from UpdateHealth(0), not the tilt.
	death := p.Proto.WriteEntityStatus(p.EID, v772.EntityStatusPlayDeathSound)
	_ = p.hooks.Broadcast(death)
	slog.Info("player died", "name", p.Name, "eid", p.EID)
}

// cancelEating stops any in-progress eat, called when the player takes damage,
// switches hotbar slot, or moves too far. Safe to call when not eating.
func (p *Player) cancelEating() {
	p.vitalsMu.Lock()
	p.Eating = false
	p.EatItem = 0
	p.EatTicks = 0
	p.vitalsMu.Unlock()
}

// currentYaw returns the player's body yaw under posMu.RLock.
func currentYaw(p *Player) float32 {
	p.posMu.RLock()
	defer p.posMu.RUnlock()
	return p.Yaw
}

// startSurvivalTick launches the 20-Hz survival tick goroutine. Exits when
// p.disconnect is closed (HandleConn's defer). Handles void damage, the
// food/exhaustion/regen/starvation simulation, and eat completion. No-op for
// creative/spectator players except for eating (which still works in creative
// but doesn't consume the stack).
func (p *Player) startSurvivalTick() {
	go p.survivalTick()
}

func (p *Player) survivalTick() {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-p.disconnect:
			return
		case <-ticker.C:
		}
		p.tickOnce()
	}
}

// tickOnce is one 20-Hz survival tick. Extracted so tests can drive it directly
// without waiting on the 50ms ticker.
func (p *Player) tickOnce() {
	if p.State() != statePlay {
		return
	}
	v := p.Vitals()
	if v.Dead {
		return
	}

	// Void damage: feet below the world floor.
	if py := p.posY(); py < minY {
		px, pz := p.posXZ()
		p.applyDamage(4, v772.DamageTypeOutOfWorld, true, px, py, pz)
		// applyDamage may have killed the player; re-check.
		if p.Vitals().Dead {
			return
		}
	}

	regenEnabled := v.Gamemode == GamemodeSurvival || v.Gamemode == GamemodeAdventure

	var (
		heal    float32
		starve  float32
		eatDone bool
		eatItem int32
	)
	p.vitalsMu.Lock()
	vv := Vitals{
		Health: p.Health, Food: p.Food, Saturation: p.Saturation,
		Exhaustion: p.Exhaustion, FoodTick: p.FoodTick, Gamemode: p.Gamemode,
	}
	heal, starve = TickFoodState(&vv, regenEnabled, p.Health)
	p.Health = vv.Health
	p.Food = vv.Food
	p.Saturation = vv.Saturation
	p.Exhaustion = vv.Exhaustion
	p.FoodTick = vv.FoodTick

	// Eating progress.
	if p.Eating {
		p.EatTicks++
		if p.EatTicks >= eatDurationTicks {
			eatItem = p.EatItem
			eatDone = true
			p.Eating = false
			p.EatTicks = 0
			p.EatItem = 0
		}
	}
	p.vitalsMu.Unlock()

	if eatDone {
		p.completeEat(eatItem)
	}
	if heal > 0 {
		// applyHeal clamps and sends UpdateHealth.
		p.applyHeal(heal)
	}
	if starve > 0 {
		p.applyDamage(starve, v772.DamageTypeStarve, false, 0, 0, 0)
	}
}

// applyHeal increases HP (clamped to maxHealth) and sends UpdateHealth.
func (p *Player) applyHeal(amount float32) {
	if amount <= 0 {
		return
	}
	p.vitalsMu.Lock()
	p.Health += amount
	if p.Health > maxHealth {
		p.Health = maxHealth
	}
	changed := p.Health < maxHealth || amount > 0
	p.vitalsMu.Unlock()
	if changed {
		p.sendHealth()
	}
}

// completeEat applies the food values of eatItem and consumes one item from the
// held slot (survival only; creative keeps the stack infinite). Sends UpdateHealth
// and a set_slot to keep the client's hotbar in sync. Called outside vitalsMu.
func (p *Player) completeEat(eatItem int32) {
	info, ok := world.FoodForItem(eatItem)
	if !ok {
		return
	}
	p.vitalsMu.Lock()
	vv := Vitals{Food: p.Food, Saturation: p.Saturation}
	ApplyEat(&vv, info)
	p.Food = vv.Food
	p.Saturation = vv.Saturation
	p.vitalsMu.Unlock()

	// Consume one item from the held slot unless creative.
	if !p.IsCreative() {
		p.hotbarMu.Lock()
		slot := p.HeldSlot
		if p.HotbarCount[slot] > 0 {
			p.HotbarCount[slot]--
		}
		if p.HotbarCount[slot] == 0 {
			p.Hotbar[slot] = 0
			if slot == p.HeldSlot {
				p.HeldItem = 0
			}
		}
		newCount := p.HotbarCount[slot]
		newItem := p.Hotbar[slot]
		p.hotbarMu.Unlock()

		wireSlot := HotbarWire(slot)
		var s protocol.Slot
		if newItem != 0 {
			s = protocol.Slot{Present: true, ItemID: newItem, Count: newCount}
		}
		_ = p.SendPacket(p.Proto.WriteSetSlot(0, 0, wireSlot, s))
	}

	p.sendHealth()
	slog.Info("player ate", "name", p.Name, "item", eatItem,
		"food", info.FoodPoints, "saturation", info.Saturation)
}

// beginEating starts an eat of the currently-held item if it is food. Called
// from the use_item handler. Returns true if eating started.
func (p *Player) beginEating() bool {
	hotbar, heldSlot, heldItem := p.HotbarSnapshot()
	_ = hotbar
	if heldItem == 0 {
		return false
	}
	if _, ok := world.FoodForItem(heldItem); !ok {
		return false
	}
	_ = heldSlot
	p.vitalsMu.Lock()
	p.Eating = true
	p.EatItem = heldItem
	p.EatTicks = 0
	p.vitalsMu.Unlock()
	return true
}

// Respawn handles a client_command perform_respawn (action 0): revives a dead
// player at spawn, resets vitals, and sends the Respawn packet + Position +
// UpdateHealth + Abilities. Broadcasts an entity_teleport so other players see
// the revived player back at spawn. No-op if the player isn't dead.
func (p *Player) Respawn() {
	p.vitalsMu.Lock()
	if !p.Dead {
		p.vitalsMu.Unlock()
		return
	}
	p.Health = maxHealth
	p.Food = 20
	p.Saturation = 5
	p.Exhaustion = 0
	p.FoodTick = 0
	p.Dead = false
	p.FallDistance = 0
	gm := p.Gamemode
	p.vitalsMu.Unlock()

	// Move to world spawn.
	spawnX, spawnY, spawnZ := 0.5, 4.0, 0.5
	if p.World != nil {
		sx, sy, sz := p.World.FindSafeSpawn(spawnX, spawnY, spawnZ)
		spawnX, spawnY, spawnZ = sx, sy, sz
	}
	p.posMu.Lock()
	p.X, p.Y, p.Z = spawnX, spawnY, spawnZ
	p.OnGround = true
	p.posMu.Unlock()

	// Respawn packet (same dimension). copyMetadata=false (fresh state).
	respawnPkt := p.Proto.WriteRespawn(
		0, "minecraft:overworld", p.World.Seed,
		gm, 0xFF, false, true,
		false, "", protocol.BlockPos{},
		0, 63, false,
	)
	_ = p.SendPacket(respawnPkt)

	// Abilities per gamemode.
	_ = p.SendPacket(p.Proto.WriteAbilities(abilitiesFor(gm)))

	// Position the client at spawn.
	px, py, pz, yaw, pitch, _ := p.Pos()
	_ = p.SendPacket(p.Proto.WritePosition(px, py, pz, yaw, pitch, 0x00, -1))

	p.sendHealth()

	// Tell other players where the revived player now is.
	_ = p.hooks.Broadcast(p.Proto.WriteEntityTeleport(p.EID, px, py, pz, yaw, pitch, true))
	slog.Info("player respawned", "name", p.Name, "eid", p.EID)
}

// SetGamemode switches the player's gamemode and pushes the wire updates:
// GameEvent (reason 3 = change_game_mode) to the player, Abilities, and a
// PlayerInfoUpdate gamemode broadcast so the tab list reflects it. Switching
// to creative/spectator cancels in-progress death (the player is revived).
func (p *Player) SetGamemode(gm uint8) {
	if gm > GamemodeSpectator {
		return
	}
	p.vitalsMu.Lock()
	prev := p.Gamemode
	p.Gamemode = gm
	if gm == GamemodeCreative || gm == GamemodeSpectator {
		p.Dead = false
		if p.Health <= 0 {
			p.Health = maxHealth
		}
		p.FallDistance = 0
	}
	p.vitalsMu.Unlock()
	if prev == gm {
		return
	}

	// Game state change reason 3 = change_game_mode; value is the new gamemode.
	_ = p.SendPacket(p.Proto.WriteGameEvent(3, float32(gm)))
	_ = p.SendPacket(p.Proto.WriteAbilities(abilitiesFor(gm)))

	// Tab list gamemode update broadcast.
	entry := protocol.PlayerInfoEntry{
		UUID:     p.UUID,
		Gamemode: int32(gm),
	}
	_ = p.hooks.Broadcast(p.Proto.WritePlayerInfoUpdate(
		protocol.PlayerInfoActionUpdateGamemode, []protocol.PlayerInfoEntry{entry}))

	slog.Info("gamemode changed", "name", p.Name, "from", prev, "to", gm)
}

// abilitiesFor returns the abilities flag byte for a gamemode.
//   - creative: allow_flying(0x04) | creative_mode(0x08)
//   - spectator: allow_flying(0x04) | flying(0x02) | creative_mode(0x08)
//   - survival/adventure: 0x00
func abilitiesFor(gm uint8) uint8 {
	switch gm {
	case GamemodeCreative:
		return 0x04 | 0x08
	case GamemodeSpectator:
		return 0x02 | 0x04 | 0x08
	default:
		return 0x00
	}
}

// posY / posXZ are lock-guarded position accessors for the survival tick.
func (p *Player) posY() float64 {
	p.posMu.RLock()
	defer p.posMu.RUnlock()
	return p.Y
}
func (p *Player) posXZ() (float64, float64) {
	p.posMu.RLock()
	defer p.posMu.RUnlock()
	return p.X, p.Z
}
