package player

import (
	"sync"
)

// Gamemode constants. Values match the wire / registry convention
// (0=survival, 1=creative, 2=adventure, 3=spectator).
const (
	GamemodeSurvival  uint8 = 0
	GamemodeCreative  uint8 = 1
	GamemodeAdventure uint8 = 2
	GamemodeSpectator uint8 = 3
)

// Vitals is a concurrency-safe snapshot of the survival-relevant player state.
// Reads from server goroutines (broadcast, persist) MUST go through Vitals();
// writes MUST go through the Set*/apply* helpers in vitals.go / survival.go.
type Vitals struct {
	Health       float32
	Food         int32
	Saturation   float32
	Exhaustion   float32
	FoodTick     int32
	Gamemode     uint8
	Dead         bool
	FallDistance float32
	Eating       bool
	EatItem      int32
	EatTicks     int32
}

// Vitals returns a snapshot of the survival state. Safe from any goroutine.
func (p *Player) Vitals() Vitals {
	p.vitalsMu.RLock()
	defer p.vitalsMu.RUnlock()
	return Vitals{
		Health:       p.Health,
		Food:         p.Food,
		Saturation:   p.Saturation,
		Exhaustion:   p.Exhaustion,
		FoodTick:     p.FoodTick,
		Gamemode:     p.Gamemode,
		Dead:         p.Dead,
		FallDistance: p.FallDistance,
		Eating:       p.Eating,
		EatItem:      p.EatItem,
		EatTicks:     p.EatTicks,
	}
}

// SetVitalsForTest replaces the full vitals state. Test-only.
func (p *Player) SetVitalsForTest(v Vitals) {
	p.vitalsMu.Lock()
	defer p.vitalsMu.Unlock()
	p.Health = v.Health
	p.Food = v.Food
	p.Saturation = v.Saturation
	p.Exhaustion = v.Exhaustion
	p.FoodTick = v.FoodTick
	p.Gamemode = v.Gamemode
	p.Dead = v.Dead
	p.FallDistance = v.FallDistance
	p.Eating = v.Eating
	p.EatItem = v.EatItem
	p.EatTicks = v.EatTicks
}

// SetGamemodeForTest sets the gamemode. Test-only (production path is
// SetGamemode in survival.go, which also sends the wire packets).
func (p *Player) SetGamemodeForTest(gm uint8) {
	p.vitalsMu.Lock()
	defer p.vitalsMu.Unlock()
	p.Gamemode = gm
}

// IsCreative returns true if the player is in creative mode (immune to damage,
// can fly, instant-break). Safe from any goroutine.
func (p *Player) IsCreative() bool {
	p.vitalsMu.RLock()
	defer p.vitalsMu.RUnlock()
	return p.Gamemode == GamemodeCreative
}

// IsSpectator returns true if the player is in spectator mode.
func (p *Player) IsSpectator() bool {
	p.vitalsMu.RLock()
	defer p.vitalsMu.RUnlock()
	return p.Gamemode == GamemodeSpectator
}

// TakesDamage reports whether the player should be subject to survival damage
// right now. Creative and spectator players never take damage; dead players
// can't be damaged again.
func (p *Player) TakesDamage() bool {
	p.vitalsMu.RLock()
	defer p.vitalsMu.RUnlock()
	if p.Dead {
		return false
	}
	return p.Gamemode == GamemodeSurvival || p.Gamemode == GamemodeAdventure
}

// vitalsLock returns the vitals mutex for callers that need to compose a
// read-modify-write under a single lock (e.g. the survival tick). Internal use.
func (p *Player) vitalsLock() sync.Locker { return &p.vitalsMu }
