// Package inspect — tests for the world loader. The loader is the
// read-only data layer that powers the TUI: it reads world.meta,
// enumerates player files, counts chunks, and reports per-file
// errors instead of bailing on the first one (a single corrupt
// file should not prevent inspecting the rest of the world).
package inspect

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// writeWorldMeta writes a valid world.meta file with the given seed.
func writeWorldMeta(t *testing.T, dir string, seed int64) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	const worldMetaSize = 4 + 1 + 8 + 16
	buf := make([]byte, worldMetaSize)
	// Magic "GOOM" = 0x4D 0x4F 0x4F 0x47 = 0x4D4F4F47 (little-endian)
	binary.LittleEndian.PutUint32(buf[0:], 0x4D4F4F47)
	buf[4] = 1
	binary.LittleEndian.PutUint64(buf[5:], uint64(seed))
	if err := os.WriteFile(filepath.Join(dir, "world.meta"), buf, 0o644); err != nil {
		t.Fatalf("write world.meta: %v", err)
	}
}

// TestLoadWorld_HappyPath: valid world.meta + 2 players + 1 chunk.
// Loader returns seed, player count, chunk count, and no errors.
func TestLoadWorld_HappyPath(t *testing.T) {
	dir := t.TempDir()
	writeWorldMeta(t, dir, 12345)

	// Create 2 player files using SavePlayer through a stub player.
	// We write raw PLOR files directly to keep the test independent
	// of the player package's helpers.
	writeStubPlayer(t, dir, "Steve", 0x0a, 1.5, 4.0, -2.5, 0, 1, 9)
	writeStubPlayer(t, dir, "Alex", 0x0b, 0.5, 4.0, 0.5, 0, 85, 0)

	// Create a chunk file (we don't need it to be valid, just to
	// exist and have a file size for the loader to count).
	if err := os.MkdirAll(filepath.Join(dir, "chunks"), 0o755); err != nil {
		t.Fatalf("mkdir chunks: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "chunks", "0_0.chunk"), make([]byte, 196725), 0o644); err != nil {
		t.Fatalf("write chunk: %v", err)
	}

	got, err := LoadWorld(dir)
	if err != nil {
		t.Fatalf("LoadWorld: %v", err)
	}
	if got.Seed != 12345 {
		t.Errorf("Seed = %d, want 12345", got.Seed)
	}
	if len(got.Players) != 2 {
		t.Errorf("Players count = %d, want 2", len(got.Players))
	}
	// Players should be sorted alphabetically by name.
	if got.Players[0].Name != "Alex" {
		t.Errorf("Players[0].Name = %q, want %q", got.Players[0].Name, "Alex")
	}
	if got.Players[1].Name != "Steve" {
		t.Errorf("Players[1].Name = %q, want %q", got.Players[1].Name, "Steve")
	}
	if got.ChunkCount != 1 {
		t.Errorf("ChunkCount = %d, want 1", got.ChunkCount)
	}
	if got.ChunkBytes != 196725 {
		t.Errorf("ChunkBytes = %d, want 196725", got.ChunkBytes)
	}
	if len(got.Errors) != 0 {
		t.Errorf("Errors = %v, want empty", got.Errors)
	}
}

// TestLoadWorld_MissingDir: dir does not exist — must fail with a
// descriptive error.
func TestLoadWorld_MissingDir(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "does-not-exist")
	_, err := LoadWorld(missing)
	if err == nil {
		t.Fatal("LoadWorld(missing): expected error, got nil")
	}
	// The error message should mention the directory.
	if got := err.Error(); !contains(got, "world.meta") && !contains(got, missing) {
		t.Errorf("error = %q, want it to mention 'world.meta' or %q", got, missing)
	}
}

