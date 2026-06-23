// Package player — persistence to disk. Custom binary format; see docs/world.md §On-disk formats.
package player

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
)

const (
	playerMagic   uint32 = 0x524F4C50 // "PLOR" little-endian
	playerVersion uint8  = 2          // v2: + [9]uint8 HotbarCount (Phase 5 eating)
	maxNameLen           = 16
)

// PlayerPath returns the on-disk path for a player file: <dir>/players/<uuid-hex>.dat.
func PlayerPath(dir string, uuid [16]byte) string {
	hex := fmt.Sprintf("%02x%02x%02x%02x%02x%02x%02x%02x%02x%02x%02x%02x%02x%02x%02x%02x",
		uuid[0], uuid[1], uuid[2], uuid[3], uuid[4], uuid[5], uuid[6], uuid[7],
		uuid[8], uuid[9], uuid[10], uuid[11], uuid[12], uuid[13], uuid[14], uuid[15])
	return filepath.Join(dir, "players", hex+".dat")
}

// SavePlayer writes player data to disk atomically (temp + rename).
func SavePlayer(dir string, p *Player) error {
	path := PlayerPath(dir, p.UUID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir players dir: %w", err)
	}

	if len(p.Name) > maxNameLen {
		return fmt.Errorf("player name too long: %d > %d", len(p.Name), maxNameLen)
	}

	size := 4 + 1 + 1 + len(p.Name) + 16 + 24 + 8 + 1 + 36 + 4 + 9 + 4 + 4 + 4 + 1 // v2: +HotbarCount(9) +Health(4)+Food(4)+Saturation(4)+Gamemode(1)
	buf := make([]byte, size)
	off := 0

	binary.LittleEndian.PutUint32(buf[off:], playerMagic)
	off += 4
	buf[off] = playerVersion
	off++
	buf[off] = uint8(len(p.Name))
	off++
	copy(buf[off:], p.Name)
	off += len(p.Name)
	copy(buf[off:], p.UUID[:])
	off += 16
	// Snapshot position under posMu.RLock.
	px, py, pz, yaw, pitch, onGround := p.Pos()
	binary.LittleEndian.PutUint64(buf[off:], math.Float64bits(px))
	off += 8
	binary.LittleEndian.PutUint64(buf[off:], math.Float64bits(py))
	off += 8
	binary.LittleEndian.PutUint64(buf[off:], math.Float64bits(pz))
	off += 8
	binary.LittleEndian.PutUint32(buf[off:], math.Float32bits(yaw))
	off += 4
	binary.LittleEndian.PutUint32(buf[off:], math.Float32bits(pitch))
	off += 4
	if onGround {
		buf[off] = 1
	} else {
		buf[off] = 0
	}
	off++
	hotbar, heldSlot, _ := p.HotbarSnapshot()
	counts := p.HotbarCountSnapshot()
	for i := 0; i < 9; i++ {
		binary.LittleEndian.PutUint32(buf[off:], uint32(hotbar[i]))
		off += 4
	}
	binary.LittleEndian.PutUint32(buf[off:], uint32(heldSlot))
	off += 4
	for i := 0; i < 9; i++ {
		buf[off] = counts[i]
		off++
	}
	// Vitals (Phase 5): Health, Food, Saturation, Gamemode. Exhaustion/FoodTick/
	// Dead are NOT persisted — Dead resets to false on rejoin (a reconnecting
	// player is alive), and Exhaustion/FoodTick resetting is harmless.
	v := p.Vitals()
	binary.LittleEndian.PutUint32(buf[off:], math.Float32bits(v.Health))
	off += 4
	binary.LittleEndian.PutUint32(buf[off:], uint32(v.Food))
	off += 4
	binary.LittleEndian.PutUint32(buf[off:], math.Float32bits(v.Saturation))
	off += 4
	buf[off] = v.Gamemode
	off++

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, buf, 0o644); err != nil {
		return fmt.Errorf("write temp player: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename player: %w", err)
	}
	return nil
}

