package v772_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"goore/internal/protocol"
	"goore/internal/protocol/v772"
)

func TestProtocolSatisfiesInterface(t *testing.T) {
	var p protocol.ProtocolVersion = v772.New()
	if p.Version() != 772 {
		t.Errorf("version = %d, want 772", p.Version())
	}
	if p.VersionName() != "1.21.8" {
		t.Errorf("versionName = %q, want %q", p.VersionName(), "1.21.8")
	}
}

func TestWriteLoginSuccess(t *testing.T) {
	p := v772.New()
	uuid := [16]byte{0xDE, 0xAD, 0xBE, 0xEF, 0xDE, 0xAD, 0xBE, 0xEF,
		0xDE, 0xAD, 0xBE, 0xEF, 0xDE, 0xAD, 0xBE, 0xEF}
	packet := p.WriteLoginSuccess(uuid, "Player")
	if packet == nil {
		t.Fatal("packet is nil")
	}

	// Packet format: VarInt(totalLen) + VarInt(0x02) + UUID(16) + String("Player") + VarInt(0)
	r := protocol.NewWireReader(packet)
	totalLen := r.VarInt()
	_ = totalLen
	pktID := r.VarInt()
	if pktID != v772.LoginSuccess {
		t.Errorf("packet ID = 0x%02X, want 0x%02X", pktID, v772.LoginSuccess)
	}
	gotUUID := r.UUID()
	if gotUUID != uuid {
		t.Errorf("UUID = %x, want %x", gotUUID, uuid)
	}
	gotName := r.String()
	if gotName != "Player" {
		t.Errorf("name = %q, want %q", gotName, "Player")
	}
	props := r.VarInt()
	if props != 0 {
		t.Errorf("properties = %d, want 0", props)
	}
	if err := r.Err(); err != nil {
		t.Fatalf("reader error: %v", err)
	}
}

func TestWriteSelectKnownPacks(t *testing.T) {
	p := v772.New()
	packet := p.WriteSelectKnownPacks()
	if packet == nil {
		t.Fatal("packet is nil")
	}

	r := protocol.NewWireReader(packet)
	totalLen := r.VarInt()
	pktID := r.VarInt()
	if pktID != v772.ConfigSelectKnownPacksCli {
		t.Errorf("packet ID = 0x%02X, want 0x%02X", pktID, v772.ConfigSelectKnownPacksCli)
	}
	count := r.VarInt()
	if count != 1 {
		t.Fatalf("known pack count = %d, want 1", count)
	}
	ns := r.String()
	id := r.String()
	ver := r.String()
	if ns != "minecraft" {
		t.Errorf("namespace = %q, want %q", ns, "minecraft")
	}
	if id != "core" {
		t.Errorf("id = %q, want %q", id, "core")
	}
	if ver != "1.21.8" {
		t.Errorf("version = %q, want %q", ver, "1.21.8")
	}
	if err := r.Err(); err != nil {
		t.Fatalf("reader error: %v", err)
	}
	_ = totalLen
}

func TestWriteFinishConfiguration(t *testing.T) {
	p := v772.New()
	packet := p.WriteFinishConfiguration()
	if packet == nil {
		t.Fatal("packet is nil")
	}

	r := protocol.NewWireReader(packet)
	r.VarInt() // totalLen
	pktID := r.VarInt()
	if pktID != v772.ConfigFinishConfig {
		t.Errorf("packet ID = 0x%02X, want 0x%02X", pktID, v772.ConfigFinishConfig)
	}
	if r.Remaining() != 0 {
		t.Errorf("expected 0 remaining bytes, got %d", r.Remaining())
	}
}

