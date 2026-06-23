// Package protocol defines the ProtocolVersion interface and wire encoding primitives.
package protocol

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math"
	"unicode/utf8"
)

// WireWriter accumulates bytes with deferred error checking.
// After all writes, call Err() once to check for errors.
// Once Err() returns non-nil, all subsequent writes become no-ops.
type WireWriter struct {
	buf bytes.Buffer
	err error
}

// Err returns the first error encountered during writing.
func (w *WireWriter) Err() error { return w.err }

// Bytes returns the written bytes. If Err() is non-nil, returns nil.
func (w *WireWriter) Bytes() []byte {
	if w.err != nil {
		return nil
	}
	return w.buf.Bytes()
}

// RawWrite writes raw bytes without any length prefix. Prefer ByteArray for length-prefixed data.
func (w *WireWriter) RawWrite(b []byte) {
	if w.err != nil {
		return
	}
	_, w.err = w.buf.Write(b)
}

func (w *WireWriter) Reset() {
	w.buf.Reset()
	w.err = nil
}

// Byte writes a single unsigned byte.
func (w *WireWriter) Byte(v uint8) {
	if w.err != nil {
		return
	}
	w.err = w.buf.WriteByte(v)
}

// Bool writes a boolean as a single byte (0x00 or 0x01).
func (w *WireWriter) Bool(v bool) {
	if w.err != nil {
		return
	}
	b := uint8(0)
	if v {
		b = 1
	}
	w.err = w.buf.WriteByte(b)
}

// Int16 writes a big-endian int16.
func (w *WireWriter) Int16(v int16) {
	if w.err != nil {
		return
	}
	w.err = binary.Write(&w.buf, binary.BigEndian, v)
}

// Uint16 writes a big-endian uint16.
func (w *WireWriter) Uint16(v uint16) {
	if w.err != nil {
		return
	}
	w.err = binary.Write(&w.buf, binary.BigEndian, v)
}

// Int32 writes a big-endian int32.
func (w *WireWriter) Int32(v int32) {
	if w.err != nil {
		return
	}
	w.err = binary.Write(&w.buf, binary.BigEndian, v)
}

// Uint32 writes a big-endian uint32. Used for fixed-size bitmasks like 1.21.2+ entity_teleport's Set<Relative> (4-byte BE u32, 9 bits used).
func (w *WireWriter) Uint32(v uint32) {
	if w.err != nil {
		return
	}
	w.err = binary.Write(&w.buf, binary.BigEndian, v)
}

// Int64 writes a big-endian int64.
func (w *WireWriter) Int64(v int64) {
	if w.err != nil {
		return
	}
	w.err = binary.Write(&w.buf, binary.BigEndian, v)
}

// Float32 writes a big-endian float32.
func (w *WireWriter) Float32(v float32) {
	if w.err != nil {
		return
	}
	w.err = binary.Write(&w.buf, binary.BigEndian, v)
}

// Float64 writes a big-endian float64.
func (w *WireWriter) Float64(v float64) {
	if w.err != nil {
		return
	}
	w.err = binary.Write(&w.buf, binary.BigEndian, v)
}

// VarInt writes a protocol VarInt (1-5 bytes, 7 bits per byte, MSB = continuation flag).
func (w *WireWriter) VarInt(v int32) {
	if w.err != nil {
		return
	}
	ux := uint32(v)
	for {
		b := uint8(ux & 0x7F)
		ux >>= 7
		if ux != 0 {
			b |= 0x80
		}
		w.err = w.buf.WriteByte(b)
		if w.err != nil {
			return
		}
		if ux == 0 {
			break
		}
	}
}

// VarLong writes a protocol VarLong (1-10 bytes).
func (w *WireWriter) VarLong(v int64) {
	if w.err != nil {
		return
	}
	ux := uint64(v)
	for {
		b := uint8(ux & 0x7F)
		ux >>= 7
		if ux != 0 {
			b |= 0x80
		}
		w.err = w.buf.WriteByte(b)
		if w.err != nil {
			return
		}
		if ux == 0 {
			break
		}
	}
}

// String writes a length-prefixed UTF-8 string.
// Format: VarInt(byteLength) + UTF-8 bytes. Max 32767 bytes.
func (w *WireWriter) String(v string) {
	if w.err != nil {
		return
	}
	if !utf8.ValidString(v) {
		w.err = errors.New("wire: invalid UTF-8 string")
		return
	}
	if len(v) > 32767 {
		w.err = errors.New("wire: string too long (max 32767)")
		return
	}
	w.VarInt(int32(len(v)))
	if w.err != nil {
		return
	}
	_, w.err = w.buf.WriteString(v)
}

