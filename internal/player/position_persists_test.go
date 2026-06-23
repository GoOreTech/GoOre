// Package player_test — end-to-end test: player position must persist
// across simulated server restarts.
//
// User-reported bug: "не сохраняется позиция игрока после перезахода
// на сервер" — "player's position doesn't persist after re-joining the
// server".
//
// We simulate the full lifecycle:
//  1. First session: player logs in (full handshake → config → play
//     flow), walks to a non-spawn position.
//  2. Server is asked to save the player (same code path as
//     SIGINT/SIGTERM shutdown → SaveAllPlayers → SavePlayer).
//  3. Fresh world + fresh Player (simulating server restart).
//  4. Second session: player logs in again with the same UUID.
//  5. After login completes, the Player's position MUST be the
//     saved one — NOT the default spawn (0.5, 4, 0.5).
package player_test

import (
	"net"
	"testing"
	"time"

	"goore/internal/config"
	"goore/internal/player"
	"goore/internal/protocol/v772"
	"goore/internal/world"
)

func TestPlayerPositionPersistsAcrossLogin(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.WorldDir = dir
	cfg.ViewDist = 2
	// Use a high port that is unlikely to be in use. (This is a
	// virtual pipe anyway; the port value only goes into the
	// handshake packet, it doesn't bind anything.)
	cfg.Port = 25565
	proto := v772.New()

	// ---------------- Phase 1: First session ----------------
	serverConn1, clientConn1 := net.Pipe()
	defer serverConn1.Close()
	defer clientConn1.Close()

	w1 := world.NewWithDir(0, dir)
	p1 := player.New(42, serverConn1, proto, w1, cfg)

	// Server-side: start the player FSM. We capture server-bound
	// packets in a channel and drive the client side from a separate
	// goroutine.
	serverPackets1 := make(chan serverPkt, 32)
	go func() {
		buf := make([]byte, 65536)
		accum := getAccum()
		_ = accum
		_ = buf
	}()

	// The read goroutine from test_helpers_test.go requires the
	// serverPkt type from this file. We replicate the loop here to
	// keep this test self-contained.
	go func() {
		buf := make([]byte, 65536)
		accum := []byte{}
		for {
			n, err := clientConn1.Read(buf)
			if err != nil {
				close(serverPackets1)
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
				// body is length-prefix varint(packetID) + payload
				pid, ok := tryReadVarIntLocal(body)
				if !ok {
					continue
				}
				pidSize := varIntSizeLocal(pid)
				payload := body[pidSize:]
				serverPackets1 <- serverPkt{id: pid, data: payload}
			}
		}
	}()

	go p1.HandleConn()

	// Drive the client side: handshake → login → config → play.
	handshakeAndLogin(t, clientConn1, cfg)
	configAckAndEnterPlay(t, clientConn1, serverPackets1)

	// Wait for play state.
	if !waitForPlay(t, p1, 3*time.Second) {
		t.Fatal("phase 1: player never reached play state")
	}

	// Move player far from spawn. Use block-center coords
	// (X.5, Y integer, Z.5) to avoid the FindSafeSpawn validator
	// normalizing block-boundary positions to the block center.
	p1.SetPositionForTest(123.5, 4, -42.5, 90.0, 0.0)
	t.Logf("phase 1: player at (%.2f, %.2f, %.2f)", p1.X, p1.Y, p1.Z)

	// Save (this is what cmd/server/main.go's signal handler does).
	if err := player.SavePlayer(dir, p1); err != nil {
		t.Fatalf("SavePlayer: %v", err)
	}
	// Close first connection.
	serverConn1.Close()
	clientConn1.Close()

	// ---------------- Phase 2: Server restart ----------------
	// Fresh world (same dir on disk so we pick up the saved player
	// file), fresh Player struct. The Player is constructed with
	// default position (0.5, 4, 0.5) by player.New.
	w2 := world.NewWithDir(0, dir)
	serverConn2, clientConn2 := net.Pipe()
	defer serverConn2.Close()
	defer clientConn2.Close()

	p2 := player.New(43, serverConn2, proto, w2, cfg)

	// Sanity: the new Player starts with default spawn position.
	if p2.X == 123.5 {
		t.Fatalf("fresh player already at saved position — test setup wrong")
	}

	// Capture server-bound packets from the second session.
	serverPackets2 := make(chan serverPkt, 32)
	go func() {
		buf := make([]byte, 65536)
		accum := []byte{}
		for {
			n, err := clientConn2.Read(buf)
			if err != nil {
				close(serverPackets2)
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
				serverPackets2 <- serverPkt{id: pid, data: payload}
			}
		}
	}()

	go p2.HandleConn()

	// Drive the second login. handleLogin → LoadStateFromDisk
	// should restore the saved position.
	handshakeAndLogin(t, clientConn2, cfg)
	configAckAndEnterPlay(t, clientConn2, serverPackets2)

	if !waitForPlay(t, p2, 3*time.Second) {
		t.Fatal("phase 2: player never reached play state")
	}

	// ---------------- Phase 3: Assertions ----------------
	if p2.X != 123.5 {
		t.Errorf("X = %v, want 123.5 (saved position was not loaded on login)", p2.X)
	}
	if p2.Y != 4 {
		t.Errorf("Y = %v, want 4", p2.Y)
	}
	if p2.Z != -42.5 {
		t.Errorf("Z = %v, want -42.5", p2.Z)
	}
	if p2.Yaw != 90.0 {
		t.Errorf("Yaw = %v, want 90.0", p2.Yaw)
	}

	// Verify the same UUID (sanity).
	if p2.UUID != p1.UUID {
		t.Errorf("UUID mismatch: phase 2 = %x, phase 1 = %x", p2.UUID, p1.UUID)
	}
}

func waitForPlay(t *testing.T, p *player.Player, timeout time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if p.IsInPlayState() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// tryReadVarIntLocal and varIntSizeLocal are local copies of the
// helpers in the test file. We don't import them because they're
// package-private.
func tryReadVarIntLocal(b []byte) (int32, bool) {
	var result int32
	var shift uint
	for _, by := range b {
		result |= int32(by&0x7F) << shift
		if by&0x80 == 0 {
			return result, true
		}
		shift += 7
		if shift > 35 {
			return 0, false
		}
	}
	return 0, false
}

func varIntSizeLocal(v int32) int {
	n := 1
	uv := uint32(v)
	for uv >= 0x80 {
		uv >>= 7
		n++
	}
	return n
}

// getAccum is a placeholder for a no-op init.
func getAccum() []byte { return nil }