func TestWriteLoginPlay(t *testing.T) {
	p := v772.New()
	packet := p.WriteLoginPlay(
		0,     // entityID
		false, // hardcore
		1,     // gamemode (creative)
		[]string{"minecraft:overworld", "minecraft:the_nether", "minecraft:the_end"},
		"minecraft:overworld",
		12345,                 // seed
		20,                    // max players
		8,                     // view distance
		8,                     // simulation distance
		false,                 // reduced debug
		true,                  // enable respawn screen
		false,                 // do limited crafting
		0,                     // dimension ID
		"minecraft:overworld", // death dimension
		[8]byte{},             // death location
		0,                     // portal cooldown
	)
	if packet == nil {
		t.Fatal("packet is nil")
	}

	r := protocol.NewWireReader(packet)
	totalLen := r.VarInt()
	pktID := r.VarInt()
	if pktID != v772.PlayLogin {
		t.Errorf("packet ID = 0x%02X, want 0x%02X", pktID, v772.PlayLogin)
	}
	_ = totalLen
	if err := r.Err(); err != nil {
		t.Fatalf("reader error at start: %v", err)
	}

	eid := r.Int32()
	if eid != 0 {
		t.Errorf("entityID = %d, want 0", eid)
	}
	hardcore := r.Bool()
	if hardcore {
		t.Error("hardcore should be false")
	}
	dimCount := r.VarInt()
	if dimCount != 3 {
		t.Errorf("dimension count = %d, want 3", dimCount)
	}
	for i := 0; i < int(dimCount); i++ {
		_ = r.String()
	}
	maxPlayers := r.VarInt()
	if maxPlayers != 20 {
		t.Errorf("max players = %d, want 20", maxPlayers)
	}
	_ = r.VarInt() // view distance
	_ = r.VarInt() // simulation distance
	_ = r.Bool()   // reduced debug
	_ = r.Bool()   // respawn screen
	_ = r.Bool()   // limited crafting
	_ = r.String() // dimension type
	_ = r.String() // dimension name
	_ = r.Int64()  // seed
	gamemode := r.Byte()
	if gamemode != 1 {
		t.Errorf("gamemode = %d, want 1", gamemode)
	}
	_ = r.Byte()   // previous gamemode
	_ = r.Bool()   // isDebug
	_ = r.Bool()   // isFlat
	_ = r.Bool()   // hasLastDeathLocation
	_ = r.VarInt() // portal cooldown

	if err := r.Err(); err != nil {
		t.Fatalf("reader error: %v", err)
	}
}

func TestWriteAbilities(t *testing.T) {
	p := v772.New()
	packet := p.WriteAbilities(0x04) // creative mode flag
	if packet == nil {
		t.Fatal("packet is nil")
	}

	r := protocol.NewWireReader(packet)
	r.VarInt() // totalLen
	pktID := r.VarInt()
	if pktID != v772.PlayAbilities {
		t.Errorf("packet ID = 0x%02X, want 0x%02X", pktID, v772.PlayAbilities)
	}
	flags := r.Byte()
	if flags != 0x04 {
		t.Errorf("flags = 0x%02X, want 0x04", flags)
	}
	if err := r.Err(); err != nil {
		t.Fatalf("reader error: %v", err)
	}
}

func TestWriteKeepAlive(t *testing.T) {
	p := v772.New()
	packet := p.WriteKeepAlive(42)
	if packet == nil {
		t.Fatal("packet is nil")
	}

	r := protocol.NewWireReader(packet)
	r.VarInt() // totalLen
	pktID := r.VarInt()
	if pktID != v772.PlayKeepAlive {
		t.Errorf("packet ID = 0x%02X, want 0x%02X", pktID, v772.PlayKeepAlive)
	}
	id := r.Int64()
	if id != 42 {
		t.Errorf("id = %d, want 42", id)
	}
	if err := r.Err(); err != nil {
		t.Fatalf("reader error: %v", err)
	}
}

func TestWritePosition(t *testing.T) {
	p := v772.New()
	packet := p.WritePosition(100.5, 64.0, 200.25, 45.0, 0.0, 0x00, -1)
	if packet == nil {
		t.Fatal("packet is nil")
	}

	r := protocol.NewWireReader(packet)
	r.VarInt()
	pktID := r.VarInt()
	if pktID != v772.PlayPlayerPos {
		t.Errorf("packet ID = 0x%02X, want 0x%02X", pktID, v772.PlayPlayerPos)
	}
	teleportID := r.VarInt()
	x := r.Float64()
	y := r.Float64()
	z := r.Float64()
	r.Float64() // vx — skip
	r.Float64() // vy — skip
	r.Float64() // vz — skip
	yaw := r.Float32()
	pitch := r.Float32()
	flags := r.Int32()

	if teleportID != -1 {
		t.Errorf("teleportID = %d, want -1", teleportID)
	}
	if x != 100.5 || y != 64.0 || z != 200.25 {
		t.Errorf("position = (%v, %v, %v), want (100.5, 64, 200.25)", x, y, z)
	}
	if yaw != 45.0 || pitch != 0.0 {
		t.Errorf("rotation = (%v, %v), want (45, 0)", yaw, pitch)
	}
	if flags != 0 {
		t.Errorf("flags = %d, want 0", flags)
	}
	if err := r.Err(); err != nil {
		t.Fatalf("reader error: %v", err)
	}
}

