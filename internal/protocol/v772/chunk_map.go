// Wire encoding of a single chunk for the map_chunk (0x27) clientbound packet (1.21.5+ format). Output is a complete map_chunk packet ready to send. See docs/protocol.md §Chunk Encoding and docs/regressions.md #14.

package v772

import (
	"goore/internal/protocol"
	"goore/internal/world"
)

// Heightmap type enum values. We only emit MOTION_BLOCKING and WORLD_SURFACE.
const (
	heightmapWorldSurfaceWG         = 0
	heightmapWorldSurface           = 1
	heightmapOceanFloorWG           = 2
	heightmapOceanFloor             = 3
	heightmapMotionBlocking         = 4
	heightmapMotionBlockingNoLeaves = 5
)

// WriteMapChunk builds a complete map_chunk (0x27) packet from a *world.Chunk. nil is returned if the underlying WireWriter errored.
func (p *Protocol) WriteMapChunk(c *world.Chunk) []byte {
	var w protocol.WireWriter

	writeHeightmaps(&w, c.Heightmap)

	secData := EncodeChunkSections(c)
	w.ByteArray(secData)

	// Block entities: 0 (no tile entities in our flat world).
	w.VarInt(0)

	writeLightData(&w, c)

	if w.Err() != nil {
		return nil
	}

	return p.WriteChunkData(w.Bytes(), protocol.ChunkPos(c.Pos))
}

// writeHeightmaps writes the heightmaps-prefixed-array field of the map_chunk body. Always emits exactly two (MOTION_BLOCKING, WORLD_SURFACE).
func writeHeightmaps(w *protocol.WireWriter, hm []int64) {
	if len(hm) == 0 {
		w.VarInt(0)
		return
	}
	w.VarInt(2)
	writeHeightmap(w, heightmapMotionBlocking, hm)
	writeHeightmap(w, heightmapWorldSurface, hm)
}

func writeHeightmap(w *protocol.WireWriter, typ int, hm []int64) {
	w.VarInt(int32(typ))
	w.VarInt(int32(len(hm)))
	for _, v := range hm {
		w.Int64(v)
	}
}

// writeLightData writes the four BitSet masks + the two light arrays.
func writeLightData(w *protocol.WireWriter, c *world.Chunk) {
	const sections = world.SectionsPerChunk

	var skyMask, blockMask, emptySkyMask, emptyBlockMask uint64
	var skyArrays, blockArrays [][]byte

	for i := 0; i < sections; i++ {
		sec := &c.Sections[i]
		if sec.HasSkyLight {
			if isAllZero(sec.SkyLight[:]) {
				emptySkyMask |= 1 << i
			} else {
				skyMask |= 1 << i
				arr := make([]byte, len(sec.SkyLight))
				copy(arr, sec.SkyLight[:])
				skyArrays = append(skyArrays, arr)
			}
		}
		if !isAllZero(sec.BlockLight[:]) {
			blockMask |= 1 << i
			arr := make([]byte, len(sec.BlockLight))
			copy(arr, sec.BlockLight[:])
			blockArrays = append(blockArrays, arr)
		} else if sec.BlockCount > 0 {
			emptyBlockMask |= 1 << i
		}
	}

	// 1.21+ removed trustEdges: bool. Do NOT write it. See docs/regressions.md #14.
	writeBitSet(w, skyMask)
	writeBitSet(w, blockMask)
	writeBitSet(w, emptySkyMask)
	writeBitSet(w, emptyBlockMask)

	w.VarInt(int32(len(skyArrays)))
	for _, arr := range skyArrays {
		w.VarInt(int32(len(arr)))
		w.RawWrite(arr)
	}

	w.VarInt(int32(len(blockArrays)))
	for _, arr := range blockArrays {
		w.VarInt(int32(len(arr)))
		w.RawWrite(arr)
	}
}

// writeBitSet writes a single BitSet: VarInt(long count) + long[count]. For 24 sections count is 1. VarInt(0) = all-zeros sentinel.
func writeBitSet(w *protocol.WireWriter, mask uint64) {
	if mask == 0 {
		w.VarInt(0)
		return
	}
	w.VarInt(1)
	w.Int64(int64(mask))
}

// isAllZero reports whether every byte in the slice is 0.
func isAllZero(data []byte) bool {
	for _, b := range data {
		if b != 0 {
			return false
		}
	}
	return true
}