// ByteArray writes a length-prefixed byte slice.
// Format: VarInt(len) + bytes.
func (w *WireWriter) ByteArray(v []byte) {
	if w.err != nil {
		return
	}
	w.VarInt(int32(len(v)))
	if w.err != nil {
		return
	}
	_, w.err = w.buf.Write(v)
}

// UUID writes a 16-byte UUID in big-endian order.
func (w *WireWriter) UUID(v [16]byte) {
	if w.err != nil {
		return
	}
	_, w.err = w.buf.Write(v[:])
}

// NBTTagEnd writes a single TAG_End byte (0x00) for empty NBT.
func (w *WireWriter) NBTTagEnd() {
	w.Byte(0)
}

// VarIntBuf returns the VarInt encoding of v as a byte slice.
// Useful for writing packet ID prefixes.
func VarIntBuf(v int32) []byte {
	var w WireWriter
	w.VarInt(v)
	if w.err != nil {
		return nil
	}
	return w.Bytes()
}

// BoolByte converts a bool to its wire byte representation.
func BoolByte(v bool) uint8 {
	if v {
		return 1
	}
	return 0
}

// MakePacket creates a complete packet by combining packet ID with payload.
// Returns the full packet bytes: VarInt(totalLength) + VarInt(packetID) + payload.
func MakePacket(packetID int32, payload []byte) []byte {
	var w WireWriter
	packetLen := len(payload) + len(VarIntBuf(packetID))
	w.VarInt(int32(packetLen))
	w.VarInt(packetID)
	w.buf.Write(payload)
	if w.err != nil {
		return nil
	}
	return w.Bytes()
}

// MaxVarIntLen is the maximum number of bytes a VarInt can occupy (5).
const MaxVarIntLen = 5

// VarIntSize returns the number of bytes a VarInt value will occupy.
func VarIntSize(v int32) int {
	ux := uint32(v)
	size := 1
	ux >>= 7
	for ux != 0 {
		size++
		ux >>= 7
	}
	return size
}

// WireReader reads protocol primitives from a byte stream.
// Accumulates errors: after the first error, all reads return zero values.
type WireReader struct {
	*bytes.Reader
	err error
}

// NewWireReader creates a WireReader from a byte slice.
func NewWireReader(data []byte) *WireReader {
	return &WireReader{Reader: bytes.NewReader(data)}
}

// Err returns the first error encountered during reading.
func (r *WireReader) Err() error { return r.err }

// Remaining returns the number of unread bytes.
func (r *WireReader) Remaining() int { return r.Len() }

// Byte reads a single unsigned byte.
func (r *WireReader) Byte() uint8 {
	if r.err != nil {
		return 0
	}
	b, err := r.ReadByte()
	if err != nil {
		r.err = err
	}
	return b
}

// Bool reads a boolean (0x00 or 0x01).
func (r *WireReader) Bool() bool {
	return r.Byte() != 0
}

// Int16 reads a big-endian int16.
func (r *WireReader) Int16() int16 {
	if r.err != nil {
		return 0
	}
	var v int16
	r.err = binary.Read(r, binary.BigEndian, &v)
	return v
}

// Uint16 reads a big-endian uint16.
func (r *WireReader) Uint16() uint16 {
	if r.err != nil {
		return 0
	}
	var v uint16
	r.err = binary.Read(r, binary.BigEndian, &v)
	return v
}

// Int32 reads a big-endian int32.
func (r *WireReader) Int32() int32 {
	if r.err != nil {
		return 0
	}
	var v int32
	r.err = binary.Read(r, binary.BigEndian, &v)
	return v
}

// Int64 reads a big-endian int64.
func (r *WireReader) Int64() int64 {
	if r.err != nil {
		return 0
	}
	var v int64
	r.err = binary.Read(r, binary.BigEndian, &v)
	return v
}

// Float32 reads a big-endian float32.
func (r *WireReader) Float32() float32 {
	if r.err != nil {
		return 0
	}
	var v float32
	r.err = binary.Read(r, binary.BigEndian, &v)
	return v
}

// Float64 reads a big-endian float64.
func (r *WireReader) Float64() float64 {
	if r.err != nil {
		return 0
	}
	var v float64
	r.err = binary.Read(r, binary.BigEndian, &v)
	return v
}

