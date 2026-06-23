// Package world defines the Minecraft world model: blocks, sections, chunks, and the world map.
package world

import (
	"math"
	"sync"
	"time"
)

//go:generate go run ../../cmd/genblocks
// Regenerates internal/world/blocks_gen.go from minecraft-data 1.21.8.

// Block is a Minecraft block state ID.
type Block uint16

// ChunkPos identifies a chunk column in the world.
type ChunkPos struct {
	X, Z int32
}

// Constants for world dimensions.
const (
	MinY             = -64
	WorldHeight      = 384
	SectionsPerChunk = WorldHeight / 16 // 24
	BlocksPerSection = 16 * 16 * 16     // 4096
	BiomesPerSection = 4 * 4 * 4        // 64
	LightBytesPerSec = 2048             // 4096 * 4 bits = 2048 bytes
)

// SectionIdx returns the section index for a world Y coordinate.
func SectionIdx(worldY int) int {
	return (worldY - MinY) / 16
}

// LocalY returns the Y offset within a section for a world Y coordinate.
func LocalY(worldY int) int {
	return (worldY - MinY) % 16
}

// Section is a 16×16×16 sub-volume of a chunk. mu serializes content
// access (Blocks, Biomes, BlockCount, lighting fields) — the Player's
// HandleConn writes via Set while the server's broadcast goroutines
// read via Get.
type Section struct {
	Blocks      [BlocksPerSection]Block
	Biomes      [BiomesPerSection]int32 // 4×4×4 biome grid
	BlockCount  int16                   // cached non-air count
	SkyLight    [LightBytesPerSec]byte
	BlockLight  [LightBytesPerSec]byte
	HasSkyLight bool
	mu          sync.RWMutex
}

// Index returns the flat array index for (x, y, z) within a section.
// x, y, z must be in [0, 15].
func Index(x, y, z int) int {
	return (y << 8) | (z << 4) | x
}

// Get returns the block state ID at (x, y, z).
func (s *Section) Get(x, y, z int) Block {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Blocks[Index(x, y, z)]
}

// Set sets the block state ID at (x, y, z).
func (s *Section) Set(x, y, z int, b Block) {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := Index(x, y, z)
	old := s.Blocks[idx]
	if old == 0 && b != 0 {
		s.BlockCount++
	} else if old != 0 && b == 0 {
		if s.BlockCount > 0 {
			s.BlockCount--
		}
	}
	s.Blocks[idx] = b
}

// FillSkyLight fills the entire section with full skylight (0xFF).
func (s *Section) FillSkyLight() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.SkyLight {
		s.SkyLight[i] = 0xFF
	}
	s.HasSkyLight = true
}

// ClearSkyLight resets the section to "no skylight" (HasSkyLight = false, SkyLight = zeros). Used when a section is fully solid.
func (s *Section) ClearSkyLight() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.SkyLight {
		s.SkyLight[i] = 0
	}
	s.HasSkyLight = false
}

// Chunk is a 16×384×16 column in the world.
type Chunk struct {
	Pos       ChunkPos
	Sections  [SectionsPerChunk]Section
	Heightmap []int64 // 36 longs, 9 bits per entry for 16×16
}

// GetBlock returns the block at (x, worldY, z) within the chunk.
func (c *Chunk) GetBlock(x, worldY, z int) Block {
	si := SectionIdx(worldY)
	ly := LocalY(worldY)
	return c.Sections[si].Get(x, ly, z)
}

// SetBlock sets the block at (x, worldY, z) within the chunk.
func (c *Chunk) SetBlock(x, worldY, z int, b Block) {
	si := SectionIdx(worldY)
	ly := LocalY(worldY)
	c.Sections[si].Set(x, ly, z, b)
}

// RecomputeSkyLight uses the MVP heuristic: section is not fully solid → fill 0xFF; fully solid → no skylight. Known limitation: enclosed indoor air pockets also get full skylight. See docs/world.md §Block lighting.
func (c *Chunk) RecomputeSkyLight() {
	for si := 0; si < SectionsPerChunk; si++ {
		sec := &c.Sections[si]
		if int(sec.BlockCount) < BlocksPerSection {
			sec.FillSkyLight()
		} else {
			sec.ClearSkyLight()
		}
	}
}