// TestLoadWorld_MissingMeta: dir exists but has no world.meta — fail.
func TestLoadWorld_MissingMeta(t *testing.T) {
	dir := t.TempDir()
	// Create the players dir but no world.meta.
	if err := os.MkdirAll(filepath.Join(dir, "players"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	_, err := LoadWorld(dir)
	if err == nil {
		t.Fatal("LoadWorld without world.meta: expected error, got nil")
	}
}

// TestLoadWorld_CorruptPlayerSkipped: one valid player + one corrupt
// player file. The valid player must be in the result, the corrupt
// one must be in Errors.
func TestLoadWorld_CorruptPlayerSkipped(t *testing.T) {
	dir := t.TempDir()
	writeWorldMeta(t, dir, 7)

	writeStubPlayer(t, dir, "Valid", 0x01, 0, 4, 0, 0, 1, 0)
	// Write a corrupt player file (wrong magic).
	corruptPath := filepath.Join(dir, "players", "aabbccddeeff00112233445566778899.dat")
	if err := os.WriteFile(corruptPath, []byte("NOPENOPE"), 0o644); err != nil {
		t.Fatalf("write corrupt: %v", err)
	}

	got, err := LoadWorld(dir)
	if err != nil {
		t.Fatalf("LoadWorld: %v", err)
	}
	if len(got.Players) != 1 {
		t.Errorf("Players count = %d, want 1", len(got.Players))
	}
	if len(got.Players) > 0 && got.Players[0].Name != "Valid" {
		t.Errorf("Players[0].Name = %q, want %q", got.Players[0].Name, "Valid")
	}
	if len(got.Errors) != 1 {
		t.Errorf("Errors count = %d, want 1", len(got.Errors))
	}
}

// TestLoadWorld_TmpFilesSkipped: an in-progress atomic write leaves
// a .tmp file. The loader must NOT try to parse it.
func TestLoadWorld_TmpFilesSkipped(t *testing.T) {
	dir := t.TempDir()
	writeWorldMeta(t, dir, 7)
	writeStubPlayer(t, dir, "Real", 0x01, 0, 4, 0, 0, 1, 0)
	// Write a .tmp file (atomic write in progress) — must be ignored.
	if err := os.MkdirAll(filepath.Join(dir, "players"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	tmpPath := filepath.Join(dir, "players", "99999999999999999999999999999999.dat.tmp")
	if err := os.WriteFile(tmpPath, []byte("garbage"), 0o644); err != nil {
		t.Fatalf("write tmp: %v", err)
	}

	got, err := LoadWorld(dir)
	if err != nil {
		t.Fatalf("LoadWorld: %v", err)
	}
	if len(got.Players) != 1 {
		t.Errorf("Players count = %d, want 1 (.tmp must be skipped)", len(got.Players))
	}
}

// TestLoadWorld_AlphabeticalSort: 3 players with names that aren't
// in alphabetical order on disk. Loader must sort A→Z.
func TestLoadWorld_AlphabeticalSort(t *testing.T) {
	dir := t.TempDir()
	writeWorldMeta(t, dir, 7)
	// Write in non-alphabetical order: Zebra, Alpha, Mike.
	writeStubPlayer(t, dir, "Zebra", 0x01, 0, 4, 0, 0, 1, 0)
	writeStubPlayer(t, dir, "Alpha", 0x02, 0, 4, 0, 0, 1, 0)
	writeStubPlayer(t, dir, "Mike", 0x03, 0, 4, 0, 0, 1, 0)

	got, err := LoadWorld(dir)
	if err != nil {
		t.Fatalf("LoadWorld: %v", err)
	}
	if len(got.Players) != 3 {
		t.Fatalf("Players count = %d, want 3", len(got.Players))
	}
	want := []string{"Alpha", "Mike", "Zebra"}
	for i, p := range got.Players {
		if p.Name != want[i] {
			t.Errorf("Players[%d].Name = %q, want %q", i, p.Name, want[i])
		}
	}
}

// --- helpers ---

// writeStubPlayer writes a minimal valid PLOR player file directly.
// UUID is the first byte (the rest is zero), name is the player name,
// position is (x, y, z), heldSlot is the slot index, hotbar[heldSlot] is set to heldID,
// and hotbar[1] is set to slot1ID. This is enough to verify loading
// and sorting; we don't need a fully round-trip test here.
func writeStubPlayer(t *testing.T, dir string, name string, uuidFirstByte byte, x, y, z float64, heldSlot int, heldID int32, slot1ID int32) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "players"), 0o755); err != nil {
		t.Fatalf("mkdir players: %v", err)
	}

	// Build UUID (first byte set, rest zero).
	uuid := [16]byte{}
	uuid[0] = uuidFirstByte
	// Filename: 32 hex chars.
	hex := make([]byte, 32)
	const hexchars = "0123456789abcdef"
	for i := 0; i < 16; i++ {
		hex[i*2] = hexchars[uuid[i]>>4]
		hex[i*2+1] = hexchars[uuid[i]&0x0F]
	}
	filename := string(hex) + ".dat"
	path := filepath.Join(dir, "players", filename)

	// Build PLOR content: magic(4) + ver(1) + nameLen(1) + name + uuid(16) + xyz(24) + yaw/pitch(8) + onGround(1) + hotbar(36) + heldSlot(4)
	if len(name) > 16 {
		t.Fatalf("name too long: %d", len(name))
	}
	size := 4 + 1 + 1 + len(name) + 16 + 24 + 8 + 1 + 36 + 4
	buf := make([]byte, size)
	off := 0
	binary.LittleEndian.PutUint32(buf[off:], 0x524F4C50) // "PLOR"
	off += 4
	buf[off] = 1
	off++
	buf[off] = uint8(len(name))
	off++
	copy(buf[off:], name)
	off += len(name)
	copy(buf[off:], uuid[:])
	off += 16
	binary.LittleEndian.PutUint64(buf[off:], uint64Float64(x))
	off += 8
	binary.LittleEndian.PutUint64(buf[off:], uint64Float64(y))
	off += 8
	binary.LittleEndian.PutUint64(buf[off:], uint64Float64(z))
	off += 8
	binary.LittleEndian.PutUint32(buf[off:], 0) // yaw
	off += 4
	binary.LittleEndian.PutUint32(buf[off:], 0) // pitch
	off += 4
	buf[off] = 0 // onGround
	off++
	// hotbar 9 × int32
	for i := 0; i < 9; i++ {
		var v int32
		if i == heldSlot {
			v = heldID
		} else if i == 1 {
			v = slot1ID
		}
		binary.LittleEndian.PutUint32(buf[off:], uint32(v))
		off += 4
	}
	binary.LittleEndian.PutUint32(buf[off:], uint32(heldSlot))
	off += 4

	if err := os.WriteFile(path, buf, 0o644); err != nil {
		t.Fatalf("write player: %v", err)
	}
}

// uint64Float64 returns the bit pattern of a float64 as a uint64.
func uint64Float64(f float64) uint64 {
	return mathFloat64bits(f)
}

func mathFloat64bits(f float64) uint64 {
	return math.Float64bits(f)
}

// contains is a tiny contains check (avoids strings.Contains import noise).
func contains(haystack, needle string) bool {
	return len(needle) == 0 || (len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
