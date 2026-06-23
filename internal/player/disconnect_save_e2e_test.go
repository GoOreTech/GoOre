// Package player_test — end-to-end regression tests for two user-reported
// bugs around player persistence:
//
//  1. "не происходит сохранения информации об игроке, файлы не
//     создаются" — player files should be created on disconnect,
//     not only on server shutdown.
//  2. "некорректно восстанавливается позиция игрока, после
//     перезапуска сервера меня восстановило под землю и я
//     застрял в блоках" — a player saved at an unsafe position
//     (e.g. inside a solid block) must be teleported to a safe
//     spot on rejoin, not restored to the unsafe spot.
//
// Both regressions are exercised end-to-end through the real
// HandleConn FSM, so they catch bugs in the wiring (server →
// OnDisconnect callback, handleLogin → FindSafeSpawn call) that
// unit tests would miss.
package player_test

import (
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"goore/internal/config"
	"goore/internal/player"
	"goore/internal/protocol/v772"
	"goore/internal/world"
)

// TestPlayerSavesOnDisconnect_E2E: drives a real HandleConn session,
// closes the client connection (simulating disconnect), waits for
// HandleConn to return, and asserts the player file is on disk.
func TestPlayerSavesOnDisconnect_E2E(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.WorldDir = dir
	cfg.ViewDist = 2
	cfg.SaveOnDisconnect = true

	serverConn, clientConn := net.Pipe()

	w := world.NewWithDir(0, dir)
	proto := v772.New()
	p := player.New(42, serverConn, proto, w, cfg)

	// Wire up the OnDisconnect hook the same way server.go does.
	// (We don't go through the actual server.Accept loop here
	// because net.Pipe already gives us a connected pair; the
	// production code path is identical except for the accept
	// step.)
	if cfg.WorldDir != "" {
		d := cfg.WorldDir
		// Phase 2.2: install a PlayerHooks bundle that persists
		// the player file. The server-side serverPlayerHooks does
		// the same plus world.SaveAll + broadcast handling; this
		// test only cares about the save, so we install a
		// minimal struct that implements PlayerHooks.
		p.SetHooks(&saveOnlyHooks{saveDir: d})
	}

	// Drive the login + config FSM from the client side, then
	// close the client connection. The server's HandleConn
	// defer should fire and write the player file.
	serverPackets := make(chan serverPkt, 32)
	go readServerPackets(clientConn, serverPackets)

	done := make(chan struct{})
	go func() {
		defer close(done)
		p.HandleConn()
	}()

	handshakeAndLogin(t, clientConn, cfg)
	configAckAndEnterPlay(t, clientConn, serverPackets)

	if !waitForPlay(t, p, 3*time.Second) {
		t.Fatal("player never reached play state")
	}

	// Set a known position so we can verify it survives.
	p.SetPositionForTest(77.0, 4.0, -55.0, 45.0, 0.0)

	// Disconnect by closing the client side. HandleConn should
	// notice and return, firing the defer which calls OnDisconnect
	// → SavePlayer.
	clientConn.Close()

	// Wait for HandleConn to finish.
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("HandleConn did not return within 3s after client close")
	}

	// Verify the player file was created.
	pattern := filepath.Join(dir, "players", "*.dat")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(matches) == 0 {
		t.Fatalf("no player file created on disconnect (dir=%s, expected %s)", dir, pattern)
	}
	t.Logf("created player file: %s", matches[0])

	// Verify the file content has the position we set.
	uuid := [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	loaded, err := player.LoadPlayer(dir, uuid)
	if err != nil {
		t.Fatalf("LoadPlayer: %v", err)
	}
	if loaded.X != 77.0 {
		t.Errorf("X = %v, want 77.0", loaded.X)
	}
	if loaded.Z != -55.0 {
		t.Errorf("Z = %v, want -55.0", loaded.Z)
	}
	if loaded.Yaw != 45.0 {
		t.Errorf("Yaw = %v, want 45.0", loaded.Yaw)
	}
}

