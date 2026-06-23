// Package config provides server configuration loading.
package config

import "time"

// Config holds all server configuration.
type Config struct {
	Port       int
	MOTD       string
	Offline    bool
	ViewDist   int32
	Seed       int64
	MaxPlayers int
	// WorldDir is the directory where world chunks and player data are
	// stored. Empty means persistence is disabled (vanilla single-session).
	WorldDir string
	// SaveInterval is how often the background flusher saves dirty
	// chunks to disk. Zero disables the flusher (manual save only).
	// Phase 3.3: was `config.Duration` (a typedef around
	// `time.Duration`); replaced with the standard-library type so
	// that `time.Duration` arithmetic, `time.ParseDuration` for
	// future JSON config, and `flag.Duration` in cmd/server all
	// work without an explicit conversion.
	SaveInterval time.Duration
	// SaveOnDisconnect saves a player's data on disconnect.
	SaveOnDisconnect bool
	// Gamemode is the default gamemode for new players. 0=survival, 1=creative,
	// 2=adventure, 3=spectator. Default 1 (creative) preserves pre-Phase-5
	// behaviour. Loaded player files override this with their saved gamemode.
	Gamemode uint8
}

// DefaultConfig returns the default server configuration.
func DefaultConfig() Config {
	return Config{
		Port:             25565,
		MOTD:             "GoOre Minecraft Server",
		Offline:          true,
		ViewDist:         8,
		Seed:             0,
		MaxPlayers:       20,
		WorldDir:         "",
		SaveInterval:     5 * time.Minute,
		SaveOnDisconnect: true,
		Gamemode:         1, // creative (pre-Phase-5 default)
	}
}
