// Package world — chunk persistence to disk. Custom binary format; see docs/world.md §On-disk formats for the full layout.
package world

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
)

const (
	chunkMagic   uint32 = 0x434F4F47 // "GOOC" little-endian: 0x47 'O' 'O' 'C'
	chunkVersion uint8  = 1
)

// ChunkPath returns the on-disk path for a chunk: <dir>/chunks/<x>_<z>.chunk.
func ChunkPath(dir string, pos ChunkPos) string {
	return filepath.Join(dir, "chunks", fmt.Sprintf("%d_%d.chunk", pos.X, pos.Z))
}

// SaveChunk writes a chunk to disk atomically (temp file + rename). Creates the chunks directory if missing.
func SaveChunk(dir string, c *Chunk) error {
	path := ChunkPath(dir, c.Pos)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir chunks dir: %w", err)
	}

	buf := make([]byte, chunkFileSize)
	off := 0

	binary.LittleEndian.PutUint32(buf[off:], chunkMagic)
	off += 4
	buf[off] = chunkVersion
	off++
	binary.LittleEndian.PutUint32(buf[off:], uint32(c.Pos.X))
	off += 4
	binary.LittleEndian.PutUint32(buf[off:], uint32(c.Pos.Z))
	off += 4
	for si := 0; si < SectionsPerChunk; si++ {
		sec := &c.Sections[si]
		binary.LittleEndian.PutUint16(buf[off:], uint16(sec.BlockCount))
		off += 2
		for i := 0; i < BlocksPerSection; i++ {
			binary.LittleEndian.PutUint16(buf[off:], uint16(sec.Blocks[i]))
			off += 2
		}
	}
	crc := crc32.ChecksumIEEE(buf[:off])
	binary.LittleEndian.PutUint32(buf[off:], crc)
	off += 4

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, buf[:off], 0o644); err != nil {
		return fmt.Errorf("write temp chunk: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename chunk: %w", err)
	}
	return nil
}

// LoadChunk reads a chunk from disk. Returns an error if the file is missing or corrupt.
func LoadChunk(dir string, pos ChunkPos) (*Chunk, error) {
	path := ChunkPath(dir, pos)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read chunk: %w", err)
	}
	if len(data) != chunkFileSize {
		return nil, fmt.Errorf("chunk size = %d, want %d", len(data), chunkFileSize)
	}
	off := 0

	magic := binary.LittleEndian.Uint32(data[off:])
	off += 4
	if magic != chunkMagic {
		return nil, errors.New("bad chunk magic")
	}
	ver := data[off]
	off++
	if ver != chunkVersion {
		return nil, fmt.Errorf("unsupported chunk version %d", ver)
	}
	cx := int32(binary.LittleEndian.Uint32(data[off:]))
	off += 4
	cz := int32(binary.LittleEndian.Uint32(data[off:]))
	off += 4
	if cx != pos.X || cz != pos.Z {
		return nil, fmt.Errorf("chunk pos mismatch: file=(%d,%d), requested=(%d,%d)", cx, cz, pos.X, pos.Z)
	}

	wantCRC := binary.LittleEndian.Uint32(data[chunkFileSize-4:])
	gotCRC := crc32.ChecksumIEEE(data[:chunkFileSize-4])
	if gotCRC != wantCRC {
		return nil, fmt.Errorf("chunk CRC mismatch: got %08x, want %08x", gotCRC, wantCRC)
	}

	c := &Chunk{Pos: pos}
	for si := 0; si < SectionsPerChunk; si++ {
		sec := &c.Sections[si]
		sec.BlockCount = int16(binary.LittleEndian.Uint16(data[off:]))
		off += 2
		for i := 0; i < BlocksPerSection; i++ {
			sec.Blocks[i] = Block(binary.LittleEndian.Uint16(data[off:]))
			off += 2
		}
	}

	// Recompute derived state NOT stored on disk (Heightmap, SkyLight).
	// See docs/world.md §Load flow.
	c.UpdateHeightmap()
	c.RecomputeSkyLight()

	return c, nil
}

// chunkFileSize is the exact wire size of a chunk on disk.
const chunkFileSize = 4 + 1 + 4 + 4 + SectionsPerChunk*(2+BlocksPerSection*2) + 4

