// Package player_test — regression tests for player movement packets
// being silently dropped (logged as "Unknown packet 0x1D").
//
// User-reported bug: "I move around, exit, and the inspector shows
// position (0, 0, 0)" — and server logs are flooded with:
//
//	Unknown packet 0x1D from <name>
//	Unknown packet 0x1E from <name>
//	Unknown packet 0x1F from <name>
//	Unknown packet 0x20 from <name>
//	Unknown packet 0x0C from <name>
//	Unknown packet 0x29 from <name>
//	Unknown packet 0x2A from <name>
//
// Root cause was twofold:
//  1. In 1.21.8 the IDs for `position` and `position_look` were SWAPPED
//     vs 1.20.x. We had the wrong IDs (0x1D=0x1E) in packetids.go.
//  2. The `PacketIDs().Serverbound` map used keys like
//     "player_position" while the handler in handlePlay() looked up
//     "set_player_position". A missing key returns 0, and no
//     case matched, so the packet was logged as "Unknown" and the
//     position was never updated.
//
// These tests drive the FSM via net.Pipe, send movement packets
// with the REAL 1.21.8 wire IDs (hardcoded as 0x1D / 0x1E, not
// constants), and assert that the Player's X/Y/Z/Yaw/Pitch are
// updated. They are hard regression tests: removing the case
// from handlePlay() (or re-swapping the IDs) makes them fail with
// the exact user symptom.
package player_test

import (
	"bytes"
	"log/slog"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"goore/internal/config"
	"goore/internal/player"
	"goore/internal/protocol"
	"goore/internal/protocol/v772"
	"goore/internal/world"
)

// syncBuffer is a thread-safe wrapper around bytes.Buffer for use as
// a log.SetOutput target in tests where the server's HandleConn
// goroutine writes to it concurrently with the test goroutine
// reading. bytes.Buffer alone is not safe for concurrent use.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// driveFSM is a small helper that drives the full handshake →
// login → config → play flow on the client side, then waits for
// the player to enter the play state. It does NOT drain chunks —
// the chunk-batch mechanism doesn't send a final ack from the
// client, so drainChunks can block indefinitely on a busy pipe.
// We only need the player in Play state to send movement packets.
//
// The pipe is set up so that:
//   - serverConn is the side the Player reads from (server-side)
//   - clientConn is the side the test writes to (client-side)
//
// `handshakeAndLogin` / `configAckAndEnterPlay` write on
// clientConn (the client sends). The Player reads on serverConn.
func driveFSM(t *testing.T, serverConn, clientConn net.Conn, cfg *config.Config) *player.Player {
	t.Helper()
	w := world.NewWithDir(42, "")
	proto := v772.New()
	p := player.New(42, serverConn, proto, w, *cfg)

	done := make(chan struct{})
	go func() {
		defer close(done)
		p.HandleConn()
	}()

	fsmPackets := make(chan serverPkt, 1024)
	go readServerPackets(clientConn, fsmPackets)

	handshakeAndLogin(t, clientConn, *cfg)
	configAckAndEnterPlay(t, clientConn, fsmPackets)

	if !waitForPlay(t, p, 3*time.Second) {
		t.Fatal("player never reached play state")
	}
	return p
}

// TestPositionPacketUpdatesCoordinates is the core regression test.
// It sends a real 1.21.8 wire `position` packet (0x1D) with new
// x/y/z, and asserts the Player's X/Y/Z are updated. If the packet
// is being silently dropped (the bug), the Player's position stays
// at the default (0, 0, 0).
//
// We HARDCODED the wire ID 0x1D in the test, not the constant, so
// that changes to the constant don't accidentally mask the
// regression.
func TestPositionPacketUpdatesCoordinates(t *testing.T) {

	cfg := config.DefaultConfig()
	cfg.WorldDir = ""
	cfg.ViewDist = 2

	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	p := driveFSM(t, serverConn, clientConn, &cfg)

	// 1.21.8 `position` packet body: x(f64) y(f64) z(f64) flags(u8)
	var w protocol.WireWriter
	w.Float64(10.5)
	w.Float64(4.0)
	w.Float64(-3.5)
	w.Byte(0x01) // on_ground
	if w.Err() != nil {
		t.Fatalf("writer: %v", w.Err())
	}
	pkt := protocol.MakePacket(0x1D, w.Bytes()) // HARDCODED 0x1D

	// clientConn is the client-side of the pipe; writes from it are
	// read by the server (Player.HandleConn reads from serverConn).
	if _, err := clientConn.Write(pkt); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Give the server a moment to process. The spawn position is
	// (0.5, 4, 0.5) so we must check for the new target (10.5), not
	// for 0. Polled via p.Pos() (not p.X directly) to avoid the
	// data race on the X/Y/Z fields that the position-packet
	// handler writes to. waitFor returns early as soon as the
	// packet handler updates the field — the pre-Phase-3.6 version
	// always slept a full 2 s in the failure case AND a full
	// 10 ms × N in the success case.
	if !waitFor(t, 2*time.Second, func() bool {
		px, _, _, _, _, _ := p.Pos()
		return px == 10.5
	}) {
		px, _, _, _, _, _ := p.Pos()
		t.Errorf("X = %v, want 10.5 (position packet dropped!)", px)
	}
	_, py, pz, _, _, _ := p.Pos()
	if py != 4.0 {
		t.Errorf("Y = %v, want 4.0", py)
	}
	if pz != -3.5 {
		t.Errorf("Z = %v, want -3.5 (position packet dropped!)", pz)
	}
}

