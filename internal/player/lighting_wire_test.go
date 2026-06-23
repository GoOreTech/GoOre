// Package player — wire-level lighting test.
//
// User-reported bug: "after restart, edited chunks render as dark".
// Root cause: LoadChunk returned a chunk with HasSkyLight=false on every
// section (Go zero value), and the wire encoder's writeLightData only
// emits skylight for sections with HasSkyLight=true. The client therefore
// received no skylight data and rendered the chunk dark.
//
// This test is the wire-level regression: it exercises the full path
// from "save a chunk" to "encode the map_chunk packet" and verifies that
// the wire byte stream contains a non-zero skyLightMask and 0xFF skylight
// bytes for the surface section.
//
// We reuse BuildChunkPacketForTest so the wire encoding matches exactly
// what the real server produces.
package player

import (
	"testing"

	"goore/internal/config"
	"goore/internal/protocol"
	"goore/internal/protocol/v772"
	"goore/internal/world"
)

// TestLoadedChunkHasLightOnWire is the user-reported bug regression: a
// chunk that has been saved and reloaded must still send a non-zero
// skyLightMask in the wire packet, with full 0xFF skylight for the
// surface section.
func TestLoadedChunkHasLightOnWire(t *testing.T) {
	dir := t.TempDir()
	w := world.NewWithDir(0, dir)

	// Modify the surface so the chunk is dirty and will be saved.
	w.SetBlock(5, 4, 5, world.BlockDirt) // air → dirt in the surface section

	// Save.
	if err := w.SaveAll(); err != nil {
		t.Fatalf("SaveAll: %v", err)
	}

	// Load via a fresh world (simulating server restart: chunks are
	// loaded lazily on first GetChunk).
	w2 := world.NewWithDir(0, dir)
	if err := w2.LoadAll(); err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	c := w2.GetChunk(world.ChunkPos{X: 0, Z: 0})

	// Build the wire packet the way the real server would.
	p := New(0, nil, v772.New(), w2, config.DefaultConfig())
	pkt := p.BuildChunkPacketForTest(c, c.Pos)
	if len(pkt) == 0 {
		t.Fatal("BuildChunkPacketForTest returned empty")
	}

	// Decode and check the skyLightMask is non-zero.
	skyMask, skyArrCount, skyArrFirstByte := decodeLightData(t, pkt)
	if skyMask == 0 {
		t.Errorf("skyLightMask = 0 on the wire — chunk will render as dark on the client")
	}
	if skyArrCount == 0 {
		t.Errorf("skyLight array count = 0 — no skylight sent to client")
	}
	if skyArrFirstByte != 0xFF {
		t.Errorf("first skyLight byte = %#x, want 0xFF (full skylight)", skyArrFirstByte)
	}

	// The surface section (index 4, yBase=0) must have its bit set in
	// skyMask. Sections 0..3 are fully solid (no skylight access);
	// sections 4..23 have sky.
	const surfaceBit = uint64(1) << 4
	if skyMask&surfaceBit == 0 {
		t.Errorf("skyLightMask missing surface section bit (bit %d) — section 4 should be lit", 4)
	}
}

// TestFreshFlatChunkHasLightOnWire is the control: a freshly generated
// flat chunk (no save/reload) must also have non-zero skylight on the
// wire. This guards against a regression where NewFlatChunk stops
// computing skylight and only LoadChunk does.
func TestFreshFlatChunkHasLightOnWire(t *testing.T) {
	w := world.New(0)
	c := w.GetChunk(world.ChunkPos{X: 0, Z: 0})

	p := New(0, nil, v772.New(), w, config.DefaultConfig())
	pkt := p.BuildChunkPacketForTest(c, c.Pos)

	skyMask, skyArrCount, _ := decodeLightData(t, pkt)
	if skyMask == 0 {
		t.Errorf("fresh flat chunk skyLightMask = 0 — should be non-zero")
	}
	if skyArrCount != 20 {
		t.Errorf("fresh flat chunk skyLight array count = %d, want 20 (sections 4..23)", skyArrCount)
	}
}

// decodeLightData walks the wire packet body and extracts the
// skyLightMask (u64), the count of sky light arrays, and the first byte
// of the first sky light array.
func decodeLightData(t *testing.T, pkt []byte) (skyMask uint64, skyArrCount int32, firstSkyByte byte) {
	t.Helper()
	r := protocol.NewWireReader(pkt)
	// Strip the outer packet header: varint length + varint packet ID.
	if _, err := readVarIntRaw(r); err != nil {
		t.Fatalf("read packet length: %v", err)
	}
	if _, err := readVarIntRaw(r); err != nil {
		t.Fatalf("read packet ID: %v", err)
	}
	// X, Z
	r.Int32()
	r.Int32()
	// Heightmaps
	hmCount := r.VarInt()
	for i := int32(0); i < hmCount; i++ {
		r.VarInt()
		hmLen := r.VarInt()
		for j := int32(0); j < hmLen; j++ {
			r.Int64()
		}
	}
	// Section data (length-prefixed byte array)
	_ = r.ByteArray()
	// Block entities
	_ = r.VarInt()
	// Light data: 4 BitSets
	skyMask = decodeBitSet(r)
	_ = decodeBitSet(r) // blockLightMask
	_ = decodeBitSet(r) // emptySkyLightMask
	_ = decodeBitSet(r) // emptyBlockLightMask
	// Sky light arrays
	skyArrCount = r.VarInt()
	if skyArrCount > 0 {
		arrLen := r.VarInt()
		if arrLen > 0 {
			firstSkyByte = r.Byte()
		}
	}
	return skyMask, skyArrCount, firstSkyByte
}

func decodeBitSet(r *protocol.WireReader) uint64 {
	n := r.VarInt()
	if n == 0 {
		return 0
	}
	var mask uint64
	for i := int32(0); i < n; i++ {
		mask = uint64(r.Int64())
	}
	return mask
}

func readVarIntRaw(r *protocol.WireReader) (int32, error) {
	var result int32
	var shift uint
	for {
		b, err := r.ReadByte()
		if err != nil {
			return 0, err
		}
		result |= int32(b&0x7F) << shift
		if b&0x80 == 0 {
			return result, nil
		}
		shift += 7
	}
}
