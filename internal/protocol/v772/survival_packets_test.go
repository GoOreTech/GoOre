package v772_test

import (
	"math"
	"testing"

	"goore/internal/protocol"
	"goore/internal/protocol/v772"
)

// TestWriteDamageEvent_NoSourcePos pins the byte layout of a damage_event with
// no cause entities and no source position — the common case for environmental
// damage (fall, void, starvation) in GoOre.
func TestWriteDamageEvent_NoSourcePos(t *testing.T) {
	p := v772.New()
	pkt := p.WriteDamageEvent(42, v772.DamageTypeFall, -1, -1, false, 0, 0, 0)
	if pkt == nil {
		t.Fatal("packet is nil")
	}
	r := protocol.NewWireReader(pkt)
	r.VarInt() // totalLen
	if id := r.VarInt(); id != v772.PlayDamageEvent {
		t.Fatalf("packet ID = 0x%02X, want 0x%02X", id, v772.PlayDamageEvent)
	}
	if eid := r.VarInt(); eid != 42 {
		t.Errorf("entityID = %d, want 42", eid)
	}
	if st := r.VarInt(); st != v772.DamageTypeFall {
		t.Errorf("sourceTypeID = %d, want %d (fall)", st, v772.DamageTypeFall)
	}
	if c := r.VarInt(); c != -1 {
		t.Errorf("sourceCauseID = %d, want -1", c)
	}
	if d := r.VarInt(); d != -1 {
		t.Errorf("sourceDirectID = %d, want -1", d)
	}
	if hasPos := r.Bool(); hasPos {
		t.Errorf("hasSourcePos = true, want false")
	}
	if err := r.Err(); err != nil {
		t.Fatalf("reader error: %v", err)
	}
	if r.Remaining() != 0 {
		t.Errorf("trailing bytes: %d", r.Remaining())
	}
}

// TestWriteDamageEvent_WithSourcePos verifies the boolean-prefixed Optional<Vec3>
// is written (and consumed) correctly. A missing bool prefix or wrong f64 order
// shifts every subsequent field.
func TestWriteDamageEvent_WithSourcePos(t *testing.T) {
	p := v772.New()
	pkt := p.WriteDamageEvent(7, v772.DamageTypeLava, 3, -1, true, 1.5, 2.5, 3.5)
	r := protocol.NewWireReader(pkt)
	r.VarInt()
	r.VarInt()
	if eid := r.VarInt(); eid != 7 {
		t.Fatalf("entityID = %d, want 7", eid)
	}
	if st := r.VarInt(); st != v772.DamageTypeLava {
		t.Errorf("sourceTypeID = %d, want %d (lava)", st, v772.DamageTypeLava)
	}
	if c := r.VarInt(); c != 3 {
		t.Errorf("sourceCauseID = %d, want 3", c)
	}
	if d := r.VarInt(); d != -1 {
		t.Errorf("sourceDirectID = %d, want -1", d)
	}
	if !r.Bool() {
		t.Fatal("hasSourcePos = false, want true")
	}
	if x := r.Float64(); x != 1.5 {
		t.Errorf("sourceX = %v, want 1.5", x)
	}
	if y := r.Float64(); y != 2.5 {
		t.Errorf("sourceY = %v, want 2.5", y)
	}
	if z := r.Float64(); z != 3.5 {
		t.Errorf("sourceZ = %v, want 3.5", z)
	}
	if err := r.Err(); err != nil {
		t.Fatalf("reader error: %v", err)
	}
	if r.Remaining() != 0 {
		t.Errorf("trailing bytes: %d", r.Remaining())
	}
}

// TestWriteEntityStatus pins the i32 (NOT VarInt) entityId field. Writing a
// VarInt here desyncs the death animation packet and the client shows no death tilt.
func TestWriteEntityStatus(t *testing.T) {
	p := v772.New()
	pkt := p.WriteEntityStatus(99, v772.EntityStatusPlayDeathSound)
	if pkt == nil {
		t.Fatal("packet is nil")
	}
	r := protocol.NewWireReader(pkt)
	r.VarInt() // totalLen
	if id := r.VarInt(); id != v772.PlayEntityStatus {
		t.Fatalf("packet ID = 0x%02X, want 0x%02X", id, v772.PlayEntityStatus)
	}
	// entityId is i32 BE: a value of 99 serializes as 4 bytes 00 00 00 63.
	if eid := r.Int32(); eid != 99 {
		t.Errorf("entityID = %d, want 99", eid)
	}
	if status := int8(r.Byte()); status != v772.EntityStatusPlayDeathSound {
		t.Errorf("status = %d, want %d", status, v772.EntityStatusPlayDeathSound)
	}
	if r.Remaining() != 0 {
		t.Errorf("trailing bytes: %d", r.Remaining())
	}
}