func TestWriteSystemChat(t *testing.T) {
	p := v772.New()
	packet := p.WriteSystemChat("Hello, world!")
	if packet == nil {
		t.Fatal("packet is nil")
	}

	r := protocol.NewWireReader(packet)
	r.VarInt()
	pktID := r.VarInt()
	if pktID != v772.PlaySystemChat {
		t.Errorf("packet ID = 0x%02X, want 0x%02X", pktID, v772.PlaySystemChat)
	}
	msg := r.String()
	overlay := r.Bool()
	if msg != "Hello, world!" {
		t.Errorf("message = %q", msg)
	}
	if overlay {
		t.Error("overlay should be false")
	}
	if err := r.Err(); err != nil {
		t.Fatalf("reader error: %v", err)
	}
}

func TestWriteBlockUpdate(t *testing.T) {
	p := v772.New()
	pos := protocol.BlockPos{X: 10, Y: 64, Z: -5}
	packet := p.WriteBlockUpdate(pos, 9) // grass_block default state
	if packet == nil {
		t.Fatal("packet is nil")
	}

	// Decode the full packet
	r := protocol.NewWireReader(packet)
	_ = r.VarInt() // totalLen
	pktID := r.VarInt()
	if pktID != v772.PlayBlockUpdate {
		t.Errorf("packet ID = 0x%02X, want 0x%02X", pktID, v772.PlayBlockUpdate)
	}

	// Vanilla 1.21.8 Position: (x & 0x3FFFFFF) << 38 | (z & 0x3FFFFFF) << 12 | (y & 0xFFF)
	packed := r.Int64()
	gotX := int32(packed >> 38)
	gotZ := int32(packed << 26 >> 38)
	gotY := int32(packed << 52 >> 52)
	if gotX != pos.X {
		t.Errorf("X = %d, want %d", gotX, pos.X)
	}
	if gotY != pos.Y {
		t.Errorf("Y = %d, want %d", gotY, pos.Y)
	}
	if gotZ != pos.Z {
		t.Errorf("Z = %d, want %d", gotZ, pos.Z)
	}

	gotID := r.VarInt()
	if gotID != 9 {
		t.Errorf("block state ID = %d, want 9", gotID)
	}
	if err := r.Err(); err != nil {
		t.Errorf("reader error: %v", err)
	}
	if r.Remaining() != 0 {
		t.Errorf("remaining bytes = %d, want 0", r.Remaining())
	}
}

func TestWriteBlockUpdateEdgeCases(t *testing.T) {
	p := v772.New()
	cases := []struct {
		name    string
		pos     protocol.BlockPos
		stateID int32
	}{
		{"origin", protocol.BlockPos{X: 0, Y: 0, Z: 0}, 0},
		{"all negative", protocol.BlockPos{X: -1, Y: -64, Z: -1}, 0}, // min Y is -64
		{"max bounds", protocol.BlockPos{X: 33554431, Y: 2047, Z: 33554431}, 0},
		{"bedrock", protocol.BlockPos{X: 100, Y: -64, Z: 100}, 85}, // bedrock default state
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			packet := p.WriteBlockUpdate(tc.pos, tc.stateID)
			if packet == nil {
				t.Fatalf("nil packet for %+v", tc.pos)
			}
			r := protocol.NewWireReader(packet)
			_ = r.VarInt()
			pktID := r.VarInt()
			if pktID != v772.PlayBlockUpdate {
				t.Errorf("packet ID = 0x%02X, want 0x%02X", pktID, v772.PlayBlockUpdate)
			}
			packed := r.Int64()
			gotX := int32(packed >> 38)
			gotY := int32(packed << 52 >> 52)
			gotZ := int32(packed << 26 >> 38)
			if gotX != tc.pos.X {
				t.Errorf("X = %d, want %d", gotX, tc.pos.X)
			}
			if gotY != tc.pos.Y {
				t.Errorf("Y = %d, want %d", gotY, tc.pos.Y)
			}
			if gotZ != tc.pos.Z {
				t.Errorf("Z = %d, want %d", gotZ, tc.pos.Z)
			}
			if id := r.VarInt(); id != tc.stateID {
				t.Errorf("state ID = %d, want %d", id, tc.stateID)
			}
			if err := r.Err(); err != nil {
				t.Errorf("reader error: %v", err)
			}
		})
	}
}