// VarInt reads a protocol VarInt (variable-length signed 32-bit integer).
func (r *WireReader) VarInt() int32 {
	if r.err != nil {
		return 0
	}
	var result uint32
	for i := 0; i < MaxVarIntLen; i++ {
		b, err := r.ReadByte()
		if err != nil {
			r.err = err
			return 0
		}
		result |= uint32(b&0x7F) << (i * 7)
		if b&0x80 == 0 {
			return int32(result)
		}
	}
	r.err = errors.New("wire: VarInt too long")
	return 0
}

// VarLong reads a protocol VarLong (variable-length signed 64-bit integer).
func (r *WireReader) VarLong() int64 {
	if r.err != nil {
		return 0
	}
	var result uint64
	for i := 0; i < 10; i++ {
		b, err := r.ReadByte()
		if err != nil {
			r.err = err
			return 0
		}
		result |= uint64(b&0x7F) << (i * 7)
		if b&0x80 == 0 {
			return int64(result)
		}
	}
	r.err = errors.New("wire: VarLong too long")
	return 0
}

// String reads a length-prefixed UTF-8 string.
func (r *WireReader) String() string {
	length := r.VarInt()
	if r.err != nil {
		return ""
	}
	if length < 0 || length > 32767 {
		r.err = errors.New("wire: invalid string length")
		return ""
	}
	if length == 0 {
		return ""
	}
	buf := make([]byte, length)
	if _, err := r.Read(buf); err != nil {
		r.err = err
		return ""
	}
	return string(buf)
}

// ByteArray reads a length-prefixed byte array.
func (r *WireReader) ByteArray() []byte {
	length := r.VarInt()
	if r.err != nil {
		return nil
	}
	if length < 0 {
		r.err = errors.New("wire: invalid byte array length")
		return nil
	}
	if length == 0 {
		return []byte{}
	}
	buf := make([]byte, length)
	if _, err := r.Read(buf); err != nil {
		r.err = err
		return nil
	}
	return buf
}

// UUID reads a 16-byte UUID.
func (r *WireReader) UUID() [16]byte {
	var uuid [16]byte
	if r.err != nil {
		return uuid
	}
	if _, err := r.Read(uuid[:]); err != nil {
		r.err = err
	}
	return uuid
}

// UUIDInto reads a 16-byte UUID into the provided array. Equivalent to UUID() but avoids the stack copy.
func (r *WireReader) UUIDInto(dst *[16]byte) {
	if r.err != nil {
		return
	}
	if _, err := r.Read(dst[:]); err != nil {
		r.err = err
	}
}

// ZigZag encoding helpers — encode signed integers as unsigned for bit-packing.
func ZigZag32(v int32) uint32   { return uint32(v<<1) ^ uint32(v>>31) }
func ZigZag64(v int64) uint64   { return uint64(v<<1) ^ uint64(v>>63) }
func UnZigZag32(v uint32) int32 { return int32(v>>1) ^ -int32(v&1) }
func UnZigZag64(v uint64) int64 { return int64(v>>1) ^ -int64(v&1) }

// Float64ToFixedPoint converts a float64 coordinate to a fixed-point int32 (multiply by 32, round to nearest).
func Float64ToFixedPoint(v float64) int32 {
	return int32(math.Round(v * 32))
}

// BlockPos is a block position in the world.
type BlockPos struct {
	X, Y, Z int32
}

// ChunkPos is a chunk position.
type ChunkPos struct {
	X, Z int32
}

// Slot represents an inventory item stack (sent in window_items, set_slot).
type Slot struct {
	Present bool
	ItemID  int32
	Count   uint8
	// TODO: NBT component data
}

// GameProfileProperty is a single property in a game_profile (used inside player_info_update's "add_player" action). Signature is empty in offline mode; Mojang-signed skins have a base64 signature.
type GameProfileProperty struct {
	Name      string
	Value     string
	Signature string
}

// Player info update (0x3F) action bitmask bits. Multiple actions can be OR'd together.
const (
	PlayerInfoActionAddPlayer      uint8 = 0x01
	PlayerInfoActionInitChat       uint8 = 0x02
	PlayerInfoActionUpdateGamemode uint8 = 0x04
	PlayerInfoActionUpdateListed   uint8 = 0x08
	PlayerInfoActionUpdateLatency  uint8 = 0x10
	PlayerInfoActionUpdateHat      uint8 = 0x40
)

