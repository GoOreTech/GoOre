// Package player handles per-client connections: FSM, keep-alive, packet dispatch.
// player.go owns the data + accessors. Per-concern handlers live in:
// fsm.go (lifecycle/dispatch), movement.go, digging.go, placing.go,
// inventory.go, chunk_encode.go, spawn.go, persist.go. See docs/player.md.
package player

import (
	"io"
	"sync"
	"sync/atomic"
	"time"

	"goore/internal/config"
	v772 "goore/internal/protocol/v772"
	"goore/internal/world"
)

type state uint8

const (
	stateHandshake state = iota
	stateStatus
	stateLogin
	stateConfiguration
	statePlay
	stateDisconnected
)

// Player represents a connected Minecraft client.
type Player struct {
	EID   int32
	Conn  io.ReadWriter
	Proto *v772.Protocol
	World *world.World
	Cfg   config.Config

	Name string
	UUID [16]byte

	// Position fields. HandleConn writes; server's broadcast/visibility goroutines read.
	// Use Pos() to read, setPos()/SetPositionForTest to write — direct access is racy.
	X, Y, Z    float64
	Yaw, Pitch float32
	OnGround   bool
	posMu      sync.RWMutex

	// Hotbar (9 slots), HeldSlot, HeldItem. Same pattern as position: writes on HandleConn,
	// reads on server goroutines. Use HotbarSnapshot() to read.
	//
	// HotbarCount is the per-slot stack size (Phase 5: eating consumes items).
	// Default 64 for non-empty slots (creative-infinite feel); 0 for empty. Not
	// part of HotbarSnapshot's int32-only return — read via HotbarCountSnapshot().
	Hotbar       [9]int32
	HotbarCount  [9]uint8
	HeldSlot     int
	HeldItem     int32
	hotbarMu     sync.RWMutex

	// Vitals (Phase 5 survival). Writes come from the movement handler
	// (fall damage) and the survival tick goroutine; reads come from
	// broadcast / persist. Use the Vitals() / Set*ForTest accessors —
	// direct field access is racy. See vitals.go.
	Health      float32
	Food        int32
	Saturation  float32
	Exhaustion  float32
	FoodTick    int32 // 20-Hz tick accumulator for regen / starvation
	Gamemode    uint8
	Dead        bool
	FallDistance float32 // accumulated since last ground contact
	Eating      bool
	EatItem     int32
	EatTicks    int32
	vitalsMu    sync.RWMutex

	// stateAtomic holds the FSM state (int32 because the standard library only ships
	// atomic.Int{32,64}). Zero value (0) maps to stateHandshake.
	stateAtomic atomic.Int32

	keepAliveID     atomic.Int64
	keepAliveTicker *time.Ticker
	disconnect      chan struct{}

	// hooks is the lifecycle + broadcast surface; always non-nil (New() installs
	// noOpHooks{} as the default). See hooks.go.
	hooks PlayerHooks
}

// New creates a new Player. Default hooks = noOpHooks (lifecycle no-op, broadcast self-send for standalone tests).
func New(eid int32, rw io.ReadWriter, proto *v772.Protocol, w *world.World, cfg config.Config) *Player {
	p := &Player{
		EID:   eid,
		Conn:  rw,
		Proto: proto,
		World: w,
		Cfg:   cfg,
		X:     0.5, Y: 4, Z: 0.5, // just above grass_block at y=3
		Yaw: 0, Pitch: 0,
		Health:     20,
		Food:       20,
		Saturation: 5,
		Gamemode:   cfg.Gamemode,
		disconnect: make(chan struct{}),
	}
	p.hooks = &noOpHooks{p: p}
	p.stateAtomic.Store(int32(stateHandshake))
	return p
}

// SetHooks installs a PlayerHooks implementation. nil is a no-op. The server's AcceptLoop calls this to wire real persistence + broadcast.
func (p *Player) SetHooks(h PlayerHooks) {
	if h == nil {
		return
	}
	p.hooks = h
}

// State returns the current FSM state. Safe to call from any goroutine.
func (p *Player) State() state {
	return state(p.stateAtomic.Load())
}

// setState atomically updates the FSM state.
func (p *Player) setState(s state) {
	p.stateAtomic.Store(int32(s))
}

// SendPacket sends a raw packet to the client.
func (p *Player) SendPacket(data []byte) error {
	_, err := p.Conn.Write(data)
	return err
}

// Close closes the underlying connection if it implements io.Closer. Returns nil otherwise.
func (p *Player) Close() error {
	if c, ok := p.Conn.(io.Closer); ok {
		return c.Close()
	}
	return nil
}

// SetPositionForTest sets the player's position and rotation directly. Test-only.
func (p *Player) SetPositionForTest(x, y, z float64, yaw, pitch float32) {
	p.posMu.Lock()
	defer p.posMu.Unlock()
	p.X = x
	p.Y = y
	p.Z = z
	p.Yaw = yaw
	p.Pitch = pitch
}

// Pos returns a snapshot of the player's position, rotation, and onGround flag. Safe from any goroutine.
func (p *Player) Pos() (x, y, z float64, yaw, pitch float32, onGround bool) {
	p.posMu.RLock()
	defer p.posMu.RUnlock()
	return p.X, p.Y, p.Z, p.Yaw, p.Pitch, p.OnGround
}

// HotbarSnapshot returns a copy of the hotbar state. Safe from any goroutine.
func (p *Player) HotbarSnapshot() (hotbar [9]int32, heldSlot int, heldItem int32) {
	p.hotbarMu.RLock()
	defer p.hotbarMu.RUnlock()
	return p.Hotbar, p.HeldSlot, p.HeldItem
}

// HotbarCountSnapshot returns a copy of the per-slot stack counts. Safe from any goroutine.
func (p *Player) HotbarCountSnapshot() [9]uint8 {
	p.hotbarMu.RLock()
	defer p.hotbarMu.RUnlock()
	return p.HotbarCount
}

// IsInPlayState returns true if the player has reached the play state. Test-only helper.
func (p *Player) IsInPlayState() bool {
	return p.State() == statePlay
}

// SetHeldItemForTest sets the held item directly. Test-only helper.
func (p *Player) SetHeldItemForTest(itemID int32) {
	p.hotbarMu.Lock()
	defer p.hotbarMu.Unlock()
	if p.HeldSlot < 0 || p.HeldSlot >= len(p.Hotbar) {
		p.HeldSlot = 0
	}
	p.Hotbar[p.HeldSlot] = itemID
	p.HeldItem = itemID
}