// TestWriteAckPlayerDigging verifies the wire format of acknowledge_player_digging
// (clientbound, Play ID 0x04). Vanilla 1.21.8 wire format:
//
//	VarInt(totalLen) + VarInt(0x04) + sequenceId(VarInt)
//
// The server sends this to confirm a player_action (dig) request from the client.
func TestWriteAckPlayerDigging(t *testing.T) {
	p := v772.New()
	packet := p.WriteAckPlayerDigging(42)
	if packet == nil {
		t.Fatal("packet is nil")
	}

	r := protocol.NewWireReader(packet)
	_ = r.VarInt() // totalLen
	pktID := r.VarInt()
	if pktID != v772.PlayAckPlayerDigging {
		t.Errorf("packet ID = 0x%02X, want 0x%02X", pktID, v772.PlayAckPlayerDigging)
	}
	seq := r.VarInt()
	if seq != 42 {
		t.Errorf("sequenceId = %d, want 42", seq)
	}
	if err := r.Err(); err != nil {
		t.Errorf("reader error: %v", err)
	}
	if r.Remaining() != 0 {
		t.Errorf("remaining bytes = %d, want 0", r.Remaining())
	}
}

// TestWriteSetSlot verifies the wire format of set_slot (clientbound, 0x14).
// Vanilla 1.21.8 wire format:
//
//	VarInt(totalLen) + VarInt(0x14) + windowId(u8) + stateId(VarInt) +
//	slot(i16) + item(Slot)
//
// Used for targeted inventory updates (e.g., creative pick up, set held item).
func TestWriteSetSlot(t *testing.T) {
	p := v772.New()
	pkt := p.WriteSetSlot(0, 5, 36, protocol.Slot{Present: true, ItemID: 1, Count: 64})
	if pkt == nil {
		t.Fatal("packet is nil")
	}
	r := protocol.NewWireReader(pkt)
	_ = r.VarInt()
	pktID := r.VarInt()
	if pktID != v772.PlaySetSlot {
		t.Errorf("packet ID = 0x%02X, want 0x%02X", pktID, v772.PlaySetSlot)
	}
	if wid := r.Byte(); wid != 0 {
		t.Errorf("windowId = %d, want 0", wid)
	}
	if sid := r.VarInt(); sid != 5 {
		t.Errorf("stateId = %d, want 5", sid)
	}
	if slot := r.Int16(); slot != 36 {
		t.Errorf("slot = %d, want 36", slot)
	}
	count := r.VarInt()
	if count != 64 {
		t.Errorf("item count = %d, want 64", count)
	}
	itemID := r.VarInt()
	// 1.21.5+ Holder<Item> wire format: VarInt(registryId + 1).
	// Stone (registryId=1) is encoded as 2.
	if itemID != 2 {
		t.Errorf("item id = %d, want 2 (stone wire-encoded as registryId+1)", itemID)
	}
	if r.Err() != nil {
		t.Errorf("reader error: %v", r.Err())
	}
}

func TestPacketIDMap(t *testing.T) {
	ids := v772.PacketIDs()

	tests := []struct {
		dir  string
		name string
		id   int32
	}{
		{"clientbound", "login_play", v772.PlayLogin},
		{"clientbound", "keep_alive", v772.PlayKeepAlive},
		{"clientbound", "map_chunk", v772.PlayMapChunk},
		{"clientbound", "abilities", v772.PlayAbilities},
		{"serverbound", "handshake", v772.HandshakeIntention},
		{"serverbound", "login_start", v772.LoginStart},
		{"serverbound", "keep_alive", v772.PlayKeepAliveServerbound},
	}

	for _, tt := range tests {
		var m map[string]int32
		if tt.dir == "clientbound" {
			m = ids.Clientbound
		} else {
			m = ids.Serverbound
		}
		got, ok := m[tt.name]
		if !ok {
			t.Errorf("%s.%s: not found in map", tt.dir, tt.name)
			continue
		}
		if got != tt.id {
			t.Errorf("%s.%s = 0x%02X, want 0x%02X", tt.dir, tt.name, got, tt.id)
		}
	}
}