// PlayerInfoEntry is a single per-player entry in a PlayerInfoUpdate (0x3F) packet. Which fields are written depends on the action bitmask.
type PlayerInfoEntry struct {
	UUID         [16]byte
	Name         string                // written if action & 0x01
	Properties   []GameProfileProperty // written if action & 0x01
	Gamemode     int32                 // written if action & 0x04 (0=survival, 1=creative, 2=adventure, 3=spectator)
	Listed       bool                  // written if action & 0x08
	Latency      int32                 // written if action & 0x10 (ping in ms)
	ShowHat      bool                  // written if action & 0x40 (1.21.6+)
	ListPriority int32                 // written if action & 0x80
}

// ProtocolVersion is the interface all version-specific protocol implementations
// must satisfy. Each Minecraft version has its own package (v772, v764, etc.).
type ProtocolVersion interface {
	Version() int32      // protocol version number (772)
	VersionName() string // Minecraft version string ("1.21.8")

	// Status phase
	WriteStatusResponse(version, protoVer int32, description, favicon string,
		playersOnline, maxPlayers int32) []byte

	// Login phase
	WriteLoginSuccess(uuid [16]byte, name string) []byte

	// Configuration phase
	WriteSelectKnownPacks() []byte
	WriteRegistries() [][]byte
	WriteFinishConfiguration() []byte

	// Play phase
	WriteLoginPlay(entityID int32, hardcore bool, gamemode uint8,
		dimensionNames []string, dimensionType string,
		seed int64, maxPlayers int32, viewDistance int32,
		simulationDistance int32, reducedDebugInfo bool,
		enableRespawnScreen bool, doLimitedCrafting bool,
		dimensionID int32, deathDimension string,
		deathLocation [8]byte, portalCooldown int32) []byte
	WritePosition(x, y, z float64, yaw, pitch float32, flags uint8, teleportID int32) []byte
	WriteAbilities(flags uint8) []byte
	WriteChunkData(chunk []byte, pos ChunkPos) []byte
	WriteUpdateLight(chunk []byte, pos ChunkPos) []byte
	WriteKeepAlive(id int64) []byte
	WriteUpdateTime(age, time int64) []byte
	WriteBlockUpdate(pos BlockPos, blockStateID int32) []byte
	WriteAckPlayerDigging(sequenceID int32) []byte
	WriteSetSlot(windowID uint8, stateID int32, slot int16, item Slot) []byte
	WriteHeldItemSlot(slot uint8) []byte
	WriteSpawnPosition(pos BlockPos, angle float32) []byte
	WriteHealth(health float32, food int32, saturation float32) []byte
	WriteSystemChat(message string) []byte
	WriteStartWaitingForChunks() []byte
	WriteSetCenterChunk(x, z int32) []byte
	WriteSetViewDistance(distance int32) []byte
	WriteSetSimulationDistance(distance int32) []byte
	WriteChunkBatchStart() []byte
	WriteChunkBatchFinished(batchSize int32) []byte
	WriteContainerItems(windowID uint8, stateID int32, slots []Slot, carried Slot) []byte

	// Phase 4: multiplayer
	WriteSpawnEntity(entityID int32, uuid [16]byte, typeID int32,
		x, y, z float64, pitch, yaw, headYaw int8) []byte
	// WriteRemoveEntities encodes a Remove Entities packet (0x46).
	WriteRemoveEntities(entityIDs []int32) []byte
	// WriteRelEntityMove encodes a delta position update (0x2E).
	WriteRelEntityMove(entityID int32, dx, dy, dz int16, onGround bool) []byte
	// WriteEntityMoveLook encodes a delta position + look update (0x2F).
	WriteEntityMoveLook(entityID int32, dx, dy, dz int16, yaw, pitch int8, onGround bool) []byte
	// WriteEntityLook encodes a look-only update (0x31).
	WriteEntityLook(entityID int32, yaw, pitch int8, onGround bool) []byte
	// WriteEntityHeadRotation encodes a head-only rotation (0x4C).
	WriteEntityHeadRotation(entityID int32, headYaw int8) []byte
	// WriteEntityEquipment encodes a Set Equipment packet (0x5F).
	// Equipment slot IDs: 0=main hand, 1=offhand, 2..5=armor, 6=body.
	WriteEntityEquipment(entityID int32, slot int8, item Slot) []byte
	// WritePlayerInfoUpdate encodes a player_info_update (0x3F). See protocol.PlayerInfoAction* constants.
	WritePlayerInfoUpdate(actions uint8, entries []PlayerInfoEntry) []byte
	// WritePlayerRemove encodes a player_remove (0x3E).
	WritePlayerRemove(uuids [][16]byte) []byte
	// WriteEntityTeleport encodes an absolute teleport (0x76). 1.21.2+ format.
	WriteEntityTeleport(entityID int32, x, y, z float64, yaw, pitch float32, onGround bool) []byte
	// WriteEntityMetadata encodes entity metadata entries (0x5C).
	WriteEntityMetadata(entityID int32, entries []MetadataEntry) []byte

	// Phase 5: survival mechanics

	// WriteDamageEvent encodes a Damage Event packet (0x19). sourceTypeID is a
	// damage_type registry id (see v772 DamageType* constants). sourceCauseID /
	// sourceDirectID are entity ids or -1 for "none" (vanilla OptionalInt sentinel).
	// hasSourcePos controls the trailing Optional<Vec3> source position.
	WriteDamageEvent(entityID, sourceTypeID, sourceCauseID, sourceDirectID int32,
		hasSourcePos bool, sourceX, sourceY, sourceZ float64) []byte
	// WriteEntityStatus encodes an Entity Event packet (0x1E). entityId is i32 (NOT
	// VarInt); status is an i8 op code (e.g. 3 = death animation for living entities).
	WriteEntityStatus(entityID int32, status int8) []byte
	// WriteHurtAnimation encodes a Hurt Animation packet (0x24) — the red damage
	// flash + tilt direction derived from yaw.
	WriteHurtAnimation(entityID int32, yaw float32) []byte
	// WriteRespawn encodes a Respawn packet (0x4B). Used both for dimension change
	// and post-death respawn. SpawnInfo mirrors the Login Play CommonPlayerSpawnInfo
	// block; copyMetadata is the trailing u8 (0 = fresh, 1 = keep metadata on respawn).
	WriteRespawn(dimensionType int32, dimensionName string, hashedSeed int64,
		gamemode, prevGamemode uint8, isDebug, isFlat bool,
		hasDeath bool, deathDimensionName string, deathPos BlockPos,
		portalCooldown, seaLevel int32, copyMetadata bool) []byte

	// Packet IDs (for dispatch in FSM)
	PacketIDs() PacketIDMap
}

