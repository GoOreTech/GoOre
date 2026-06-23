// Package world — persistence tests.
//
// We test the custom binary chunk format: write a chunk with known
// blocks, read it back, verify equality.
package world

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"time"
	"unsafe"
)

func TestSaveLoadChunkRoundTrip(t *testing.T) {
	dir := t.TempDir()

	// Build a chunk with a non-trivial pattern.
	original := NewFlatChunk(ChunkPos{X: 7, Z: -3})
	// Modify a few blocks in the surface (y=3..5) range.
	original.SetBlock(0, 3, 0, BlockDirt)    // grass → dirt
	original.SetBlock(1, 4, 0, BlockStone)   // air → stone
	original.SetBlock(2, 3, 2, BlockBedrock) // grass → bedrock (overwrite)

	// Save.
	if err := SaveChunk(dir, original); err != nil {
		t.Fatalf("SaveChunk: %v", err)
	}

	// Verify file exists.
	path := ChunkPath(dir, original.Pos)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("chunk file not created: %v", err)
	}

	// Load.
	loaded, err := LoadChunk(dir, original.Pos)
	if err != nil {
		t.Fatalf("LoadChunk: %v", err)
	}

	// Verify.
	if loaded.Pos != original.Pos {
		t.Errorf("Pos = %+v, want %+v", loaded.Pos, original.Pos)
	}
	for si := 0; si < SectionsPerChunk; si++ {
		// Field-by-field comparison instead of copying the Section
		// (which now embeds a sync.RWMutex and triggers go vet's
		// "copies lock value" check).
		if original.Sections[si].BlockCount != loaded.Sections[si].BlockCount {
			t.Errorf("section %d BlockCount = %d, want %d",
				si, loaded.Sections[si].BlockCount, original.Sections[si].BlockCount)
		}
		if !bytes.Equal(blocksAsBytes(original.Sections[si].Blocks[:]), blocksAsBytes(loaded.Sections[si].Blocks[:])) {
			t.Errorf("section %d blocks differ", si)
		}
	}
}

func TestLoadChunkMissing(t *testing.T) {
	dir := t.TempDir()
	_, err := LoadChunk(dir, ChunkPos{X: 100, Z: 100})
	if err == nil {
		t.Errorf("LoadChunk of non-existent chunk should return an error, got nil")
	}
}

func TestSaveChunkCreatesDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "chunks")
	c := NewFlatChunk(ChunkPos{X: 0, Z: 0})
	if err := SaveChunk(dir, c); err != nil {
		t.Fatalf("SaveChunk: %v", err)
	}
	if _, err := os.Stat(ChunkPath(dir, c.Pos)); err != nil {
		t.Errorf("chunk file not created: %v", err)
	}
}

// blocksAsBytes reinterprets a []Block as []byte using unsafe for fast
// comparison. Block is uint16 (little-endian on all Go platforms).
func blocksAsBytes(blocks []Block) []byte {
	n := len(blocks) * int(unsafe.Sizeof(Block(0)))
	return unsafe.Slice((*byte)(unsafe.Pointer(&blocks[0])), n)
}

// Compile-time check that the helper is used and binary is referenced.
var _ = binary.LittleEndian

func TestWorldSetBlockMarksDirty(t *testing.T) {
	dir := t.TempDir()
	w := NewWithDir(0, dir)

	// Set a block; the chunk should be marked dirty.
	w.SetBlock(0, 4, 0, BlockDirt)
	if !w.IsDirty(ChunkPos{X: 0, Z: 0}) {
		t.Errorf("chunk (0,0) should be dirty after SetBlock")
	}
}

func TestWorldSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	w := NewWithDir(42, dir)

	// Modify a few blocks.
	w.SetBlock(0, 4, 0, BlockDirt)
	w.SetBlock(1, 4, 0, BlockStone)
	w.SetBlock(2, 4, 2, BlockBedrock)

	// Save.
	if err := w.SaveAll(); err != nil {
		t.Fatalf("SaveAll: %v", err)
	}

	// Sanity: file exists.
	chunkFile := filepath.Join(dir, "chunks", "0_0.chunk")
	if _, err := os.Stat(chunkFile); err != nil {
		t.Fatalf("chunk file missing after save: %v (dir=%q)", err, dir)
	}

	// Reopen.
	w2 := NewWithDir(0, dir)
	if err := w2.LoadAll(); err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if w2.Seed != 42 {
		t.Errorf("Seed = %d, want 42", w2.Seed)
	}
	if got := w2.GetBlock(0, 4, 0); got != BlockDirt {
		t.Errorf("GetBlock(0,4,0) = %d, want %d (dirt)", got, BlockDirt)
	}
	if got := w2.GetBlock(1, 4, 0); got != BlockStone {
		t.Errorf("GetBlock(1,4,0) = %d, want %d (stone)", got, BlockStone)
	}
	if got := w2.GetBlock(2, 4, 2); got != BlockBedrock {
		t.Errorf("GetBlock(2,4,2) = %d, want %d (bedrock)", got, BlockBedrock)
	}
	// Unmodified region: the flat world has grass at y=3.
	if got := w2.GetBlock(5, 3, 5); got != BlockGrass {
		t.Errorf("GetBlock(5,3,5) = %d, want %d (grass)", got, BlockGrass)
	}
}

func TestWorldSaveEmpty(t *testing.T) {
	dir := t.TempDir()
	w := NewWithDir(0, dir)
	// No modifications. SaveAll should not fail.
	if err := w.SaveAll(); err != nil {
		t.Errorf("SaveAll on empty world: %v", err)
	}
}

// TestLoadChunkRecomputesSkylight verifies that a chunk loaded from disk
// has its skylight recomputed from the block layout. Without the
// recompute, every section has HasSkyLight=false (Go zero value) and the
// client renders the chunk as dark.
func TestLoadChunkRecomputesSkylight(t *testing.T) {
	dir := t.TempDir()
	w := NewWithDir(0, dir)

	// Set a block on the surface, then save (marking the chunk dirty).
	w.SetBlock(0, 4, 0, BlockDirt) // place a dirt block in the air section
	if err := w.SaveAll(); err != nil {
		t.Fatalf("SaveAll: %v", err)
	}

	// Load via LoadChunk (simulates the lazy path on join).
	loaded, err := LoadChunk(dir, ChunkPos{X: 0, Z: 0})
	if err != nil {
		t.Fatalf("LoadChunk: %v", err)
	}

	// Section 4 (yBase=0) contains the surface — it has air, so it
	// should have HasSkyLight=true after load.
	const surfaceSection = 4
	if !loaded.Sections[surfaceSection].HasSkyLight {
		t.Errorf("section %d HasSkyLight = false, want true (surface section has air)", surfaceSection)
	}
	// All skylight nibbles should be 0xF (full skylight).
	for i, b := range loaded.Sections[surfaceSection].SkyLight {
		if b != 0xFF {
			t.Errorf("section %d SkyLight[%d] = %#x, want 0xFF", surfaceSection, i, b)
			break
		}
	}

	// Section 0 (yBase=-64, fully bedrock+dirt+stone) is fully solid,
	// so it should have HasSkyLight=false.
	const bottomSection = 0
	if loaded.Sections[bottomSection].HasSkyLight {
		t.Errorf("section %d HasSkyLight = true, want false (fully solid)", bottomSection)
	}
}

// TestLoadChunkRecomputesHeightmap verifies that a chunk loaded from disk
// has its heightmap recomputed. The heightmap is not stored on disk
// (saves bytes) and must be derived from the block layout on load.
func TestLoadChunkRecomputesHeightmap(t *testing.T) {
	dir := t.TempDir()
	w := NewWithDir(0, dir)
	w.SetBlock(0, 50, 0, BlockDirt) // place a block at y=50
	if err := w.SaveAll(); err != nil {
		t.Fatalf("SaveAll: %v", err)
	}
	loaded, err := LoadChunk(dir, ChunkPos{X: 0, Z: 0})
	if err != nil {
		t.Fatalf("LoadChunk: %v", err)
	}
	// Heightmap is now non-empty and reflects the placed block.
	if len(loaded.Heightmap) == 0 {
		t.Errorf("Heightmap is nil/empty after load — must be recomputed")
	}
	// Sanity: the heightmap should reflect the highest non-air block.
	// GetBlock still works (it reads from Sections directly).
	if got := loaded.GetBlock(0, 50, 0); got != BlockDirt {
		t.Errorf("GetBlock(0,50,0) = %d, want %d (dirt)", got, BlockDirt)
	}
}