// TestUnsafeSavedPositionIsCorrected_E2E: a player saves while
// inside a solid block (e.g. they dug a 1×1 hole and stood at the
// bottom). On rejoin, the player must be teleported to a safe
// position above the saved spot, NOT the original unsafe Y.
func TestUnsafeSavedPositionIsCorrected_E2E(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.WorldDir = dir
	cfg.ViewDist = 2

	w := world.NewWithDir(0, dir)
	proto := v772.New()
	p1 := player.New(42, nil, proto, w, cfg)
	p1.UUID = [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	// Save the player at an unsafe Y=10 (which is air in the flat
	// world — that's safe, so the test would pass trivially).
	// To simulate the bug, we save the player at Y=-60 (underground,
	// in solid stone).
	p1.X = 5.5
	p1.Y = -60.0
	p1.Z = 5.5
	if err := player.SavePlayer(dir, p1); err != nil {
		t.Fatalf("SavePlayer: %v", err)
	}

	// Simulate rejoin: a fresh Player in a fresh world, with the
	// same UUID. handleLogin → LoadStateFromDisk → FindSafeSpawn.
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	w2 := world.NewWithDir(0, dir)
	p2 := player.New(43, serverConn, proto, w2, cfg)

	serverPackets := make(chan serverPkt, 32)
	go readServerPackets(clientConn, serverPackets)

	done := make(chan struct{})
	go func() {
		defer close(done)
		p2.HandleConn()
	}()

	handshakeAndLogin(t, clientConn, cfg)
	configAckAndEnterPlay(t, clientConn, serverPackets)

	if !waitForPlay(t, p2, 3*time.Second) {
		t.Fatal("player never reached play state")
	}

	// The unsafe Y=-60 must NOT be used. The player must be on
	// or above the surface.
	if p2.Y < 0 {
		t.Errorf("Y = %v, want >= 0 (player was saved underground and not moved to safety)", p2.Y)
	}
	// The chosen position must be in air (feet+head) above a solid
	// block — i.e. the player can actually stand there.
	px, pz := 5, 5
	if p2.Y-1 < world.MinY {
		t.Fatalf("chosen Y is below MinY: %v", p2.Y)
	}
	if w2.GetBlock(px, int(p2.Y), pz) != world.BlockAir {
		t.Errorf("feet block at (%.0f, %.0f, %.0f) is not air — player will spawn inside a block", p2.X, p2.Y, p2.Z)
	}
	if w2.GetBlock(px, int(p2.Y)+1, pz) != world.BlockAir {
		t.Errorf("head block at (%.0f, %.0f, %.0f) is not air", p2.X, p2.Y+1, p2.Z)
	}
	if w2.GetBlock(px, int(p2.Y)-1, pz) == world.BlockAir {
		t.Errorf("ground block at (%.0f, %.0f, %.0f) is air — player will fall", p2.X, p2.Y-1, p2.Z)
	}
}

// readServerPackets reads packets from conn and forwards them on the
// channel. Closes the channel when the conn returns an error.
func readServerPackets(conn net.Conn, out chan<- serverPkt) {
	defer close(out)
	buf := make([]byte, 65536)
	accum := []byte{}
	for {
		n, err := conn.Read(buf)
		if err != nil {
			return
		}
		accum = append(accum, buf[:n]...)
		for len(accum) > 0 {
			pktLen, ok := tryReadVarIntLocal(accum)
			if !ok {
				break
			}
			consumed := varIntSizeLocal(pktLen) + int(pktLen)
			if len(accum) < consumed {
				break
			}
			body := accum[varIntSizeLocal(pktLen):consumed]
			accum = accum[consumed:]
			pid, ok := tryReadVarIntLocal(body)
			if !ok {
				continue
			}
			pidSize := varIntSizeLocal(pid)
			payload := body[pidSize:]
			out <- serverPkt{id: pid, data: payload}
		}
	}
}

// Unused but keeps the import list valid in case we add more tests
// that use os.Stat directly.
var _ = os.Stat
