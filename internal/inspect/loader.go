// Package inspect — read-only world loader. The data layer that powers the TUI. Per-file errors are accumulated; see docs/inspect.md.
package inspect

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"goore/internal/player"
	"goore/internal/world"
)

// PlayerInfo is the inspector's view of a single player. We copy from *player.Player so the inspect package doesn't leak references to the live struct.
type PlayerInfo struct {
	Name     string
	UUID     [16]byte
	X, Y, Z  float64
	Yaw      float32
	Pitch    float32
	OnGround bool
	Hotbar   [9]int32
	HeldSlot int
	HeldItem int32
}

// WorldData is the read-only snapshot of a world that powers the TUI. All fields are populated by LoadWorld; mutations are not supported.
type WorldData struct {
	Dir        string
	Seed       int64
	Players    []PlayerInfo
	ChunkCount int
	ChunkBytes int64
	Errors     []LoadError
}

// LoadError describes a single per-file load failure.
type LoadError struct {
	Path string
	Err  error
}

// Error returns "path: underlying error" for log output.
func (e LoadError) Error() string {
	return fmt.Sprintf("%s: %v", e.Path, e.Err)
}

// LoadWorld reads a world directory. Structural errors (missing dir, missing world.meta, bad magic) return as a non-nil error. Per-file errors (corrupt player file, unreadable chunk) are collected in WorldData.Errors.
func LoadWorld(dir string) (*WorldData, error) {
	// 1. Verify the directory exists and is a directory.
	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("world directory %s does not exist", dir)
		}
		return nil, fmt.Errorf("stat world dir: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", dir)
	}

	wd := &WorldData{Dir: dir}

	// 2. Read world.meta — fail-fast if missing or corrupt.
	seed, err := world.LoadWorldMeta(dir)
	if err != nil {
		return nil, fmt.Errorf("load world meta: %w", err)
	}
	wd.Seed = seed

	// 3. Enumerate players. Per-file errors accumulated in wd.Errors.
	players, perrs := listPlayers(dir)
	wd.Players = players
	for _, e := range perrs {
		wd.Errors = append(wd.Errors, LoadError{Path: e.path, Err: e.err})
	}

	// 4. Count chunks. Same policy.
	n, bytes, cerrs := countChunks(dir)
	wd.ChunkCount = n
	wd.ChunkBytes = bytes
	for _, e := range cerrs {
		wd.Errors = append(wd.Errors, LoadError{Path: e.path, Err: e.err})
	}

	return wd, nil
}

// fileErr is an internal helper for accumulating per-file errors.
type fileErr struct {
	path string
	err  error
}

// listPlayers reads <dir>/players, skips .tmp files (atomic writes in progress) and non-.dat files, then loads each .dat via player.LoadPlayer. Loaded players are sorted A→Z by Name.
func listPlayers(dir string) ([]PlayerInfo, []fileErr) {
	playersDir := filepath.Join(dir, "players")
	entries, err := os.ReadDir(playersDir)
	if err != nil {
		if os.IsNotExist(err) {
			// No players dir yet — valid state.
			return nil, nil
		}
		return nil, []fileErr{{path: playersDir, err: err}}
	}

	// First pass: collect (filename, path, parsed UUID) for every .dat file. Skip .tmp.
	type entry struct {
		filename string
		path     string
		uuid     [16]byte
	}
	var valid []entry
	var perrs []fileErr
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(name, ".tmp") {
			continue // atomic write in progress
		}
		if !strings.HasSuffix(name, ".dat") {
			continue // stray file
		}
		// UUID is 32 hex chars before .dat.
		hexPart := strings.TrimSuffix(name, ".dat")
		if len(hexPart) != 32 {
			continue // not a UUID-shaped filename
		}
		var uuid [16]byte
		ok := true
		for i := 0; i < 16; i++ {
			b, err := hexByte(hexPart[i*2 : i*2+2])
			if err != nil {
				perrs = append(perrs, fileErr{
					path: filepath.Join(playersDir, name),
					err:  fmt.Errorf("bad uuid in filename: %w", err),
				})
				ok = false
				break
			}
			uuid[i] = b
		}
		if !ok {
			continue
		}
		valid = append(valid, entry{
			filename: name,
			path:     filepath.Join(playersDir, name),
			uuid:     uuid,
		})
	}

	// Second pass: load each player. Re-sort by Name (filename is UUID-based).
	result := make([]PlayerInfo, 0, len(valid))
	for _, e := range valid {
		p, err := player.LoadPlayer(dir, e.uuid)
		if err != nil {
			perrs = append(perrs, fileErr{path: e.path, err: err})
			continue
		}
		result = append(result, playerToInfo(p))
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result, perrs
}

// playerToInfo copies a *player.Player into a PlayerInfo. Breaks the dependency on the live player struct.
func playerToInfo(p *player.Player) PlayerInfo {
	return PlayerInfo{
		Name:     p.Name,
		UUID:     p.UUID,
		X:        p.X,
		Y:        p.Y,
		Z:        p.Z,
		Yaw:      p.Yaw,
		Pitch:    p.Pitch,
		OnGround: p.OnGround,
		Hotbar:   p.Hotbar,
		HeldSlot: p.HeldSlot,
		HeldItem: p.HeldItem,
	}
}

// countChunks reads <dir>/chunks and returns the number of .chunk files plus their total size in bytes. Skips .tmp.
func countChunks(dir string) (int, int64, []fileErr) {
	chunksDir := filepath.Join(dir, "chunks")
	entries, err := os.ReadDir(chunksDir)
	if err != nil {
		if os.IsNotExist(err) {
			// No chunks dir yet — valid state for a fresh world.
			return 0, 0, nil
		}
		return 0, 0, []fileErr{{path: chunksDir, err: err}}
	}
	var n int
	var total int64
	var perrs []fileErr
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.HasSuffix(e.Name(), ".tmp") {
			continue // atomic write in progress
		}
		info, err := e.Info()
		if err != nil {
			perrs = append(perrs, fileErr{path: filepath.Join(chunksDir, e.Name()), err: err})
			continue
		}
		n++
		total += info.Size()
	}
	return n, total, perrs
}

// hexByte parses a 2-character hex string into a byte. Both cases accepted.
func hexByte(s string) (byte, error) {
	if len(s) != 2 {
		return 0, errors.New("hex string must be 2 chars")
	}
	var b byte
	for i := 0; i < 2; i++ {
		c := s[i]
		var v byte
		switch {
		case c >= '0' && c <= '9':
			v = c - '0'
		case c >= 'a' && c <= 'f':
			v = c - 'a' + 10
		case c >= 'A' && c <= 'F':
			v = c - 'A' + 10
		default:
			return 0, errors.New("invalid hex char")
		}
		b = b*16 + v
	}
	return b, nil
}