// TestFindSafeSpawnFromValidPosition verifies that a player saved at
// a valid surface position (on top of grass_block, with 2 air blocks
// above) is restored to the same position.
func TestFindSafeSpawnFromValidPosition(t *testing.T) {
	w := New(0)
	// Save point: standing on grass at Y=3 → feet at Y=4, head at Y=5.
	x, y, z := 5.5, 4.0, 5.5
	sx, sy, sz := w.FindSafeSpawn(x, y, z)
	if sx != x || sy != y || sz != z {
		t.Errorf("valid position changed: got (%.1f, %.1f, %.1f), want (%.1f, %.1f, %.1f)",
			sx, sy, sz, x, y, z)
	}
}

// TestFindSafeSpawnFromUnderground verifies that a player saved
// underground (e.g. they dug a 1×1 hole and stood at the bottom) is
// teleported to the nearest safe surface. This is the
// user-reported regression "восстановило под землю и я застрял в
// блоках".
func TestFindSafeSpawnFromUnderground(t *testing.T) {
	w := New(0)
	// Stand inside solid stone at Y=-60 (between bedrock at Y=-64 and
	// dirt at Y=-63..2). Feet and head are both in stone.
	x, y, z := 5.0, -60.0, 5.0
	sx, sy, sz := w.FindSafeSpawn(x, y, z)

	// The result must NOT be (x, -60, z) — that's the bug.
	// It must be a safe position: 2 air blocks (feet + head) on top
	// of a non-air block.
	if int(sy) == int(y) {
		t.Errorf("FindSafeSpawn returned the original unsafe Y=%.0f — player would be stuck", sy)
	}
	// Verify the result is actually safe.
	px, pz := int(sx), int(sz)
	if int(sy)-1 < MinY || int(sy)+1 >= MinY+WorldHeight {
		t.Fatalf("safe spawn Y out of world range: %.0f", sy)
	}
	if w.GetBlock(px, int(sy), pz) != BlockAir {
		t.Errorf("feet block at (%.0f, %.0f, %.0f) is not air", sx, sy, sz)
	}
	if w.GetBlock(px, int(sy)+1, pz) != BlockAir {
		t.Errorf("head block at (%.0f, %.0f, %.0f) is not air", sx, sy+1, sz)
	}
	if w.GetBlock(px, int(sy)-1, pz) == BlockAir {
		t.Errorf("ground block at (%.0f, %.0f, %.0f) is air — player would fall", sx, sy-1, sz)
	}
}

// TestFindSafeSpawnFromAboveWorld verifies that a player saved way
// above the build limit (e.g. Y=400) falls back to the default
// spawn at (0.5, 4.0, 0.5) when no safe position is found in range.
func TestFindSafeSpawnFromAboveWorld(t *testing.T) {
	w := New(0)
	x, y, z := 5.0, 400.0, 5.0
	sx, sy, sz := w.FindSafeSpawn(x, y, z)
	if sx != 0.5 || sy != 4.0 || sz != 0.5 {
		t.Errorf("above-world spawn should fall back to (0.5, 4.0, 0.5), got (%.1f, %.1f, %.1f)",
			sx, sy, sz)
	}
}

func TestWorldFlusherSavesDirtyChunks(t *testing.T) {
	dir := t.TempDir()
	w := NewWithDir(0, dir)

	// Start a flusher with a fast interval so the test finishes quickly.
	stop := w.StartFlusher(20 * time.Millisecond)
	defer stop()

	w.SetBlock(5, 4, 5, BlockDirt)
	w.SetBlock(6, 4, 6, BlockStone)

	// Wait for the flusher to run.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !w.IsDirty(ChunkPos{X: 0, Z: 0}) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if w.IsDirty(ChunkPos{X: 0, Z: 0}) {
		t.Errorf("chunk (0,0) still dirty after 2s — flusher did not run")
	}
	if _, err := os.Stat(filepath.Join(dir, "chunks", "0_0.chunk")); err != nil {
		t.Errorf("chunk file missing after flusher: %v", err)
	}
}