// UpdateHeightmap recalculates the heightmap. heightmap = (worldY - minY + 1) for highest non-air block, or 0 if all air.
func (c *Chunk) UpdateHeightmap() {
	bitsPerEntry := 9 // ceil(log2(384+1)) = 9
	entries := 256    // 16×16

	if c.Heightmap == nil || len(c.Heightmap) == 0 {
		longs := (entries*bitsPerEntry + 63) / 64
		c.Heightmap = make([]int64, longs)
	}

	values := make([]int32, entries)
	for x := 0; x < 16; x++ {
		for z := 0; z < 16; z++ {
			idx := z*16 + x
			height := int32(0)
			for worldY := MinY + WorldHeight - 1; worldY >= MinY; worldY-- {
				if c.GetBlock(x, worldY, z) != 0 {
					height = int32(worldY - MinY + 1)
					break
				}
			}
			values[idx] = height
		}
	}

	// Pack values into longs
	for i, v := range values {
		bitOffset := i * bitsPerEntry
		longIdx := bitOffset / 64
		bitShift := bitOffset % 64
		mask := uint64(v) & ((1 << bitsPerEntry) - 1)
		c.Heightmap[longIdx] |= int64(mask) << bitShift
		if bitShift+bitsPerEntry > 64 {
			// Value spans across two longs
			overflow := bitShift + bitsPerEntry - 64
			c.Heightmap[longIdx+1] |= int64(mask>>uint(64-bitShift)) & ((1 << overflow) - 1)
		}
	}
}

// World holds all loaded chunks, protected by a RWMutex.
type World struct {
	mu     sync.RWMutex
	chunks map[ChunkPos]*Chunk
	Seed   int64
	dir    string                // "" = no persistence
	dirty  map[ChunkPos]struct{} // chunks modified since last save
}

// New creates an empty, in-memory world (no persistence).
func New(seed int64) *World {
	return &World{
		chunks: make(map[ChunkPos]*Chunk),
		Seed:   seed,
	}
}

// NewWithDir creates a world backed by a directory. SetBlock will mark chunks dirty; SaveAll flushes them. dir must exist.
func NewWithDir(seed int64, dir string) *World {
	return &World{
		chunks: make(map[ChunkPos]*Chunk),
		Seed:   seed,
		dir:    dir,
		dirty:  make(map[ChunkPos]struct{}),
	}
}

// Dir returns the persistence directory (or "" if no persistence).
func (w *World) Dir() string { return w.dir }

// IsDirty reports whether a chunk has been modified since the last save.
func (w *World) IsDirty(pos ChunkPos) bool {
	if w.dirty == nil {
		return false
	}
	w.mu.RLock()
	defer w.mu.RUnlock()
	_, ok := w.dirty[pos]
	return ok
}

// markDirty records that a chunk needs to be saved. Internal helper.
func (w *World) markDirty(pos ChunkPos) {
	if w.dirty == nil {
		return
	}
	w.mu.Lock()
	w.dirty[pos] = struct{}{}
	w.mu.Unlock()
}

// StartFlusher launches a background goroutine that periodically flushes dirty chunks to disk every `interval`. Returns a stop function the caller should defer. No-op if w.dir == "".
func (w *World) StartFlusher(interval time.Duration) func() {
	if w.dir == "" {
		return func() {}
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				// Final flush before exit.
				_ = w.SaveAll()
				return
			case <-ticker.C:
				_ = w.SaveAll()
			}
		}
	}()
	return func() {
		close(stop)
		<-done
	}
}