// PacketIDMap maps packet names to their numeric IDs.
type PacketIDMap struct {
	Clientbound map[string]int32
	Serverbound map[string]int32
}

// MetadataType is the entity metadata value type tag.
type MetadataType uint8

const (
	MetadataTypeByte     MetadataType = 0  // i8
	MetadataTypeVarInt   MetadataType = 1  // varint
	MetadataTypeVarLong  MetadataType = 2  // varlong
	MetadataTypeFloat    MetadataType = 3  // f32
	MetadataTypeString   MetadataType = 4  // string
	MetadataTypeChat     MetadataType = 5  // chat component (NBT)
	MetadataTypeOptChat  MetadataType = 6  // optional chat
	MetadataTypeSlot     MetadataType = 7  // item stack
	MetadataTypeBoolean  MetadataType = 8  // bool
	MetadataTypeRot      MetadataType = 9  // 3 floats (pitch, yaw, roll)
	MetadataTypePosition MetadataType = 10 // block pos
	MetadataTypeOptPos   MetadataType = 11 // optional block pos
	MetadataTypeDir      MetadataType = 12 // 3 floats (velocity)
	MetadataTypeOptUUID  MetadataType = 13 // optional uuid
	MetadataTypeBlockSt  MetadataType = 14 // block state (varint)
	MetadataTypeOptBlock MetadataType = 15 // optional block state
	MetadataTypeNBT      MetadataType = 16 // nbt
	MetadataTypeParticle MetadataType = 17 // particle
	MetadataTypePose     MetadataType = 21 // pose (varint)
)

// MetadataEntry is a single (key, type, value) tuple written to an entity_metadata packet.
type MetadataEntry struct {
	Key   uint8
	Type  MetadataType
	Value any // byte, bool, int32, int64, float32, string, or protocol.BlockPos
}

func MetadataByte(key uint8, v int8) MetadataEntry {
	return MetadataEntry{Key: key, Type: MetadataTypeByte, Value: v}
}

func MetadataBool(key uint8, v bool) MetadataEntry {
	return MetadataEntry{Key: key, Type: MetadataTypeBoolean, Value: v}
}

func MetadataVarInt(key uint8, v int32) MetadataEntry {
	return MetadataEntry{Key: key, Type: MetadataTypeVarInt, Value: v}
}

func MetadataVarLong(key uint8, v int64) MetadataEntry {
	return MetadataEntry{Key: key, Type: MetadataTypeVarLong, Value: v}
}

func MetadataFloat(key uint8, v float32) MetadataEntry {
	return MetadataEntry{Key: key, Type: MetadataTypeFloat, Value: v}
}