// World meta file format (<dir>/world.meta).
const (
	worldMetaMagic   uint32 = 0x4D4F4F47 // "GOOM"
	worldMetaVersion uint8  = 1
)

func worldMetaPath(dir string) string {
	return filepath.Join(dir, "world.meta")
}

// saveWorldMeta writes the world metadata file.
func saveWorldMeta(dir string, seed int64) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir world dir: %w", err)
	}
	const worldMetaSize = 4 + 1 + 8 + 16
	buf := make([]byte, worldMetaSize)
	off := 0
	binary.LittleEndian.PutUint32(buf[off:], worldMetaMagic)
	off += 4
	buf[off] = worldMetaVersion
	off++
	binary.LittleEndian.PutUint64(buf[off:], uint64(seed))
	off += 8
	// reserved: 16 zero bytes (already zeroed by make)
	tmp := worldMetaPath(dir) + ".tmp"
	if err := os.WriteFile(tmp, buf, 0o644); err != nil {
		return fmt.Errorf("write meta temp: %w", err)
	}
	if err := os.Rename(tmp, worldMetaPath(dir)); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename meta: %w", err)
	}
	return nil
}

// loadWorldMeta reads the world metadata file. Returns the seed.
func loadWorldMeta(dir string) (int64, error) {
	const worldMetaSize = 4 + 1 + 8 + 16
	data, err := os.ReadFile(worldMetaPath(dir))
	if err != nil {
		return 0, fmt.Errorf("read world meta: %w", err)
	}
	if len(data) != worldMetaSize {
		return 0, fmt.Errorf("world meta size = %d, want %d", len(data), worldMetaSize)
	}
	magic := binary.LittleEndian.Uint32(data[0:])
	if magic != worldMetaMagic {
		return 0, errors.New("bad world meta magic")
	}
	if data[4] != worldMetaVersion {
		return 0, fmt.Errorf("unsupported world meta version %d", data[4])
	}
	seed := int64(binary.LittleEndian.Uint64(data[5:]))
	return seed, nil
}

// LoadWorldMeta reads the world metadata file and returns the seed. Exported counterpart of loadWorldMeta for read-only tools (cmd/inspect).
func LoadWorldMeta(dir string) (int64, error) {
	return loadWorldMeta(dir)
}

// SaveAll writes all dirty chunks to disk and updates the world meta file. On success, all chunks are marked clean.
func (w *World) SaveAll() error {
	if w.dir == "" {
		return errors.New("world has no persistence directory")
	}
	w.mu.Lock()
	dirtyList := make([]ChunkPos, 0, len(w.dirty))
	for pos := range w.dirty {
		dirtyList = append(dirtyList, pos)
	}
	w.mu.Unlock()

	// Write each dirty chunk. Don't hold the lock while writing.
	var firstErr error
	for _, pos := range dirtyList {
		w.mu.RLock()
		c, ok := w.chunks[pos]
		w.mu.RUnlock()
		if !ok {
			continue
		}
		if err := SaveChunk(w.dir, c); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		w.mu.Lock()
		delete(w.dirty, pos)
		w.mu.Unlock()
	}

	// Always write the meta so the seed is in sync.
	if err := saveWorldMeta(w.dir, w.Seed); err != nil && firstErr == nil {
		firstErr = err
	}
	return firstErr
}

// LoadAll reads the world meta and discovers existing chunk files. Loaded chunks are not yet in memory — they are loaded lazily on first GetChunk(pos).
func (w *World) LoadAll() error {
	if w.dir == "" {
		return errors.New("world has no persistence directory")
	}
	seed, err := loadWorldMeta(w.dir)
	if err != nil {
		return err
	}
	w.Seed = seed
	return nil
}

// LoadedChunks returns the list of chunk files present on disk.
func LoadedChunks(dir string) ([]ChunkPos, error) {
	chunksDir := filepath.Join(dir, "chunks")
	entries, err := os.ReadDir(chunksDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []ChunkPos
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		var x, z int32
		n, err := fmt.Sscanf(e.Name(), "%d_%d.chunk", &x, &z)
		if err != nil || n != 2 {
			continue
		}
		out = append(out, ChunkPos{X: x, Z: z})
	}
	return out, nil
}