// GetChunk returns a chunk at pos, loading from disk (if persistence dir) or generating a fresh flat chunk. Only SetBlock marks dirty.
func (w *World) GetChunk(pos ChunkPos) *Chunk {
	w.mu.RLock()
	c, ok := w.chunks[pos]
	w.mu.RUnlock()
	if ok {
		return c
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	// Double-check after acquiring writelock
	if c, ok = w.chunks[pos]; ok {
		return c
	}
	if w.dir != "" {
		if loaded, err := LoadChunk(w.dir, pos); err == nil {
			c = loaded
			w.chunks[pos] = c
			return c
		}
	}
	c = NewFlatChunk(pos)
	w.chunks[pos] = c
	return c
}

// SetBlock sets a block at absolute world coordinates. cx/cz use >> 4
// (Go's arithmetic shift is already floor-division; the Java/C idiom
// `(x-15) >> 4` would push negative chunks one too far).
func (w *World) SetBlock(worldX, worldY, worldZ int, b Block) {
	cx := int32(worldX >> 4)
	cz := int32(worldZ >> 4)
	c := w.GetChunk(ChunkPos{cx, cz})
	lx := ((worldX % 16) + 16) % 16
	lz := ((worldZ % 16) + 16) % 16
	c.SetBlock(lx, worldY, lz, b)
	w.markDirty(ChunkPos{cx, cz})
}

// GetBlock returns the block at absolute world coordinates.
func (w *World) GetBlock(worldX, worldY, worldZ int) Block {
	cx := int32(worldX >> 4)
	cz := int32(worldZ >> 4)
	c := w.GetChunk(ChunkPos{cx, cz})
	lx := ((worldX % 16) + 16) % 16
	lz := ((worldZ % 16) + 16) % 16
	return c.GetBlock(lx, worldY, lz)
}

// FindSafeSpawn validates a saved player position. "Safe" = feet=air AND head=air AND ground=solid. Searches up to 32 blocks above; falls back to default spawn (0.5, 4.0, 0.5) if none found. See docs/world.md §Player safe spawn validation.
func (w *World) FindSafeSpawn(savedX, savedY, savedZ float64) (x, y, z float64) {
	const fallbackX, fallbackY, fallbackZ = 0.5, 4.0, 0.5
	const maxSafeSearch = 32

	// math.Floor (not int() truncation) for correct negative coords.
	px := int(math.Floor(savedX))
	pz := int(math.Floor(savedZ))

	sy := int(math.Floor(savedY))
	if sy < MinY {
		sy = MinY
	}

	// Search upward for a safe spot.
	for y := sy; y < MinY+WorldHeight-2 && y < sy+maxSafeSearch; y++ {
		feet := w.GetBlock(px, y, pz)
		head := w.GetBlock(px, y+1, pz)
		ground := w.GetBlock(px, y-1, pz)
		if feet == BlockAir && head == BlockAir && ground != BlockAir {
			return float64(px) + 0.5, float64(y), float64(pz) + 0.5
		}
	}

	// No safe spot — fall back to default spawn.
	return fallbackX, fallbackY, fallbackZ
}

// NewFlatChunk creates a flat-world chunk (vanilla superflat "classic" preset).
// y=-64: bedrock, y=-63..2: dirt, y=3: grass_block, y=4+: air. Player spawns at y=4.
func NewFlatChunk(pos ChunkPos) *Chunk {
	c := &Chunk{Pos: pos}
	for si := 0; si < SectionsPerChunk; si++ {
		yBase := MinY + si*16
		sec := &c.Sections[si]

		for x := 0; x < 16; x++ {
			for z := 0; z < 16; z++ {
				for ly := 0; ly < 16; ly++ {
					worldY := yBase + ly
					var b Block
					switch {
					case worldY == -64:
						b = BlockBedrock
					case worldY == 3:
						b = BlockGrass
					case worldY >= -63 && worldY <= 2:
						b = BlockDirt
					default:
						b = BlockAir
					}
					sec.Set(x, ly, z, b)
				}
			}
		}

		// Section 4 (yBase=0) is the first with air; sections 0..3 are fully solid (bedrock+dirt).
		if yBase >= 0 {
			sec.FillSkyLight()
		}
	}
	c.UpdateHeightmap()
	return c
}

// Block state ID constants (minecraft-data 1.21.8 defaultState field, NOT base id).
// TODO: generate from blocks.json via go generate
const (
	BlockAir     Block = 0
	BlockStone   Block = 1
	BlockGrass   Block = 9  // minecraft:grass_block default state (snowy=false)
	BlockDirt    Block = 10 // minecraft:dirt default state
	BlockBedrock Block = 85 // minecraft:bedrock default state
)