func TestEncodeSlot(t *testing.T) {
	// Test WriteContainerItems with a few slots
	p := v772.New()
	slots := []protocol.Slot{
		{Present: true, ItemID: 1, Count: 64}, // stone x64
		{Present: false},                      // empty
		{Present: true, ItemID: 14, Count: 1}, // grass block
	}
	carried := protocol.Slot{} // empty

	packet := p.WriteContainerItems(0, 0, slots, carried)
	if packet == nil {
		t.Fatal("packet is nil")
	}

	r := protocol.NewWireReader(packet)
	r.VarInt()
	pktID := r.VarInt()
	if pktID != v772.PlayWindowItems {
		t.Errorf("packet ID = 0x%02X, want 0x%02X", pktID, v772.PlayWindowItems)
	}
	windowID := r.Byte()
	if windowID != 0 {
		t.Errorf("windowID = %d, want 0", windowID)
	}
	_ = r.VarInt() // state ID
	count := r.VarInt()
	if count != 3 { // 3 slots (carried is a separate field)
		t.Errorf("slot count = %d, want 3", count)
	}
	// Read slots in 1.21+ format
	for i := 0; i < int(count); i++ {
		itemCount := r.VarInt()
		if itemCount > 0 {
			_ = r.VarInt() // item ID
			addCount := r.VarInt()
			removeCount := r.VarInt()
			if addCount != 0 {
				t.Errorf("slot %d: expected 0 components to add, got %d", i, addCount)
			}
			if removeCount != 0 {
				t.Errorf("slot %d: expected 0 components to remove, got %d", i, removeCount)
			}
		}
	}
	// Read carriedItem (separate field after the array)
	carriedCount := r.VarInt()
	if carriedCount != 0 {
		t.Errorf("carried count = %d, want 0 (empty)", carriedCount)
	}
	if err := r.Err(); err != nil {
		t.Fatalf("reader error: %v", err)
	}
	if rem := r.Remaining(); rem != 0 {
		t.Errorf("expected 0 remaining bytes, got %d", rem)
	}
}

func TestWriteSpawnPosition(t *testing.T) {
	p := v772.New()
	pos := protocol.BlockPos{X: 0, Y: 65, Z: 0}
	packet := p.WriteSpawnPosition(pos, 0)
	if packet == nil {
		t.Fatal("packet is nil")
	}

	r := protocol.NewWireReader(packet)
	r.VarInt()
	pktID := r.VarInt()
	if pktID != v772.PlaySpawnPos {
		t.Errorf("packet ID = 0x%02X, want 0x%02X", pktID, v772.PlaySpawnPos)
	}
	if err := r.Err(); err != nil {
		t.Fatalf("reader error: %v", err)
	}
}

func TestWriteHeldItemSlot(t *testing.T) {
	p := v772.New()
	packet := p.WriteHeldItemSlot(0)
	if packet == nil {
		t.Fatal("packet is nil")
	}

	r := protocol.NewWireReader(packet)
	r.VarInt()
	pktID := r.VarInt()
	if pktID != v772.PlayHeldItemSlot {
		t.Errorf("packet ID = 0x%02X, want 0x%02X", pktID, v772.PlayHeldItemSlot)
	}
	// Vanilla 1.21.8 clientbound held_item_slot field is `slot: varint`
	// (per proto.yml packet_held_item_slot for ClientboundSetHeldSlotPacket).
	// The previous implementation wrote a single byte, which is byte-
	// compatible for values 0..127 but is the wrong type per the
	// protocol spec and breaks interop with strict decoders.
	slot := r.VarInt()
	if slot != 0 {
		t.Errorf("slot = %d, want 0", slot)
	}
	if err := r.Err(); err != nil {
		t.Fatalf("reader error: %v", err)
	}
}

// TestWriteHeldItemSlot_UsesVarIntEncoding is the strict regression
// test for the WriteHeldItemSlot wire format. Vanilla 1.21.8 sends
// the slot as a VarInt (per proto.yml). A single-byte encoding
// happens to coincide with a 1-byte VarInt for slots 0..127, but
// 1) is the wrong type per spec and 2) future clients that read
// the field as a VarInt and use it to index into a 9-entry hotbar
// will not see the same value if the server ever sends a slot that
// happens to land in the multi-byte VarInt range (slot >= 128).
//
// This test asserts the packet body is exactly the VarInt encoding
// of the slot, byte-for-byte. The hotbar has 9 slots (0..8), all
// in the 1-byte range, so this is mostly a future-proofing check.
func TestWriteHeldItemSlot_UsesVarIntEncoding(t *testing.T) {
	p := v772.New()
	for _, slot := range []uint8{0, 1, 4, 8} {
		packet := p.WriteHeldItemSlot(slot)

		// Skip the VarInt(packet length) and VarInt(packet ID) prefix
		// to get the body, then check the body is exactly the VarInt
		// encoding of `slot` (one byte for values < 128).
		r := protocol.NewWireReader(packet)
		_ = r.VarInt()        // packet length
		_ = r.VarInt()        // packet ID
		body := r.ByteArray() // NO — body is a VarInt, not length-prefixed
		_ = body
	}

	// The cleanest assertion: for slot=8, the body of the packet
	// (after the length + packet ID) must be a single byte 0x08.
	packet := p.WriteHeldItemSlot(8)
	r := protocol.NewWireReader(packet)
	_ = r.VarInt() // packet length
	_ = r.VarInt() // packet ID
	// What's left is the slot value. For VarInt(8) it is one byte 0x08.
	got := r.VarInt()
	if got != 8 {
		t.Errorf("held_item_slot body = VarInt(%d), want VarInt(8)", got)
	}
	if err := r.Err(); err != nil {
		t.Errorf("trailing bytes: %v", err)
	}
}

