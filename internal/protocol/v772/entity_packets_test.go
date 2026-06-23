package v772

import (
	"testing"

	"goore/internal/protocol"
)

// TestWriteSpawnEntityRoundTrip verifies the wire format of
// WriteSpawnEntity by decoding the encoded packet. Vanilla 1.21.8
// format: eid(v) + uuid(16) + type(v) + x,y,z(f64) + pitch(i8) +
// yaw(i8) + headPitch(i8) + objectData(v) + vx,vy,vz(i16×3).
func TestWriteSpawnEntityRoundTrip(t *testing.T) {
	uuid := [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	raw := (&Protocol{}).WriteSpawnEntity(42, uuid, 149 /*player*/, 1.5, 64.0, -3.5, 64, -128, 32)

	r := protocol.NewWireReader(raw)
	_ = r.VarInt() // length (ignored)
	if id := r.VarInt(); id != 0x01 {
		t.Fatalf("packet id = 0x%X, want 0x01", id)
	}
	if eid := r.VarInt(); eid != 42 {
		t.Errorf("eid = %d, want 42", eid)
	}
	var gotUUID [16]byte
	r.UUIDInto(&gotUUID)
	if gotUUID != uuid {
		t.Errorf("uuid mismatch: got %v, want %v", gotUUID, uuid)
	}
	if ty := r.VarInt(); ty != 149 {
		t.Errorf("type = %d, want 149 (player)", ty)
	}
	if x := r.Float64(); x != 1.5 {
		t.Errorf("x = %f, want 1.5", x)
	}
	if y := r.Float64(); y != 64.0 {
		t.Errorf("y = %f, want 64.0", y)
	}
	if z := r.Float64(); z != -3.5 {
		t.Errorf("z = %f, want -3.5", z)
	}
	if pitch := r.Byte(); pitch != 64 {
		t.Errorf("pitch = %d, want 64", pitch)
	}
	if yaw := r.Byte(); int8(yaw) != -128 {
		t.Errorf("yaw = %d, want -128 (0x80)", yaw)
	}
	if hpitch := r.Byte(); hpitch != 32 {
		t.Errorf("headPitch = %d, want 32", hpitch)
	}
	if od := r.VarInt(); od != 0 {
		t.Errorf("objectData = %d, want 0", od)
	}
	if vx := r.Int16(); vx != 0 {
		t.Errorf("vx = %d, want 0", vx)
	}
	if vy := r.Int16(); vy != 0 {
		t.Errorf("vy = %d, want 0", vy)
	}
	if vz := r.Int16(); vz != 0 {
		t.Errorf("vz = %d, want 0", vz)
	}
	if r.Err() != nil {
		t.Errorf("decode: %v", r.Err())
	}
}

// TestWriteRemoveEntitiesRoundTrip verifies the wire format of
// WriteRemoveEntities (0x46). Vanilla 1.21.8 format: count(v) + ids[v].
func TestWriteRemoveEntitiesRoundTrip(t *testing.T) {
	raw := (&Protocol{}).WriteRemoveEntities([]int32{5, 17, 99})

	r := protocol.NewWireReader(raw)
	_ = r.VarInt()
	if id := r.VarInt(); id != 0x46 {
		t.Fatalf("packet id = 0x%X, want 0x46", id)
	}
	if cnt := r.VarInt(); cnt != 3 {
		t.Errorf("count = %d, want 3", cnt)
	}
	for i, want := range []int32{5, 17, 99} {
		if got := r.VarInt(); got != want {
			t.Errorf("id[%d] = %d, want %d", i, got, want)
		}
	}
	if r.Err() != nil {
		t.Errorf("decode: %v", r.Err())
	}
}

// TestWriteRelEntityMoveRoundTrip verifies the wire format of
// WriteRelEntityMove (0x2E).
func TestWriteRelEntityMoveRoundTrip(t *testing.T) {
	raw := (&Protocol{}).WriteRelEntityMove(7, 100, 200, -300, true)

	r := protocol.NewWireReader(raw)
	_ = r.VarInt()
	if id := r.VarInt(); id != 0x2E {
		t.Fatalf("packet id = 0x%X, want 0x2E", id)
	}
	if eid := r.VarInt(); eid != 7 {
		t.Errorf("eid = %d, want 7", eid)
	}
	if dx := r.Int16(); dx != 100 {
		t.Errorf("dx = %d, want 100", dx)
	}
	if dy := r.Int16(); dy != 200 {
		t.Errorf("dy = %d, want 200", dy)
	}
	if dz := r.Int16(); dz != -300 {
		t.Errorf("dz = %d, want -300", dz)
	}
	if g := r.Bool(); !g {
		t.Errorf("onGround = false, want true")
	}
	if r.Err() != nil {
		t.Errorf("decode: %v", r.Err())
	}
}

// TestWriteEntityMoveLookRoundTrip verifies the wire format of
// WriteEntityMoveLook (0x2F).
func TestWriteEntityMoveLookRoundTrip(t *testing.T) {
	raw := (&Protocol{}).WriteEntityMoveLook(11, 50, -25, 0, 64, -32, false)

	r := protocol.NewWireReader(raw)
	_ = r.VarInt()
	if id := r.VarInt(); id != 0x2F {
		t.Fatalf("packet id = 0x%X, want 0x2F", id)
	}
	if eid := r.VarInt(); eid != 11 {
		t.Errorf("eid = %d, want 11", eid)
	}
	if dx := r.Int16(); dx != 50 {
		t.Errorf("dx = %d, want 50", dx)
	}
	if dy := r.Int16(); dy != -25 {
		t.Errorf("dy = %d, want -25", dy)
	}
	if dz := r.Int16(); dz != 0 {
		t.Errorf("dz = %d, want 0", dz)
	}
	if yaw := r.Byte(); int8(yaw) != 64 {
		t.Errorf("yaw = %d, want 64", yaw)
	}
	if pitch := r.Byte(); int8(pitch) != -32 {
		t.Errorf("pitch = %d, want -32", pitch)
	}
	if g := r.Bool(); g {
		t.Errorf("onGround = true, want false")
	}
	if r.Err() != nil {
		t.Errorf("decode: %v", r.Err())
	}
}

// TestWriteEntityHeadRotationRoundTrip verifies the wire format of
// WriteEntityHeadRotation (0x4C).
func TestWriteEntityHeadRotationRoundTrip(t *testing.T) {
	raw := (&Protocol{}).WriteEntityHeadRotation(13, 100)

	r := protocol.NewWireReader(raw)
	_ = r.VarInt()
	if id := r.VarInt(); id != 0x4C {
		t.Fatalf("packet id = 0x%X, want 0x4C", id)
	}
	if eid := r.VarInt(); eid != 13 {
		t.Errorf("eid = %d, want 13", eid)
	}
	if hy := r.Byte(); hy != 100 {
		t.Errorf("headYaw = %d, want 100", hy)
	}
	if r.Err() != nil {
		t.Errorf("decode: %v", r.Err())
	}
}

// TestWriteEntityTeleportRoundTrip verifies the wire format of
// WriteEntityTeleport (0x76).
//
// 1.21.2+ format (vanilla PositionMoveRotation-based):
//
//	entityId:    varint
//	pos.x:       f64
//	pos.y:       f64
//	pos.z:       f64
//	delta.x:     f64
//	delta.y:     f64
//	delta.z:     f64
//	yRot:        f32 (degrees, NOT i8 as in pre-1.21.2!)
//	xRot:        f32 (degrees, NOT i8 as in pre-1.21.2!)
//	relatives:   u32 BE bitmask (9 bits: X, Y, Z, Y_ROT, X_ROT, DELTA_X, DELTA_Y, DELTA_Z, ROTATE_DELTA)
//	onGround:    bool
//
// The pre-1.21.2 format (EID + 3×f64 + 2×i8 + bool) is rejected by the
// 1.21.8 vanilla client with `readerIndex(26) + length(8) exceeds
// writerIndex(29)` because the decoder expects deltaX (f64) at byte 26
// but only 3 bytes remain. The old format sent yaw/pitch as i8 instead
// of f32 and had no delta or bitmask, so every byte from the deltas
// onwards was a leftover 1.21.x field that the decoder tried to parse.
//
// The bitmask is big-endian u32; RelativeMovements::all_absolute() = 0
// means "client REPLACES position with pos.x/y/z from the packet"
// (no current-pos addition).
func TestWriteEntityTeleportRoundTrip(t *testing.T) {
	raw := (&Protocol{}).WriteEntityTeleport(99, 1.0, 64.5, -1.5, 90.0, -45.0, true)

	r := protocol.NewWireReader(raw)
	_ = r.VarInt() // packet length
	if id := r.VarInt(); id != 0x76 {
		t.Fatalf("packet id = 0x%X, want 0x76", id)
	}
	if eid := r.VarInt(); eid != 99 {
		t.Errorf("eid = %d, want 99", eid)
	}
	// pos (3×f64)
	if x := r.Float64(); x != 1.0 {
		t.Errorf("pos.x = %f, want 1.0", x)
	}
	if y := r.Float64(); y != 64.5 {
		t.Errorf("pos.y = %f, want 64.5", y)
	}
	if z := r.Float64(); z != -1.5 {
		t.Errorf("pos.z = %f, want -1.5", z)
	}
	// delta (3×f64) — must be zero for absolute teleport
	if dx := r.Float64(); dx != 0.0 {
		t.Errorf("delta.x = %f, want 0.0 (absolute teleport has zero delta)", dx)
	}
	if dy := r.Float64(); dy != 0.0 {
		t.Errorf("delta.y = %f, want 0.0", dy)
	}
	if dz := r.Float64(); dz != 0.0 {
		t.Errorf("delta.z = %f, want 0.0", dz)
	}
	// yRot, xRot (2×f32) — float, NOT i8
	if yaw := r.Float32(); yaw != 90.0 {
		t.Errorf("yRot = %f, want 90.0", yaw)
	}
	if pitch := r.Float32(); pitch != -45.0 {
		t.Errorf("xRot = %f, want -45.0", pitch)
	}
	// relatives bitmask (u32 BE = 4 bytes)
	b0 := r.Byte()
	b1 := r.Byte()
	b2 := r.Byte()
	b3 := r.Byte()
	bitmask := uint32(b0)<<24 | uint32(b1)<<16 | uint32(b2)<<8 | uint32(b3)
	if bitmask != 0 {
		t.Errorf("relatives bitmask = 0x%08X, want 0 (all-absolute)", bitmask)
	}
	// onGround
	if onGround := r.Bool(); !onGround {
		t.Errorf("onGround = false, want true")
	}
	if r.Err() != nil {
		t.Errorf("decode: %v", r.Err())
	}
}

// TestWriteEntityMetadataRoundTrip verifies the wire format of
// WriteEntityMetadata (0x5C). Each entry is (key: u8, type: v,
// value: type-specific). The list is terminated by 0xFF.
func TestWriteEntityMetadataRoundTrip(t *testing.T) {
	entries := []protocol.MetadataEntry{
		protocol.MetadataByte(0, 0),     // shared_flags
		protocol.MetadataVarInt(1, 300), // air_supply
		protocol.MetadataBool(3, false), // custom_name_visible
		protocol.MetadataFloat(7, 1.5),  // some float metadata
	}
	raw := (&Protocol{}).WriteEntityMetadata(42, entries)

	r := protocol.NewWireReader(raw)
	_ = r.VarInt()
	if id := r.VarInt(); id != 0x5C {
		t.Fatalf("packet id = 0x%X, want 0x5C", id)
	}
	if eid := r.VarInt(); eid != 42 {
		t.Errorf("eid = %d, want 42", eid)
	}
	// Entry 1: key=0, type=0 (byte), value=0
	if k := r.Byte(); k != 0 {
		t.Errorf("entry 0 key = %d, want 0", k)
	}
	if ty := r.VarInt(); ty != 0 {
		t.Errorf("entry 0 type = %d, want 0 (byte)", ty)
	}
	if v := r.Byte(); v != 0 {
		t.Errorf("entry 0 value = %d, want 0", v)
	}
	// Entry 2: key=1, type=1 (varint), value=300
	if k := r.Byte(); k != 1 {
		t.Errorf("entry 1 key = %d, want 1", k)
	}
	if ty := r.VarInt(); ty != 1 {
		t.Errorf("entry 1 type = %d, want 1 (varint)", ty)
	}
	if v := r.VarInt(); v != 300 {
		t.Errorf("entry 1 value = %d, want 300", v)
	}
	// Entry 3: key=3, type=8 (bool), value=false
	if k := r.Byte(); k != 3 {
		t.Errorf("entry 2 key = %d, want 3", k)
	}
	if ty := r.VarInt(); ty != 8 {
		t.Errorf("entry 2 type = %d, want 8 (bool)", ty)
	}
	if v := r.Bool(); v {
		t.Errorf("entry 2 value = true, want false")
	}
	// Entry 4: key=7, type=3 (float), value=1.5
	if k := r.Byte(); k != 7 {
		t.Errorf("entry 3 key = %d, want 7", k)
	}
	if ty := r.VarInt(); ty != 3 {
		t.Errorf("entry 3 type = %d, want 3 (float)", ty)
	}
	if v := r.Float32(); v != 1.5 {
		t.Errorf("entry 3 value = %f, want 1.5", v)
	}
	// Terminator
	if term := r.Byte(); term != 0xFF {
		t.Errorf("terminator = 0x%X, want 0xFF", term)
	}
	if r.Err() != nil {
		t.Errorf("decode: %v", r.Err())
	}
}

// TestWritePlayerInfoUpdateRoundTrip verifies the wire format of
// WritePlayerInfoUpdate (0x3F) — the packet that registers a player
// in the tab list BEFORE spawn_entity. Without this packet, the
// vanilla 1.21.8 client will not render the spawned player entity
// (no skin / gamemode data to attach).
//
// Vanilla 1.21.8 wire format (with actions = add_player |
// update_game_mode | update_listed | update_latency):
//
//	actions:   u8 bitmask (0x1D for the four actions above)
//	count:     VarInt (1)
//	entry 0:
//	  uuid:     UUID (16 bytes)
//	  name:     string
//	  props:    []varint  count (0)
//	  gamemode: VarInt
//	  listed:   VarInt
//	  latency:  VarInt
func TestWritePlayerInfoUpdateRoundTrip(t *testing.T) {
	uuid := [16]byte{0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	raw := (&Protocol{}).WritePlayerInfoUpdate(
		protocol.PlayerInfoActionAddPlayer|
			protocol.PlayerInfoActionUpdateGamemode|
			protocol.PlayerInfoActionUpdateListed|
			protocol.PlayerInfoActionUpdateLatency,
		[]protocol.PlayerInfoEntry{{
			UUID:     uuid,
			Name:     "TestPlayer",
			Gamemode: 1, // creative
			Listed:   true,
			Latency:  42,
		}},
	)

	r := protocol.NewWireReader(raw)
	_ = r.VarInt() // length prefix
	if id := r.VarInt(); id != 0x3F {
		t.Fatalf("packet id = 0x%X, want 0x3F", id)
	}
	if actions := r.Byte(); actions != 0x1D {
		t.Errorf("actions = 0x%X, want 0x1D (add_player | update_game_mode | update_listed | update_latency)", actions)
	}
	if cnt := r.VarInt(); cnt != 1 {
		t.Fatalf("count = %d, want 1", cnt)
	}
	// entry 0: uuid (16 bytes)
	var gotUUID [16]byte
	r.UUIDInto(&gotUUID)
	if gotUUID != uuid {
		t.Errorf("uuid mismatch: got %v, want %v", gotUUID, uuid)
	}
	// name
	if name := r.String(); name != "TestPlayer" {
		t.Errorf("name = %q, want %q", name, "TestPlayer")
	}
	// properties (count=0)
	if props := r.VarInt(); props != 0 {
		t.Errorf("properties count = %d, want 0", props)
	}
	// gamemode
	if gm := r.VarInt(); gm != 1 {
		t.Errorf("gamemode = %d, want 1 (creative)", gm)
	}
	// listed
	if listed := r.VarInt(); listed != 1 {
		t.Errorf("listed = %d, want 1", listed)
	}
	// latency
	if lat := r.VarInt(); lat != 42 {
		t.Errorf("latency = %d, want 42", lat)
	}
	if r.Err() != nil {
		t.Errorf("decode: %v", r.Err())
	}
}

// TestWritePlayerRemoveRoundTrip verifies the wire format of
// WritePlayerRemove (0x3E) — sent on disconnect so the client
// removes the player from the tab list.
//
// Vanilla 1.21.8 wire format:
//
//	count:  VarInt (number of UUIDs)
//	uuids:  UUID[count]
func TestWritePlayerRemoveRoundTrip(t *testing.T) {
	uuidA := [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	uuidB := [16]byte{16, 15, 14, 13, 12, 11, 10, 9, 8, 7, 6, 5, 4, 3, 2, 1}
	raw := (&Protocol{}).WritePlayerRemove([][16]byte{uuidA, uuidB})

	r := protocol.NewWireReader(raw)
	_ = r.VarInt() // length prefix
	if id := r.VarInt(); id != 0x3E {
		t.Fatalf("packet id = 0x%X, want 0x3E", id)
	}
	if cnt := r.VarInt(); cnt != 2 {
		t.Fatalf("count = %d, want 2", cnt)
	}
	var gotA, gotB [16]byte
	r.UUIDInto(&gotA)
	r.UUIDInto(&gotB)
	if gotA != uuidA {
		t.Errorf("uuid[0] mismatch: got %v, want %v", gotA, uuidA)
	}
	if gotB != uuidB {
		t.Errorf("uuid[1] mismatch: got %v, want %v", gotB, uuidB)
	}
	if r.Err() != nil {
		t.Errorf("decode: %v", r.Err())
	}
}

// TestWritePlayerInfoUpdateWithoutChatSession ensures we do NOT
// accidentally write the chat_session field (bit 0x02). Sending a
// malformed chat_session corrupts every subsequent entry of the
// packet and crashes the client with a chat_session decoder error.
func TestWritePlayerInfoUpdateWithoutChatSession(t *testing.T) {
	raw := (&Protocol{}).WritePlayerInfoUpdate(
		protocol.PlayerInfoActionAddPlayer,
		[]protocol.PlayerInfoEntry{{
			UUID: [16]byte{1, 2, 3},
			Name: "X",
		}},
	)
	r := protocol.NewWireReader(raw)
	_ = r.VarInt()
	_ = r.VarInt() // id
	if actions := r.Byte(); actions&0x02 != 0 {
		t.Errorf("actions = 0x%X, must NOT include init_chat (0x02)", actions)
	}
	// Decode the rest; if we accidentally wrote chat_session bytes,
	// the parser will fail because we don't have a chat_session
	// decoder.
	var discard [16]byte
	r.UUIDInto(&discard)
	_ = r.String()
	if r.Err() != nil {
		t.Errorf("decode: %v", r.Err())
	}
}
