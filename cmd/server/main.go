// Package main is the entry point for the GoOre Minecraft server.
// Targets Minecraft 1.21.8 (protocol 772).
//
// Usage:
//
//	go run ./cmd/server [flags]
//	  -port 25565            listen port
//	  -world ./world         world persistence directory
//	  -save-interval 5m      periodic world save interval
//	  -offline               offline mode (no auth, default true)
//	  -view-distance 8       chunk view distance
//	  -seed 0                world seed
//	  -max-players 20        max simultaneous players
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"goore/internal/config"
	"goore/internal/server"
)

func main() {
	port := flag.Int("port", 25565, "listen port")
	worldDir := flag.String("world", "", "world persistence directory (empty = no persistence)")
	saveInterval := flag.Duration("save-interval", 5*time.Minute, "periodic world save interval")
	offline := flag.Bool("offline", true, "offline mode (no auth)")
	viewDist := flag.Int("view-distance", 8, "chunk view distance")
	seed := flag.Int64("seed", 0, "world seed")
	maxPlayers := flag.Int("max-players", 20, "max simultaneous players")
	gamemode := flag.Int("gamemode", 1, "default gamemode: 0=survival, 1=creative, 2=adventure, 3=spectator")
	flag.Parse()
	slog.Error("gamemode", "type", *gamemode)
	cfg := config.DefaultConfig()
	cfg.Port = *port
	cfg.WorldDir = *worldDir
	cfg.SaveInterval = *saveInterval
	cfg.Offline = *offline
	cfg.ViewDist = int32(*viewDist)
	cfg.Seed = *seed
	cfg.MaxPlayers = *maxPlayers
	cfg.Gamemode = uint8(*gamemode)
	cfg.SaveOnDisconnect = true

	addr := fmt.Sprintf(":%d", cfg.Port)
	l, err := net.Listen("tcp", addr)
	if err != nil {
		slog.Error("listen failed", "addr", addr, "err", err)
		os.Exit(1)
	}
	defer l.Close()

	if cfg.WorldDir != "" {
		if err := os.MkdirAll(cfg.WorldDir, 0o755); err != nil {
			slog.Error("mkdir world dir failed", "path", cfg.WorldDir, "err", err)
			os.Exit(1)
		}
		slog.Info("world persistence enabled",
			"path", cfg.WorldDir, "save_interval", cfg.SaveInterval)
	}

	slog.Info("GoOre listening", "addr", addr, "seed", cfg.Seed, "view_dist", cfg.ViewDist)

	srv := server.New(cfg)

	// ctx-based graceful shutdown. Signal handler cancels ctx; Serve closes the listener, drains connections, and returns.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		slog.Info("received signal; initiating graceful shutdown", "signal", sig)
		cancel()
	}()

	if err := srv.Serve(ctx, l); err != nil {
		slog.Error("serve failed", "err", err)
		os.Exit(1)
	}

	// Final save: catch any chunks dirty after the last OnDisconnect flush
	// (e.g. world edits by a player who never reached statePlay).
	slog.Info("server stopped; doing final save")
	srv.SaveAllPlayers()
	if err := srv.SaveAll(); err != nil {
		slog.Warn("world save on shutdown failed", "err", err)
	}
}
