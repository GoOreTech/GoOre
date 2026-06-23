// Tests for the Player Visibility module (visibility.go). These
// pin the invariants that motivated the deepening: the 5-packet
// spawn sequence MUST arrive in a strict order, and the 2-packet
// despawn sequence MUST arrive in the reverse-symmetric order.
// A regression in the ordering would silently break client
// rendering — these tests are the type-system check that didn't
// exist when the logic was inline in spawnPlayerPairOnto.
package server_test

import (
	"bytes"
	"io"
	"net"
	"testing"
	"time"

	"goore/internal/config"
	"goore/internal/player"
	"goore/internal/protocol"
	"goore/internal/protocol/v772"
	"goore/internal/server"
	"goore/internal/world"
)

// captureN starts a goroutine that reads from `conn` and pushes
// packet IDs into a channel. Returns:
//   - start: a no-op to call (kept for symmetry; the goroutine
//     starts immediately).
//   - wait: blocks until `n` IDs are read or `timeout` elapses,
//     then returns whatever was captured.
//
// CRITICAL: the goroutine must start BEFORE the writer — net.Pipe
// is synchronous, so a Write to one side blocks until the other
// side Reads. Calling wait() without first having something
// reading the pipe will deadlock.
func captureN(conn net.Conn, n int, timeout time.Duration) (start func(), wait func() []int32) {
	ch := make(chan int32, n)
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 65536)
		accum := &bytes.Buffer{}
		captured := 0
		for captured < n {
			_ = conn.SetReadDeadline(time.Now().Add(timeout))
			r, err := conn.Read(buf)
			if err != nil {
				return
			}
			accum.Write(buf[:r])
			for accum.Len() > 0 && captured < n {
				rdr := protocol.NewWireReader(accum.Bytes())
				length := rdr.VarInt()
				if rdr.Err() != nil {
					return
				}
				pktLen := int(length) + protocol.VarIntSize(length)
				if accum.Len() < pktLen {
					break
				}
				pktID := rdr.VarInt()
				if rdr.Err() != nil {
					return
				}
				ch <- pktID
				captured++
				accum.Next(pktLen)
			}
		}
	}()

	start = func() {}
	wait = func() []int32 {
		out := make([]int32, 0, n)
		deadline := time.After(timeout + 500*time.Millisecond)
		for len(out) < n {
			select {
			case id := <-ch:
				out = append(out, id)
			case <-deadline:
				return out
			case <-done:
				for {
					select {
					case id := <-ch:
						out = append(out, id)
					default:
						return out
					}
				}
			}
		}
		return out
	}
	return start, wait
}

// nopRW is a no-op ReadWriter used as a Player.Conn for the
// `who` argument in tests where we only care about the bytes
// going to the `to` Player's connection.
type nopRW struct{}

func (nopRW) Read(p []byte) (int, error)  { return 0, io.EOF }
func (nopRW) Write(p []byte) (int, error) { return len(p), nil }

// TestMakeVisible_PacketOrder pins the 5-packet sequence of
// MakeVisible. If the order of SendPacket calls in visibility.go
// changes, this test fails with a clear "got X, want Y" diff.
//
// The vanilla 1.21.8 decoder depends on this exact order:
//  1. player_info_update (0x3F) BEFORE spawn_entity — the client
//     uses player_info to attach skin/gamemode to the entity.
//  2. spawn_entity (0x01) — the entity must exist before
//     metadata, equipment, and head rotation can reference it.
//  3. entity_metadata (0x5C) — bare minimum so the entity renders.
//  4. entity_equipment (0x5F) — main-hand item (or empty slot).
//  5. entity_head_rotation (0x4C) — head-only channel, separate
//     from body yaw.
func TestMakeVisible_PacketOrder(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	proto := v772.New()
	cfg := config.DefaultConfig()
	cfg.ViewDist = 1
	who := player.New(7, nopRW{}, proto, world.New(0), cfg)
	who.Name = "Alice"
	who.UUID = [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	who.HeldItem = 1 // stone — exercises the non-empty equipment branch
	to := player.New(8, serverConn, proto, world.New(0), cfg)
	to.Name = "Bob"

	// Start the reader BEFORE MakeVisible — net.Pipe is synchronous,
	// so a Write blocks until the peer Reads. Starting the reader
	// first unblocks the writer.
	_, wait := captureN(clientConn, 5, 2*time.Second)
	if err := server.MakeVisible(who, to); err != nil {
		t.Fatalf("MakeVisible returned %v, want nil", err)
	}
	got := wait()

	want := []int32{
		v772.PlayPlayerInfoUpdate,   // 0x3F
		v772.PlaySpawnEntity,        // 0x01
		v772.PlayEntityMetadata,     // 0x5C
		v772.PlayEntityEquipment,    // 0x5F
		v772.PlayEntityHeadRotation, // 0x4C
	}
	if len(got) != len(want) {
		t.Fatalf("MakeVisible produced %d packets, want %d. got = %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("packet[%d] = 0x%02X, want 0x%02X (send order is an invariant — see visibility.go MakeVisible docs)",
				i, got[i], want[i])
		}
	}
}

// TestMakeInvisible_PacketOrder pins the 2-packet despawn
// sequence: remove_entities (0x46) BEFORE player_remove (0x3E).
// The client must stop rendering the entity before the tab-list
// entry disappears, otherwise there is a one-frame flicker where
// the player exists as a nameless entity.
func TestMakeInvisible_PacketOrder(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	proto := v772.New()
	cfg := config.DefaultConfig()
	who := player.New(7, nopRW{}, proto, world.New(0), cfg)
	who.UUID = [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	to := player.New(8, serverConn, proto, world.New(0), cfg)

	// Start reader before MakeInvisible for the same reason as
	// TestMakeVisible_PacketOrder: net.Pipe is synchronous.
	_, wait := captureN(clientConn, 2, 2*time.Second)
	if err := server.MakeInvisible(who, to); err != nil {
		t.Fatalf("MakeInvisible returned %v, want nil", err)
	}
	got := wait()

	want := []int32{
		v772.PlayRemoveEntities, // 0x46
		v772.PlayPlayerRemove,   // 0x3E
	}
	if len(got) != len(want) {
		t.Fatalf("MakeInvisible produced %d packets, want %d. got = %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("packet[%d] = 0x%02X, want 0x%02X (despawn order is an invariant — entity first, then tab-list)",
				i, got[i], want[i])
		}
	}
}

// TestMakeVisible_BestEffortOnError verifies the "return first
// error" contract. We close the receiver before the call. The
// first SendPacket in MakeVisible will fail with a closed-pipe
// error; MakeVisible must return that error rather than
// swallowing it. If a future refactor changes the contract to
// "always return nil", this test fails.
func TestMakeVisible_BestEffortOnError(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	// Close the receiver BEFORE the call so to.SendPacket fails.
	clientConn.Close()

	proto := v772.New()
	cfg := config.DefaultConfig()
	who := player.New(7, nopRW{}, proto, world.New(0), cfg)
	to := player.New(8, serverConn, proto, world.New(0), cfg)

	err := server.MakeVisible(who, to)
	serverConn.Close()
	if err == nil {
		t.Errorf("MakeVisible returned nil; want non-nil (best-effort should return the first error so callers can log it)")
	}
}