// LoadPlayer reads a player file from disk. Returns an error if the file is missing, corrupt, or has an unsupported version.
func LoadPlayer(dir string, uuid [16]byte) (*Player, error) {
	path := PlayerPath(dir, uuid)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read player: %w", err)
	}
	if len(data) < 4+1+1+16+24+8+1+36+4 {
		return nil, fmt.Errorf("player file too short: %d bytes", len(data))
	}
	off := 0

	magic := binary.LittleEndian.Uint32(data[off:])
	off += 4
	if magic != playerMagic {
		return nil, errors.New("bad player magic")
	}
	ver := data[off]
	off++
	if ver != 1 && ver != 2 {
		return nil, fmt.Errorf("unsupported player version %d", ver)
	}
	nameLen := int(data[off])
	off++
	if nameLen < 0 || nameLen > maxNameLen {
		return nil, fmt.Errorf("invalid name length %d", nameLen)
	}
	if off+nameLen > len(data) {
		return nil, errors.New("name extends past file")
	}
	name := string(data[off : off+nameLen])
	off += nameLen

	var readUUID [16]byte
	copy(readUUID[:], data[off:off+16])
	off += 16
	x := math.Float64frombits(binary.LittleEndian.Uint64(data[off:]))
	off += 8
	y := math.Float64frombits(binary.LittleEndian.Uint64(data[off:]))
	off += 8
	z := math.Float64frombits(binary.LittleEndian.Uint64(data[off:]))
	off += 8
	yaw := math.Float32frombits(binary.LittleEndian.Uint32(data[off:]))
	off += 4
	pitch := math.Float32frombits(binary.LittleEndian.Uint32(data[off:]))
	off += 4
	onGround := data[off] != 0
	off++
	var hotbar [9]int32
	for i := 0; i < 9; i++ {
		hotbar[i] = int32(binary.LittleEndian.Uint32(data[off:]))
		off += 4
	}
	heldSlot := int(binary.LittleEndian.Uint32(data[off:]))
	off += 4

	var counts [9]uint8
	var health float32 = 20
	var food int32 = 20
	var saturation float32 = 5
	var gamemode uint8 = 1
	if ver >= 2 {
		if off+9 > len(data) {
			return nil, errors.New("player file too short for v2 counts")
		}
		copy(counts[:], data[off:off+9])
		off += 9
		if off+13 > len(data) {
			return nil, errors.New("player file too short for v2 vitals")
		}
		health = math.Float32frombits(binary.LittleEndian.Uint32(data[off:]))
		off += 4
		food = int32(binary.LittleEndian.Uint32(data[off:]))
		off += 4
		saturation = math.Float32frombits(binary.LittleEndian.Uint32(data[off:]))
		off += 4
		gamemode = data[off]
		off++
	} else {
		// v1 migration: counts unknown — assume 64 for non-empty slots, 0 for empty.
		for i := 0; i < 9; i++ {
			if hotbar[i] != 0 {
				counts[i] = 64
			}
		}
	}

	p := &Player{
		Name:        name,
		UUID:        readUUID,
		X:           x,
		Y:           y,
		Z:           z,
		Yaw:         yaw,
		Pitch:       pitch,
		OnGround:    onGround,
		Hotbar:      hotbar,
		HotbarCount: counts,
		HeldSlot:    heldSlot,
		Health:      health,
		Food:        food,
		Saturation:  saturation,
		Gamemode:    gamemode,
	}
	p.HeldItem = p.Hotbar[p.HeldSlot]
	return p, nil
}

// LoadStateFromDisk reads the saved state for this player's UUID and merges it into the receiver. Called on login after the UUID is parsed but before the position is sent to the client. See docs/player.md §State persistence on connect.
func (p *Player) LoadStateFromDisk(dir string) error {
	if dir == "" {
		return nil
	}
	path := PlayerPath(dir, p.UUID)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil // first join — no saved state
	} else if err != nil {
		return fmt.Errorf("stat player file: %w", err)
	}
	loaded, err := LoadPlayer(dir, p.UUID)
	if err != nil {
		return fmt.Errorf("load player: %w", err)
	}
	// Don't touch p.EID, p.Conn, p.Proto, p.World, p.Cfg, p.stateAtomic — those are owned by the live Player.
	p.Name = loaded.Name
	p.posMu.Lock()
	p.X = loaded.X
	p.Y = loaded.Y
	p.Z = loaded.Z
	p.Yaw = loaded.Yaw
	p.Pitch = loaded.Pitch
	p.OnGround = loaded.OnGround
	p.posMu.Unlock()
	p.hotbarMu.Lock()
	p.Hotbar = loaded.Hotbar
	p.HotbarCount = loaded.HotbarCount
	p.HeldSlot = loaded.HeldSlot
	p.HeldItem = loaded.HeldItem
	p.hotbarMu.Unlock()
	p.vitalsMu.Lock()
	p.Health = loaded.Health
	p.Food = loaded.Food
	p.Saturation = loaded.Saturation
	p.Gamemode = loaded.Gamemode
	p.Dead = false // a reconnecting player is never dead
	p.vitalsMu.Unlock()
	return nil
}