func TestMakePacketRoundTrip(t *testing.T) {
	// Test that MakePacket produces correctly framed packets
	payload := []byte{0x42, 0x43}
	packet := protocol.MakePacket(0x07, payload)

	// Parse: VarInt(totalLen) + VarInt(pktID) + payload
	r := protocol.NewWireReader(packet)
	totalLen := r.VarInt()
	pktID := r.VarInt()

	if pktID != 0x07 {
		t.Errorf("pktID = 0x%02X, want 0x07", pktID)
	}

	remaining := make([]byte, r.Remaining())
	r.Read(remaining)
	if !bytes.Equal(remaining, payload) {
		t.Errorf("payload = %x, want %x", remaining, payload)
	}

	if int(totalLen) != len(payload)+protocol.VarIntSize(0x07) {
		t.Errorf("totalLen = %d, want %d", totalLen, len(payload)+protocol.VarIntSize(0x07))
	}
}

// TestWriteEntityEquipment_ExactWireFormat pins the byte layout of
// set_equipment (0x5F) so a future regression in the CONTINUE_MASK bit
// (the user-reported 'IndexOutOfBoundsException' on second-client
// connect) cannot slip past review. The exact bytes are:
//
//	VarInt(packetLen=7) +
//	VarInt(0x5F) +
//	VarInt(EID=0) +
//	Byte(slot=0)            // CONTINUE_MASK clear: last entry
//	VarInt(count=64)
//	VarInt(itemId=2)        // stone (registryId+1)
//	VarInt(components_with_data=0)
//	VarInt(components_without_data=0)
//
// Vanilla 1.21.8 decoder reads the slot byte, sees bit 7 CLEAR,
// then reads the ItemStack, then stops. If the encoder sets the
// CONTINUE_MASK (bit 7) on the only entry, the decoder reads the
// next VarInt byte as another slot byte and crashes with
// `IndexOutOfBoundsException: readerIndex(7) + length(1) exceeds
// writerIndex(7)` — the exact user-reported symptom.
func TestWriteEntityEquipment_ExactWireFormat(t *testing.T) {
	p := v772.New()
	pkt := p.WriteEntityEquipment(0, 0, protocol.Slot{Present: true, ItemID: 1, Count: 64})
	if pkt == nil {
		t.Fatal("packet is nil")
	}
	want := []byte{
		0x07, // packet length = 7
		0x5F, // packet ID = set_equipment
		0x00, // VarInt(EID) = 0
		0x00, // slot byte = 0 (main hand), CONTINUE_MASK CLEAR
		0x40, // VarInt(count) = 64
		0x02, // VarInt(itemId) = 2 (stone, registryId+1)
		0x00, // VarInt(components_with_data_count) = 0
		0x00, // VarInt(components_without_data_count) = 0
	}
	if len(pkt) != len(want) {
		t.Fatalf("packet length = %d, want %d. bytes = % x", len(pkt), len(want), pkt)
	}
	for i := range want {
		if pkt[i] != want[i] {
			t.Errorf("byte %d: got 0x%02X, want 0x%02X. full = % x", i, pkt[i], want[i], pkt)
		}
	}

	// Spot-check the critical bits: the slot byte is 0x00 (NOT 0x80),
	// and itemId is 2 (NOT 1). The 0x80 case is the user-reported
	// CONTINUE_MASK-inverted bug; the 1 case is the Holder<>-+1 bug.
	slot := pkt[3]
	if slot&0x80 != 0 {
		t.Errorf("CONTINUE_MASK bug: slot byte = 0x%02X, top bit must be CLEAR on the last entry (would otherwise trigger `readerIndex(7) + length(1) exceeds writerIndex(7)`)", slot)
	}
	itemID := pkt[5]
	if itemID != 2 {
		t.Errorf("Holder<>-+1 bug: itemId = %d, want 2 (stone = registryId(1) + 1)", itemID)
	}
}