// TestPositionLookPacketUpdatesEverything: send a position_look
// packet (0x1E in 1.21.8) with new x,y,z,yaw,pitch and assert all
// 5 fields are updated. This catches the swapped ID bug too —
// if 0x1E were still mapped to "set_player_position" (the 1.20.x
// assignment), the handler would call handlePosition() which
// reads only 4 fields and the wire reader would be off by 6 bytes
// for the look fields.
func TestPositionLookPacketUpdatesEverything(t *testing.T) {

	cfg := config.DefaultConfig()
	cfg.WorldDir = ""
	cfg.ViewDist = 2

	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	p := driveFSM(t, serverConn, clientConn, &cfg)

	// 1.21.8 position_look body: x(f64) y(f64) z(f64) yaw(f32) pitch(f32) flags(u8)
	var w protocol.WireWriter
	w.Float64(20.0)
	w.Float64(64.0)
	w.Float64(-8.0)
	w.Float32(45.0) // yaw
	w.Float32(-15.0)
	w.Byte(0x01) // on_ground
	if w.Err() != nil {
		t.Fatalf("writer: %v", w.Err())
	}
	pkt := protocol.MakePacket(0x1E, w.Bytes()) // HARDCODED 0x1E

	if _, err := clientConn.Write(pkt); err != nil {
		t.Fatalf("write: %v", err)
	}

	if !waitFor(t, 2*time.Second, func() bool {
		px, _, _, _, _, _ := p.Pos()
		return px == 20.0
	}) {
		px, _, _, _, _, _ := p.Pos()
		t.Errorf("X = %v, want 20.0 (position_look packet dropped!)", px)
	}
	_, py, pz, yaw, pitch, _ := p.Pos()
	if py != 64.0 {
		t.Errorf("Y = %v, want 64.0", py)
	}
	if pz != -8.0 {
		t.Errorf("Z = %v, want -8.0", pz)
	}
	if yaw != 45.0 {
		t.Errorf("Yaw = %v, want 45.0", yaw)
	}
	if pitch != -15.0 {
		t.Errorf("Pitch = %v, want -15.0", pitch)
	}
}

// TestUnknownPacketLogIsGone: a "position" packet must NOT be logged
// as "Unknown packet 0x1D" any more. We capture log output and
// assert that 0x1D/0x1E/0x1F/0x20/0x29/0x2A/0x0C are not in the
// unknown-packet list. (This catches future regressions where
// someone moves a handler into a default case again.)
func TestUnknownPacketLogIsGone(t *testing.T) {
	// logBuf needs to be safe for concurrent Write (from the
	// server's log.Printf) and String (from the test goroutine).
	// Use a small sync.Mutex wrapper around bytes.Buffer. We can't
	// just use log.SetOutput with a thread-safe writer from the
	// stdlib directly — the stdlib doesn't ship one. Without this
	// guard, the test would race on every run with -race.
	logBuf := &syncBuffer{}
	origLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(logBuf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(origLogger) })

	cfg := config.DefaultConfig()
	cfg.WorldDir = ""
	cfg.ViewDist = 2

	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	driveFSM(t, serverConn, clientConn, &cfg)

	// Send one of each "was-unknown" packet with a minimal 1-byte body.
	for _, id := range []int32{0x1D, 0x1E, 0x1F, 0x20, 0x0C, 0x29, 0x2A} {
		var w protocol.WireWriter
		w.Byte(0x00)
		pkt := protocol.MakePacket(id, w.Bytes())
		if _, err := clientConn.Write(pkt); err != nil {
			t.Fatalf("write 0x%02X: %v", id, err)
		}
	}
	// Give the server time to process.
	time.Sleep(300 * time.Millisecond)

	logged := logBuf.String()
	for _, id := range []int32{0x1D, 0x1E, 0x1F, 0x20, 0x0C, 0x29, 0x2A} {
		needle := "Unknown packet 0x" + byteToHex(id)
		if strings.Contains(logged, needle) {
			t.Errorf("server still logs %s for movement packet — fix the dispatch!", needle)
		}
	}
}

func byteToHex(b int32) string {
	const hexchars = "0123456789abcdef"
	return string([]byte{hexchars[(b>>4)&0xF], hexchars[b&0xF]})
}
