package player_test

import (
	"bytes"
	"log"
	"net"
	"testing"
	"time"

	"goore/internal/config"
	"goore/internal/player"
	"goore/internal/protocol"
	"goore/internal/protocol/v772"
	"goore/internal/world"
)

// TestStatusFlow tests the status protocol (server list ping).
func TestStatusFlow(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	w := world.New(0)
	proto := v772.New()
	cfg := config.DefaultConfig()
	p := player.New(99, serverConn, proto, w, cfg)

	// Read goroutine
	serverPackets := make(chan serverPkt, 10)
	go func() {
		buf := make([]byte, 65536)
		accum := &bytes.Buffer{}
		for {
			n, err := clientConn.Read(buf)
			if err != nil {
				return
			}
			accum.Write(buf[:n])
			for accum.Len() > 0 {
				r := protocol.NewWireReader(accum.Bytes())
				length := r.VarInt()
				if r.Err() != nil {
					return
				}
				pktLen := int(length) + protocol.VarIntSize(length)
				if accum.Len() < pktLen {
					break
				}
				pktID := r.VarInt()
				if r.Err() != nil {
					return
				}
				payload := make([]byte, int(length)-protocol.VarIntSize(pktID))
				r.Read(payload)
				serverPackets <- serverPkt{id: pktID, data: payload}
				accum.Next(pktLen)
			}
		}
	}()

	go p.HandleConn()

	// Handshake with status intent
	hw := &protocol.WireWriter{}
	hw.VarInt(v772.Version)
	hw.String("localhost")
	hw.Uint16(uint16(cfg.Port))
	hw.VarInt(v772.HandshakeStateStatus)
	clientConn.Write(protocol.MakePacket(v772.HandshakeIntention, hw.Bytes()))

	// Status request
	clientConn.Write(protocol.MakePacket(v772.StatusRequest, nil))

	// Read status response
	pkt := readPacket(t, serverPackets, 2*time.Second)
	if pkt.id != v772.StatusServerInfo {
		t.Fatalf("expected StatusResponse (0x%02X), got 0x%02X", v772.StatusServerInfo, pkt.id)
	}

	// Verify JSON payload contains expected fields
	r := protocol.NewWireReader(pkt.data)
	jsonStr := r.String()
	if r.Err() != nil {
		t.Fatalf("failed to read status JSON: %v", r.Err())
	}
	if !stringsContains(jsonStr, "GoOre") {
		t.Errorf("status JSON doesn't contain server name: %s", jsonStr)
	}
	if !stringsContains(jsonStr, `"protocol":772`) {
		t.Errorf("status JSON doesn't contain protocol version: %s", jsonStr)
	}
	if !stringsContains(jsonStr, cfg.MOTD) {
		t.Errorf("status JSON doesn't contain MOTD: %s", jsonStr)
	}

	// Ping request
	ts := time.Now().UnixMilli()
	pw := &protocol.WireWriter{}
	pw.Int64(ts)
	clientConn.Write(protocol.MakePacket(v772.StatusPong, pw.Bytes()))

	// Read pong response
	pkt = readPacket(t, serverPackets, 2*time.Second)
	if pkt.id != v772.StatusPing {
		t.Fatalf("expected PongResponse (0x%02X), got 0x%02X", v772.StatusPing, pkt.id)
	}
	r = protocol.NewWireReader(pkt.data)
	echo := r.Int64()
	if r.Err() != nil {
		t.Fatalf("failed to read pong timestamp: %v", r.Err())
	}
	if echo != ts {
		t.Errorf("pong timestamp mismatch: sent %d, got %d", ts, echo)
	}
}