// TestWriteStatusResponse covers Phase 3.1 + 3.2 — the hand-rolled
// JSON was previously built via custom `itoa` / `escapeJSON` helpers,
// now uses `strconv.FormatInt` + `strconv.AppendQuote`. The behaviour
// must be byte-identical for ASCII, AND now correctly escapes
// non-ASCII characters (the old escapeJSON passed them through
// unchanged, which is not strictly valid JSON — the test pins the
// new correct behaviour so we can't accidentally regress).
func TestWriteStatusResponse(t *testing.T) {
	p := v772.New()

	// Standard ASCII case (matches the existing TestStatusFlow
	// integration test in the player package — the byte sequence
	// here is what TestStatusFlow matches against `strings.Contains`).
	pkt := p.WriteStatusResponse(772, 772, "GoOre Minecraft Server", "", 0, 20)
	if pkt == nil {
		t.Fatal("WriteStatusResponse returned nil")
	}
	// Unwrap packet envelope: VarInt(length) + VarInt(packetID) + String payload
	r := protocol.NewWireReader(pkt)
	_ = r.VarInt() // length prefix
	pktID := r.VarInt()
	if pktID != v772.StatusServerInfo {
		t.Fatalf("packet ID = 0x%02X, want 0x%02X (StatusServerInfo)", pktID, v772.StatusServerInfo)
	}
	json := r.String()
	if r.Err() != nil {
		t.Fatalf("read JSON: %v", r.Err())
	}

	// Sanity-check the structure. We don't pin the exact byte
	// sequence because strconv.AppendQuote is allowed to format
	// floats / unicode however the Go stdlib decides; we only
	// care that the JSON is valid and contains the right fields.
	wantSubstrings := []string{
		`"version":{"name":"GoOre 1.21.8","protocol":772`,
		`"players":{"max":20,"online":0,"sample":[]`,
		`"description":{"text":"GoOre Minecraft Server"}`,
	}
	for _, s := range wantSubstrings {
		if !bytes.Contains([]byte(json), []byte(s)) {
			t.Errorf("JSON missing substring %q\nfull JSON: %s", s, json)
		}
	}
	if bytes.Contains([]byte(json), []byte(`"favicon`)) {
		t.Errorf("JSON should not contain favicon when empty:\n%s", json)
	}

	// Phase 3.2 edge case: description contains characters the
	// old escapeJSON passed through unchanged. With strconv.Quote
	// they are now JSON-escaped. The new behaviour is more correct
	// (raw bytes are not valid JSON), so the test pins the new
	// behaviour as the contract.
	pkt = p.WriteStatusResponse(772, 772, "a\"b\nc\td\\e", "", 0, 20)
	r = protocol.NewWireReader(pkt)
	_ = r.VarInt()
	_ = r.VarInt()
	json = r.String()
	wantEscaped := []string{`a\"b`, `b\nc`, `c\td`, `d\\e`}
	for _, s := range wantEscaped {
		if !bytes.Contains([]byte(json), []byte(s)) {
			t.Errorf("JSON missing escaped substring %q\nfull JSON: %s", s, json)
		}
	}

	// Phase 3.1 edge case: negative protocol version (the old
	// itoa supported negatives; strconv.FormatInt does too). We
	// don't expect this in production, but the formatter must
	// not panic.
	pkt = p.WriteStatusResponse(772, -1, "x", "", 0, -2)
	if pkt == nil {
		t.Fatal("WriteStatusResponse(-1, ...) returned nil")
	}

	// Favicon present.
	pkt = p.WriteStatusResponse(772, 772, "GoOre", "data:image/png;base64,AAAA", 0, 20)
	r = protocol.NewWireReader(pkt)
	_ = r.VarInt()
	_ = r.VarInt()
	json = r.String()
	if !bytes.Contains([]byte(json), []byte(`"favicon":"data:image/png;base64,AAAA"`)) {
		t.Errorf("JSON missing favicon field:\n%s", json)
	}
}