// TestWriteHurtAnimation verifies the 2-field hurt_animation (entityId VarInt + yaw f32).
func TestWriteHurtAnimation(t *testing.T) {
	p := v772.New()
	pkt := p.WriteHurtAnimation(5, 90.0)
	r := protocol.NewWireReader(pkt)
	r.VarInt()
	r.VarInt()
	if eid := r.VarInt(); eid != 5 {
		t.Errorf("entityID = %d, want 5", eid)
	}
	yaw := r.Float32()
	if math.Abs(float64(yaw)-90.0) > 1e-3 {
		t.Errorf("yaw = %v, want 90.0", yaw)
	}
	if r.Remaining() != 0 {
		t.Errorf("trailing bytes: %d", r.Remaining())
	}
}

// TestWriteRespawn_AfterDeath pins the full Respawn (0x4B) layout used when a
// player clicks "Respawn" on the death screen. copyMetadata=false, no death
// location is sent (we don't anchor a compass to the death spot yet).
func TestWriteRespawn_AfterDeath(t *testing.T) {
	p := v772.New()
	pkt := p.WriteRespawn(
		0,                       // dimensionType = overworld
		"minecraft:overworld",   // dimensionName
		12345,                   // hashedSeed
		0,                       // gamemode = survival
		0xFF,                    // prevGamemode = -1 (none)
		false,                   // isDebug
		true,                    // isFlat
		false,                   // hasDeath
		"",                      // deathDimensionName
		protocol.BlockPos{},     // deathPos
		0,                       // portalCooldown
		63,                      // seaLevel
		false,                   // copyMetadata
	)
	if pkt == nil {
		t.Fatal("packet is nil")
	}
	r := protocol.NewWireReader(pkt)
	r.VarInt()
	if id := r.VarInt(); id != v772.PlayRespawn {
		t.Fatalf("packet ID = 0x%02X, want 0x%02X", id, v772.PlayRespawn)
	}
	if dt := r.VarInt(); dt != 0 {
		t.Errorf("dimensionType = %d, want 0", dt)
	}
	if name := r.String(); name != "minecraft:overworld" {
		t.Errorf("dimensionName = %q, want %q", name, "minecraft:overworld")
	}
	if seed := r.Int64(); seed != 12345 {
		t.Errorf("hashedSeed = %d, want 12345", seed)
	}
	if gm := r.Byte(); gm != 0 {
		t.Errorf("gamemode = %d, want 0 (survival)", gm)
	}
	if pg := r.Byte(); pg != 0xFF {
		t.Errorf("prevGamemode = %d, want 0xFF (-1)", pg)
	}
	if r.Bool() {
		t.Error("isDebug = true, want false")
	}
	if !r.Bool() {
		t.Error("isFlat = false, want true")
	}
	if r.Bool() {
		t.Error("hasDeath = true, want false")
	}
	if pc := r.VarInt(); pc != 0 {
		t.Errorf("portalCooldown = %d, want 0", pc)
	}
	if sl := r.VarInt(); sl != 63 {
		t.Errorf("seaLevel = %d, want 63", sl)
	}
	if cm := r.Byte(); cm != 0 {
		t.Errorf("copyMetadata = %d, want 0", cm)
	}
	if err := r.Err(); err != nil {
		t.Fatalf("reader error: %v", err)
	}
	if r.Remaining() != 0 {
		t.Errorf("trailing bytes: %d", r.Remaining())
	}
}

// TestWriteRespawn_WithDeath verifies the optional death location block is
// written (bool=true + dimensionName string + packed BlockPos) and consumed cleanly.
func TestWriteRespawn_WithDeath(t *testing.T) {
	p := v772.New()
	dp := protocol.BlockPos{X: 10, Y: 64, Z: -20}
	pkt := p.WriteRespawn(0, "minecraft:overworld", 1, 0, 0xFF, false, true,
		true, "minecraft:overworld", dp, 0, 63, true)
	r := protocol.NewWireReader(pkt)
	r.VarInt()
	r.VarInt()
	r.VarInt()        // dimensionType
	_ = r.String()    // dimensionName
	r.Int64()         // hashedSeed
	r.Byte()          // gamemode
	r.Byte()          // prevGamemode
	r.Bool()          // isDebug
	r.Bool()          // isFlat
	if !r.Bool() {
		t.Fatal("hasDeath = false, want true")
	}
	if name := r.String(); name != "minecraft:overworld" {
		t.Errorf("deathDimensionName = %q, want %q", name, "minecraft:overworld")
	}
	packed := r.Int64()
	want := (int64(dp.X)&0x3FFFFFF)<<38 | (int64(dp.Z)&0x3FFFFFF)<<12 | (int64(dp.Y)&0xFFF)
	if packed != want {
		t.Errorf("deathPos packed = 0x%X, want 0x%X", packed, want)
	}
	r.VarInt() // portalCooldown
	r.VarInt() // seaLevel
	if cm := r.Byte(); cm != 1 {
		t.Errorf("copyMetadata = %d, want 1", cm)
	}
	if r.Remaining() != 0 {
		t.Errorf("trailing bytes: %d", r.Remaining())
	}
}