func stringsContains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// TestLoginFlow tests the full handshake → login → config → play flow.
func TestLoginFlow(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	w := world.New(0)
	proto := v772.New()
	cfg := config.DefaultConfig()
	cfg.ViewDist = 2 // small view distance for fast test
	p := player.New(42, serverConn, proto, w, cfg)

	// Read goroutine — reads packets from client end continuously
	serverPackets := make(chan serverPkt, 100)
	go func() {
		buf := make([]byte, 65536)
		accum := &bytes.Buffer{}
		for {
			n, err := clientConn.Read(buf)
			if err != nil {
				return
			}
			accum.Write(buf[:n])
			for accum.Len() > 0 {
				r := protocol.NewWireReader(accum.Bytes())
				length := r.VarInt()
				if r.Err() != nil {
					return
				}
				pktLen := int(length) + protocol.VarIntSize(length)
				if accum.Len() < pktLen {
					break
				}
				pktID := r.VarInt()
				if r.Err() != nil {
					return
				}
				payload := make([]byte, int(length)-protocol.VarIntSize(pktID))
				r.Read(payload)
				serverPackets <- serverPkt{id: pktID, data: payload}
				accum.Next(pktLen)
			}
		}
	}()

	// Run player handler in goroutine
	go p.HandleConn()

	// Step 1: Handshake
	hw := &protocol.WireWriter{}
	hw.VarInt(v772.Version)
	hw.String("localhost")
	hw.Uint16(uint16(cfg.Port))
	hw.VarInt(v772.HandshakeStateLogin)
	if hw.Err() != nil {
		t.Fatalf("handshake write: %v", hw.Err())
	}
	_, err := clientConn.Write(protocol.MakePacket(v772.HandshakeIntention, hw.Bytes()))
	if err != nil {
		t.Fatalf("write handshake: %v", err)
	}

	// Step 2: Login Start
	lw := &protocol.WireWriter{}
	lw.String("TestPlayer")
	uuid := [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	lw.UUID(uuid)
	if lw.Err() != nil {
		t.Fatalf("login start write: %v", lw.Err())
	}
	_, err = clientConn.Write(protocol.MakePacket(v772.LoginStart, lw.Bytes()))
	if err != nil {
		t.Fatalf("write login start: %v", err)
	}

	// Read Login Success
	pkt := readPacket(t, serverPackets, 2*time.Second)
	if pkt.id != v772.LoginSuccess {
		t.Fatalf("expected LoginSuccess (0x%02X), got 0x%02X", v772.LoginSuccess, pkt.id)
	}

	// Step 3: Login Acknowledged
	_, err = clientConn.Write(protocol.MakePacket(v772.LoginAcknowledged, nil))
	if err != nil {
		t.Fatalf("write login ack: %v", err)
	}

	// Read config packets
	configPackets := make(map[int32]bool)
	for i := 0; i < 16; i++ { // at most 16 config packets (registries)
		pkt := readPacket(t, serverPackets, 1*time.Second)
		configPackets[pkt.id] = true
		log.Printf("Config packet: 0x%02X", pkt.id)
		if pkt.id == v772.ConfigFinishConfig {
			break // last config packet
		}
	}

	if !configPackets[v772.ConfigSelectKnownPacksCli] {
		t.Error("missing SelectKnownPacks packet")
	}
	if !configPackets[v772.ConfigRegistryData] {
		t.Error("missing RegistryData packet")
	}
	if !configPackets[v772.ConfigFinishConfig] {
		t.Error("missing FinishConfiguration packet")
	}

	// Step 4: Client sends serverbound config packets
	// ConfigSettings (0x00)
	cw := &protocol.WireWriter{}
	cw.String("en_US") // locale
	cw.Byte(8)         // view distance
	cw.VarInt(1)       // chat mode = 1 (commands only)
	cw.Bool(true)      // chat colors
	cw.Byte(0xFF)      // skin parts = all
	cw.VarInt(1)       // main hand = right
	cw.Bool(false)     // text filtering
	cw.Bool(true)      // allow server listings
	_, err = clientConn.Write(protocol.MakePacket(v772.ConfigSettings, cw.Bytes()))
	if err != nil {
		t.Fatalf("write config settings: %v", err)
	}

	// ConfigSelectKnownPacks (0x07)
	sw := &protocol.WireWriter{}
	sw.VarInt(1)           // 1 known pack
	sw.String("minecraft") // namespace
	sw.String("core")      // id
	sw.String("1.21.8")    // version
	_, err = clientConn.Write(protocol.MakePacket(v772.ConfigSelectKnownPacks, sw.Bytes()))
	if err != nil {
		t.Fatalf("write select known packs: %v", err)
	}

	// ConfigFinishConfig (0x03 serverbound) — triggers server to enter play
	_, err = clientConn.Write(protocol.MakePacket(v772.ConfigFinishConfig, nil))
	if err != nil {
		t.Fatalf("write finish config: %v", err)
	}

	// Read play packets: login_play, spawn_pos, abilities, position, chunks, etc.
	playPackets := make(map[int32]bool)
	var chunkPkt, containerPkt serverPkt
	for i := 0; i < 50; i++ {
		pkt := readPacket(t, serverPackets, 1*time.Second)
		playPackets[pkt.id] = true
		log.Printf("Play packet: 0x%02X", pkt.id)
		if pkt.id == v772.PlayMapChunk {
			chunkPkt = pkt
		}
		if pkt.id == v772.PlayWindowItems {
			containerPkt = pkt
		}
		if pkt.id == v772.PlayHeldItemSlot {
			// Last packet in spawn sequence
			break
		}
	}

	if !playPackets[v772.PlayLogin] {
		t.Error("missing LoginPlay packet")
	}
	if !playPackets[v772.PlaySpawnPos] {
		t.Error("missing SpawnPosition packet")
	}
	if !playPackets[v772.PlayAbilities] {
		t.Error("missing Abilities packet")
	}
	if !playPackets[v772.PlayPlayerPos] {
		t.Error("missing Position packet")
	}
	if !playPackets[v772.PlayMapChunk] {
		t.Error("missing Chunk packet")
	}
	if !playPackets[v772.PlayWindowItems] {
		t.Error("missing Set Container Content packet")
	}

	if chunkPkt.id == v772.PlayMapChunk {
		validateChunkPacket(t, chunkPkt.data)
	}
	if containerPkt.id == v772.PlayWindowItems {
		validateContainerContent(t, containerPkt.data)
	}
}

func validateContainerContent(t *testing.T, payload []byte) {
	t.Helper()
	r := protocol.NewWireReader(payload)
	windowID := r.VarInt()
	if windowID != 0 {
		t.Errorf("window id = %d, want 0", windowID)
	}
	_ = r.VarInt() // state id
	count := r.VarInt()
	if count != 46 { // 46 slots; carried is a separate field
		t.Errorf("slot count = %d, want 46", count)
	}
	for i := 0; i < int(count); i++ {
		itemCount := r.VarInt()
		if itemCount > 0 {
			_ = r.VarInt() // item id
			addCount := r.VarInt()
			removeCount := r.VarInt()
			if addCount != 0 {
				t.Errorf("slot %d: expected 0 add components, got %d", i, addCount)
			}
			if removeCount != 0 {
				t.Errorf("slot %d: expected 0 remove components, got %d", i, removeCount)
			}
		}
	}
	// carriedItem is a separate field after the array
	carriedCount := r.VarInt()
	if carriedCount != 0 {
		t.Errorf("carried count = %d, want 0 (empty)", carriedCount)
	}
	if r.Err() != nil {
		t.Fatalf("container content parse error: %v", r.Err())
	}
	if r.Remaining() != 0 {
		t.Errorf("expected 0 remaining bytes, got %d", r.Remaining())
	}
}

func validateChunkPacket(t *testing.T, payload []byte) {
	t.Helper()
	r := protocol.NewWireReader(payload)
	x := r.Int32()
	z := r.Int32()
	_ = x
	_ = z

	// Heightmaps: prefixed array of Heightmap structures
	hmCount := r.VarInt()
	for i := int32(0); i < hmCount; i++ {
		r.VarInt() // type
		hmLen := r.VarInt()
		for j := int32(0); j < hmLen; j++ {
			r.Int64()
		}
	}
	if r.Err() != nil {
		t.Fatalf("failed to read heightmaps: %v", r.Err())
	}

	// Data: prefixed byte array of chunk sections
	dataBytes := r.ByteArray()
	if r.Err() != nil {
		t.Fatalf("failed to read data byte array: %v", r.Err())
	}
	// Verify that data contains the expected 24 sections
	dr := protocol.NewWireReader(dataBytes)
	for i := 0; i < world.SectionsPerChunk; i++ {
		dr.Int16() // block count
		bits := dr.Byte()
		if dr.Err() != nil {
			t.Fatalf("section %d header error: %v", i, dr.Err())
		}
		if bits == 0 {
			dr.VarInt() // single value (no data length)
		} else {
			pl := dr.VarInt()
			for j := int32(0); j < pl; j++ {
				dr.VarInt()
			}
			// 1.21.5+ (noSizePrefix): no data length on the wire, computed from bits and section size.
			longs := (world.BlocksPerSection*int(bits) + 63) / 64
			for j := 0; j < longs; j++ {
				dr.Int64()
			}
		}
		// biome
		bb := dr.Byte()
		if bb == 0 {
			dr.VarInt() // single value (no data length)
		} else {
			pl := dr.VarInt()
			for j := int32(0); j < pl; j++ {
				dr.VarInt()
			}
			// 1.21.5+ (noSizePrefix): no data length on the wire.
			longs := (world.BiomesPerSection*int(bb) + 63) / 64
			for j := 0; j < longs; j++ {
				dr.Int64()
			}
		}
	}
	if dr.Err() != nil {
		t.Fatalf("data parse error: %v", dr.Err())
	}
	if dr.Remaining() != 0 {
		t.Errorf("expected 0 remaining bytes in data, got %d", dr.Remaining())
	}

	// block entities
	beCount := r.VarInt()
	if beCount != 0 {
		t.Errorf("expected 0 block entities, got %d", beCount)
	}

	// 1.21+ removed trustEdges from map_chunk. No bool to skip here.
	// sky light mask count + longs
	// BitSet: VarInt(long count) + i64[long count]. For 24 sections the
	// BitSet always allocates 1 long, even if most bits are 0. If we see
	// 2, the mask shifted and a stray 0x01 byte (e.g. leftover trustEdges
	// bool) was interpreted as part of the count.
	skyMaskCount := r.VarInt()
	if skyMaskCount != 1 {
		t.Errorf("skyLightMask long count = %d, want 1 (BitSet always reserves 1 long for 24 sections)", skyMaskCount)
	}
	for i := int32(0); i < skyMaskCount; i++ {
		r.Int64()
	}
	// block light mask count
	// Flat world has no block light set anywhere → BitSet is empty → 0 longs.
	blockMaskCount := r.VarInt()
	if blockMaskCount != 0 {
		t.Errorf("blockLightMask long count = %d, want 0 (no block light set)", blockMaskCount)
	}
	for i := int32(0); i < blockMaskCount; i++ {
		r.Int64()
	}
	// empty sky mask count
	// All sections that have HasSkyLight=true are fully lit (no zero-only
	// skylight sections) → BitSet is empty → 0 longs.
	emptySkyCount := r.VarInt()
	if emptySkyCount != 0 {
		t.Errorf("emptySkyLightMask long count = %d, want 0 (all skylight sections are fully lit)", emptySkyCount)
	}
	for i := int32(0); i < emptySkyCount; i++ {
		r.Int64()
	}
	// empty block mask count
	// Sections 0..3 have blocks but no block light → 1 long, 4 bits set.
	emptyBlockCount := r.VarInt()
	if emptyBlockCount != 1 {
		t.Errorf("emptyBlockLightMask long count = %d, want 1 (4 sections with empty block light)", emptyBlockCount)
	}
	for i := int32(0); i < emptyBlockCount; i++ {
		r.Int64()
	}

	// sky arrays
	// Flat world: surface at y=3. Sections with HasSkyLight=true are
	// those with yBase >= 4 (i.e. world Y >= 4) — that's sections 4..23,
	// or 20 sections. Each array is 2048 bytes (4-bit nibbles × 4096).
	skyArrCount := r.VarInt()
	if skyArrCount != 20 {
		t.Errorf("skyLight array count = %d, want 20 (sections 4..23)", skyArrCount)
	}
	for i := int32(0); i < skyArrCount; i++ {
		alen := r.VarInt()
		if alen != 2048 {
			t.Errorf("skyLight[%d] length = %d, want 2048", i, alen)
		}
		r.Read(make([]byte, alen))
	}
	// block arrays
	blockArrCount := r.VarInt()
	if blockArrCount != 0 {
		t.Errorf("blockLight array count = %d, want 0 (no block light set)", blockArrCount)
	}
	for i := int32(0); i < blockArrCount; i++ {
		alen := r.VarInt()
		r.Read(make([]byte, alen))
	}

	if r.Err() != nil {
		t.Fatalf("chunk packet parse error: %v", r.Err())
	}
	if r.Remaining() != 0 {
		t.Errorf("expected 0 remaining bytes, got %d", r.Remaining())
	}
}