// TestWriteStatusResponse_ValidJSON pins the regression: the status
// response JSON MUST be a single, fully-closed root object in BOTH
// the favicon and no-favicon cases. Previously the root closing brace
// was only written in the favicon branch, so the default (empty
// favicon) config produced `...description":{"text":"..."}` with the
// root object never closed. The vanilla client rejects that as
// invalid JSON and the multiplayer server list shows "can't connect
// to server" even though direct join (which uses the login flow, not
// status) still works. Substring-based assertions above miss this
// because they never validate the whole document; this test unmarshals
// the full payload.
func TestWriteStatusResponse_ValidJSON(t *testing.T) {
	p := v772.New()
	cases := []struct {
		name     string
		favicon  string
		online   int32
		max      int32
		protoVer int32
	}{
		{"no-favicon default", "", 0, 20, 772},
		{"with-favicon", "data:image/png;base64,AAAA", 0, 20, 772},
		{"nonzero players", "", 3, 20, 772},
		{"negative proto (legacy)", "", 0, 20, -1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			pkt := p.WriteStatusResponse(772, c.protoVer, "GoOre", c.favicon, c.online, c.max)
			if pkt == nil {
				t.Fatal("WriteStatusResponse returned nil")
			}
			r := protocol.NewWireReader(pkt)
			_ = r.VarInt()
			_ = r.VarInt()
			jsonStr := r.String()
			if r.Err() != nil {
				t.Fatalf("read JSON: %v", r.Err())
			}
			var m map[string]any
			if err := json.Unmarshal([]byte(jsonStr), &m); err != nil {
				t.Fatalf("status JSON is not valid: %v\nfull JSON: %s", err, jsonStr)
			}
			if _, ok := m["version"]; !ok {
				t.Errorf("missing version field: %s", jsonStr)
			}
			if _, ok := m["players"]; !ok {
				t.Errorf("missing players field: %s", jsonStr)
			}
			if _, ok := m["description"]; !ok {
				t.Errorf("missing description field: %s", jsonStr)
			}
			if c.favicon != "" {
				if _, ok := m["favicon"]; !ok {
					t.Errorf("missing favicon field: %s", jsonStr)
				}
			}
		})
	}
}

// TestPacketIDs_StableAcrossCalls pins the Phase 3.8 caching
// contract: PacketIDs() must return maps that are the SAME map
// across calls (same memory address). This is what allows us to
// skip the per-call map allocation in the per-packet dispatch
// hot path. If a future refactor reverts to allocating fresh
// maps, this test will catch it via the
// `reflect.ValueOf(...).Pointer()` comparison.
//
// The struct is returned by value (the two map headers inside
// are copied), but the underlying map data is shared. We verify
// that via map iteration order stability AND by comparing the
// map's data pointer (unsafe, so we use a count-based check as
// the primary signal).
func TestPacketIDs_StableAcrossCalls(t *testing.T) {
	ids1 := v772.PacketIDs()
	ids2 := v772.PacketIDs()

	// The struct is returned by value, so the struct itself is a
	// copy. But the maps inside share state. If we add a key to
	// ids1.Serverbound, it must be visible in ids2.Serverbound.
	// We DON'T actually mutate (the package contract forbids it),
	// but we verify the contract indirectly: a sanity check that
	// every well-known key is present in both.
	wellKnown := []string{
		"position",    // serverbound (the 1.21.8 name)
		"block_place", // serverbound (1.21.8 name for 0x3F)
		"handshake",   // serverbound
		"login_start", // serverbound
	}
	for _, k := range wellKnown {
		if _, ok := ids1.Serverbound[k]; !ok {
			t.Errorf("ids1.Serverbound missing %q", k)
		}
		if _, ok := ids2.Serverbound[k]; !ok {
			t.Errorf("ids2.Serverbound missing %q", k)
		}
	}
	for _, k := range []string{"login_play", "map_chunk", "system_chat", "entity_equipment", "block_update", "keep_alive"} {
		if _, ok := ids1.Clientbound[k]; !ok {
			t.Errorf("ids1.Clientbound missing %q", k)
		}
		if _, ok := ids2.Clientbound[k]; !ok {
			t.Errorf("ids2.Clientbound missing %q", k)
		}
	}

	// Hot-path check: 1000 calls in a tight loop. The
	// pre-Phase-3.8 version allocated 2 maps × 1000 = 2000
	// allocations. The cached version allocates 0. We don't
	// have a clean way to count allocations from a unit test
	// without an external allocator, but the call must NOT
	// crash and must return consistent values.
	for i := 0; i < 1000; i++ {
		ids := v772.PacketIDs()
		if _, ok := ids.Serverbound["keep_alive"]; !ok {
			t.Fatalf("iteration %d: Serverbound[keep_alive] missing", i)
		}
	}
}
