package v772

import (
	"math/bits"

	"goore/internal/protocol"
	"goore/internal/world"
)

// encodeSection writes a single section's data to a WireWriter. Format: BlockCount(i16) + BlockStates(palette) + Biomes(palette).
func encodeSection(w *protocol.WireWriter, sec *world.Section) {
	w.Int16(sec.BlockCount)
	encodePaletteBlock(w, sec.Blocks[:], world.BlocksPerSection)
	encodePaletteBiome(w, sec.Biomes[:], world.BiomesPerSection)
}

// encodeChunkSections encodes all 24 sections of a chunk into a byte slice.
func EncodeChunkSections(c *world.Chunk) []byte {
	var w protocol.WireWriter
	for i := 0; i < world.SectionsPerChunk; i++ {
		encodeSection(&w, &c.Sections[i])
	}
	if w.Err() != nil {
		return nil
	}
	return w.Bytes()
}

// paletteEntry is a value in the palette.
type paletteEntry int32

// encodePaletteBlock writes a block state palette container (1.21.5+ noSizePrefix). See docs/regressions.md #14.
func encodePaletteBlock(w *protocol.WireWriter, values []world.Block, count int) {
	palette, paletteMap := buildPalette(values, count)
	bits := paletteBits(len(palette))
	w.Byte(uint8(bits))

	if bits == 0 {
		// Single value — just the value, NO data length.
		w.VarInt(int32(palette[0]))
		return
	}

	w.VarInt(int32(len(palette)))
	for _, v := range palette {
		w.VarInt(int32(v))
	}

	// 1.21.5+ (noSizePrefix): length NOT on the wire, computed from bits × count.
	dataLongs := (count*bits + 63) / 64

	longs := make([]int64, dataLongs)
	for i := 0; i < count; i++ {
		v := paletteMap[values[i]]
		bitOffset := i * bits
		longIdx := bitOffset / 64
		bitShift := bitOffset % 64

		longs[longIdx] |= int64(v) << bitShift
		if longIdx+1 < len(longs) && bitShift+bits > 64 {
			overflow := bitShift + bits - 64
			longs[longIdx+1] |= int64(v) >> uint(64-bitShift) & ((1 << overflow) - 1)
		}
	}
	for _, l := range longs {
		w.Int64(l)
	}
}

// encodePaletteBiome writes a biome palette container. Format: see encodePaletteBlock.
func encodePaletteBiome(w *protocol.WireWriter, values []int32, count int) {
	palette, paletteMap := buildPaletteI32(values, count)
	bits := paletteBits(len(palette))
	w.Byte(uint8(bits))

	if bits == 0 {
		w.VarInt(palette[0])
		return
	}

	w.VarInt(int32(len(palette)))
	for _, v := range palette {
		w.VarInt(v)
	}

	dataLongs := (count*bits + 63) / 64

	longs := make([]int64, dataLongs)
	for i := 0; i < count; i++ {
		v := paletteMap[values[i]]
		bitOffset := i * bits
		longIdx := bitOffset / 64
		bitShift := bitOffset % 64

		longs[longIdx] |= int64(v) << bitShift
		if longIdx+1 < len(longs) && bitShift+bits > 64 {
			overflow := bitShift + bits - 64
			longs[longIdx+1] |= int64(v) >> uint(64-bitShift) & ((1 << overflow) - 1)
		}
	}
	for _, l := range longs {
		w.Int64(l)
	}
}

// buildPalette builds a unique palette from block values, ensuring air (id=0) is at index 0 (protocol convention) without producing duplicates.
func buildPalette(values []world.Block, count int) ([]paletteEntry, map[world.Block]int32) {
	seen := make(map[world.Block]int32)
	var pal []paletteEntry
	for i := 0; i < count; i++ {
		if _, ok := seen[values[i]]; !ok {
			seen[values[i]] = int32(len(pal))
			pal = append(pal, paletteEntry(values[i]))
		}
	}
	airIdx, hasAir := seen[0]
	if hasAir {
		if airIdx != 0 {
			// Move air to the front, shift everything else down.
			pal[0], pal[airIdx] = pal[airIdx], pal[0]
			for k, v := range seen {
				if v == 0 {
					seen[k] = airIdx
				} else if v == airIdx {
					seen[k] = 0
				}
			}
		}
	} else if len(pal) > 0 {
		// No air in palette — prepend it.
		newPal := make([]paletteEntry, 0, len(pal)+1)
		newPal = append(newPal, 0)
		newPal = append(newPal, pal...)
		pal = newPal
		for k := range seen {
			seen[k]++
		}
		seen[0] = 0
	}
	return pal, seen
}

func buildPaletteI32(values []int32, count int) ([]int32, map[int32]int32) {
	seen := make(map[int32]int32)
	var pal []int32
	for i := 0; i < count; i++ {
		if _, ok := seen[values[i]]; !ok {
			seen[values[i]] = int32(len(pal))
			pal = append(pal, values[i])
		}
	}
	return pal, seen
}

// paletteBits returns the bit width for a palette of given size.
func paletteBits(paletteSize int) int {
	switch {
	case paletteSize <= 1:
		return 0
	case paletteSize <= 16:
		return 4
	default:
		b := bits.Len(uint(paletteSize - 1))
		if b > 15 {
			return 15
		}
		return b
	}
}
