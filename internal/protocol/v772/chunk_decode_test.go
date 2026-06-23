package v772

import (
	"testing"

	"goore/internal/protocol"
	"goore/internal/world"
)

// readSectionHeader reads a section's block-states header, returns (blockCount, bits, palette).
// Skips block-states data array and biomes palette.
func readSectionHeader(t *testing.T, r *protocol.WireReader) (int16, uint8, []int32) {
	t.Helper()
	count := r.Int16()
	bits := r.Byte()
	if r.Err() != nil {
		t.Fatalf("section header: %v", r.Err())
	}
	var palette []int32
	if bits == 0 {
		_ = r.VarInt() // single value
		// No data length for single-value palettes.
	} else {
		paletteLen := r.VarInt()
		palette = make([]int32, paletteLen)
		for i := int32(0); i < paletteLen; i++ {
			palette[i] = r.VarInt()
		}
		// In 1.21.5+ (noSizePrefix) the data length is NOT on the wire;
		// the client computes it from bits and section size.
		entries := world.BlocksPerSection
		longs := (entries*int(bits) + 63) / 64
		for j := 0; j < longs; j++ {
			_ = r.Int64()
		}
	}
	// skip biomes
	bb := r.Byte()
	if bb == 0 {
		_ = r.VarInt() // single value, no data length
	} else {
		pl := r.VarInt()
		for j := int32(0); j < pl; j++ {
			_ = r.VarInt()
		}
		// In 1.21.5+ (noSizePrefix) the data length is NOT on the wire.
		entries := world.BiomesPerSection
		longs := (entries*int(bb) + 63) / 64
		for j := 0; j < longs; j++ {
			_ = r.Int64()
		}
	}
	return count, bits, palette
}

// readSection reads a full section and returns the unpacked block values (or nil if single-value).
func readSection(t *testing.T, r *protocol.WireReader) []world.Block {
	t.Helper()
	count := r.Int16()
	bits := r.Byte()
	if r.Err() != nil {
		t.Fatalf("section header: %v", r.Err())
	}
	_ = count

	var values []world.Block
	if bits == 0 {
		_ = r.VarInt()
		// No data length for single-value palettes.
	} else {
		paletteLen := r.VarInt()
		palette := make([]int32, paletteLen)
		for i := int32(0); i < paletteLen; i++ {
			palette[i] = r.VarInt()
		}
		// In 1.21.5+ (noSizePrefix) the data length is NOT on the wire.
		longsCount := (world.BlocksPerSection*int(bits) + 63) / 64
		longs := make([]int64, longsCount)
		for i := 0; i < longsCount; i++ {
			longs[i] = r.Int64()
		}

		values = make([]world.Block, world.BlocksPerSection)
		mask := uint64((1 << bits) - 1)
		for i := 0; i < world.BlocksPerSection; i++ {
			bitOffset := i * int(bits)
			longIdx := bitOffset / 64
			bitShift := bitOffset % 64
			v := uint64(longs[longIdx]>>uint(bitShift)) & mask
			if bitShift+int(bits) > 64 && longIdx+1 < len(longs) {
				overflow := bitShift + int(bits) - 64
				v |= uint64(longs[longIdx+1]&((1<<overflow)-1)) << uint(64-bitShift)
			}
			values[i] = world.Block(palette[v])
		}
	}

	// skip biomes
	bb := r.Byte()
	if bb == 0 {
		_ = r.VarInt() // single value, no data length
	} else {
		pl := r.VarInt()
		for j := int32(0); j < pl; j++ {
			_ = r.VarInt()
		}
		// In 1.21.5+ (noSizePrefix) the data length is NOT on the wire.
		longsCount := (world.BiomesPerSection*int(bb) + 63) / 64
		for j := 0; j < longsCount; j++ {
			_ = r.Int64()
		}
	}
	return values
}

// TestChunkPacketStructure is intentionally placed in the player package's
// integration test (`validateChunkPacket` in player_integration_test.go).
// It exercises the full packet including light data, which is assembled
// inside `Player.buildChunkPacket`. See that file for the assertions.

// TestChunkRoundTrip encodes a flat chunk and verifies the blocks decode back correctly.
func TestChunkRoundTrip(t *testing.T) {
	c := world.NewFlatChunk(world.ChunkPos{X: 0, Z: 0})

	data := EncodeChunkSections(c)
	if data == nil {
		t.Fatal("encoded chunk data is nil")
	}
	t.Logf("encoded chunk data: %d bytes", len(data))

	// First, dump metadata for all 24 sections
	r := protocol.NewWireReader(data)
	for i := 0; i < world.SectionsPerChunk; i++ {
		pos := len(data) - r.Remaining()
		count, bits, palette := readSectionHeader(t, r)
		t.Logf("section %2d: pos=%4d count=%4d bits=%d palette=%v", i, pos, count, bits, palette)
		// Invariant: palette must not contain duplicates and air (0) must be at index 0 if present.
		seen := make(map[int32]bool)
		for _, v := range palette {
			if seen[v] {
				t.Errorf("section %d: duplicate palette entry %d", i, v)
			}
			seen[v] = true
		}
		if len(palette) > 0 && palette[0] != 0 {
			t.Errorf("section %d: palette[0] = %d, want 0 (air first)", i, palette[0])
		}
	}

	// Now decode ALL 24 sections and verify the full content round-trips.
	r2 := protocol.NewWireReader(data)
	for si := 0; si < world.SectionsPerChunk; si++ {
		values := readSection(t, r2)
		original := c.Sections[si].Blocks[:]
		worldYBase := world.MinY + si*16

		// If section is a single-value palette, all blocks should equal the same value.
		// If we have values, compare them.
		if values == nil {
			// Single-value palette — all 4096 blocks should be the same.
			// Determine the expected value by checking any position.
			expected := original[0]
			for i, v := range original {
				if v != expected {
					t.Errorf("section %d index %d: encoded as single-value but original blocks differ (%d vs %d)", si, i, v, expected)
				}
			}
		} else {
			if len(values) != len(original) {
				t.Fatalf("section %d: decoded %d values, want %d", si, len(values), len(original))
			}
			for i, v := range values {
				if v != original[i] {
					x := i & 0xF
					z := (i >> 4) & 0xF
					ly := (i >> 8) & 0xF
					worldY := worldYBase + ly
					t.Errorf("section %d (worldY %d..%d) at (x=%d, y=%d, z=%d) = %d, want %d",
						si, worldYBase, worldYBase+15, x, worldY, z, v, original[i])
				}
			}
		}
	}
	if r2.Err() != nil {
		t.Errorf("reader error after all sections: %v", r2.Err())
	}
}
