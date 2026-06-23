package player_test

import (
	"bytes"
	"net"
	"testing"
	"time"

	"goore/internal/config"
	"goore/internal/protocol"
	"goore/internal/protocol/v772"
)

// serverPkt is a single server-bound packet captured by the read goroutine.
type serverPkt struct {
	id   int32
	data []byte
}

// readPacket waits up to `timeout` for the next packet. Fails the test if none arrives.
func readPacket(t *testing.T, ch <-chan serverPkt, timeout time.Duration) serverPkt {
	t.Helper()
	select {
	case pkt := <-ch:
		return pkt
	case <-time.After(timeout):
		t.Fatalf("timed out after %v waiting for packet", timeout)
		return serverPkt{}
	}
}

// startPacketReader starts a goroutine that reads all packets the server
// sends to clientConn and decodes them into serverPkt values on the
// returned channel. The channel is buffered; the goroutine exits on
// read error.
func startPacketReader(clientConn net.Conn) <-chan serverPkt {
	ch := make(chan serverPkt, 100)
	go func() {
		buf := make([]byte, 65536)
		accum := &bytes.Buffer{}
		for {
			n, err := clientConn.Read(buf)
			if err != nil {
				close(ch)
				return
			}
			accum.Write(buf[:n])
			for accum.Len() > 0 {
				r := protocol.NewWireReader(accum.Bytes())
				length := r.VarInt()
				if r.Err() != nil {
					return
				}
				pktLen := int(length) + protocol.VarIntSize(length)
				if accum.Len() < pktLen {
					break
				}
				pktID := r.VarInt()
				if r.Err() != nil {
					return
				}
				payload := make([]byte, int(length)-protocol.VarIntSize(pktID))
				r.Read(payload)
				ch <- serverPkt{id: pktID, data: payload}
				accum.Next(pktLen)
			}
		}
	}()
	return ch
}

// handshakeAndLogin walks the client through handshake (state=login) and
// login_start. Reads the LoginSuccess packet. The test must have a
// serverPackets channel receiving from a client-side read goroutine.
func handshakeAndLogin(t *testing.T, clientConn net.Conn, cfg config.Config) {
	t.Helper()

	// Handshake
	hw := &protocol.WireWriter{}
	hw.VarInt(v772.Version)
	hw.String("localhost")
	hw.Uint16(uint16(cfg.Port))
	hw.VarInt(v772.HandshakeStateLogin)
	if hw.Err() != nil {
		t.Fatalf("handshake write: %v", hw.Err())
	}
	if _, err := clientConn.Write(protocol.MakePacket(v772.HandshakeIntention, hw.Bytes())); err != nil {
		t.Fatalf("write handshake: %v", err)
	}

	// Login Start
	lw := &protocol.WireWriter{}
	lw.String("TestPlayer")
	uuid := [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	lw.UUID(uuid)
	if lw.Err() != nil {
		t.Fatalf("login start write: %v", lw.Err())
	}
	if _, err := clientConn.Write(protocol.MakePacket(v772.LoginStart, lw.Bytes())); err != nil {
		t.Fatalf("write login start: %v", err)
	}
}

// configAckAndEnterPlay drives the configuration phase: reads all config
// packets, sends Settings, SelectKnownPacks, FinishConfig, then sends
// ConfigAcknowledged (0x0F) to enter play.
func configAckAndEnterPlay(t *testing.T, clientConn net.Conn, serverPackets <-chan serverPkt) {
	t.Helper()

	// Read LoginSuccess
	pkt := readPacket(t, serverPackets, 2*time.Second)
	if pkt.id != v772.LoginSuccess {
		t.Fatalf("expected LoginSuccess (0x%02X), got 0x%02X", v772.LoginSuccess, pkt.id)
	}

	// LoginAcknowledged
	if _, err := clientConn.Write(protocol.MakePacket(v772.LoginAcknowledged, nil)); err != nil {
		t.Fatalf("write login ack: %v", err)
	}

	// Read config packets until we see FinishConfiguration (0x03 clientbound)
	for i := 0; i < 32; i++ {
		pkt := readPacket(t, serverPackets, 2*time.Second)
		if pkt.id == v772.ConfigFinishConfig {
			goto sendConfig
		}
	}
	t.Fatal("did not receive ConfigFinishConfig from server")

sendConfig:
	// Settings (0x00)
	cw := &protocol.WireWriter{}
	cw.String("en_US")
	cw.Byte(8)
	cw.VarInt(1)
	cw.Bool(true)
	cw.Byte(0xFF)
	cw.VarInt(1)
	cw.Bool(false)
	cw.Bool(true)
	if _, err := clientConn.Write(protocol.MakePacket(v772.ConfigSettings, cw.Bytes())); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	// SelectKnownPacks (0x07)
	sw := &protocol.WireWriter{}
	sw.VarInt(1)
	sw.String("minecraft")
	sw.String("core")
	sw.String("1.21.8")
	if _, err := clientConn.Write(protocol.MakePacket(v772.ConfigSelectKnownPacks, sw.Bytes())); err != nil {
		t.Fatalf("write select known packs: %v", err)
	}

	// FinishConfig (serverbound 0x03)
	if _, err := clientConn.Write(protocol.MakePacket(v772.ConfigFinishConfig, nil)); err != nil {
		t.Fatalf("write finish config: %v", err)
	}
}

// drainChunks reads the play-phase spawn packets (login_play, spawn_pos,
// abilities, position, view distance, all chunks, container content, held
// item) until we see HeldItemSlot (0x62). This brings the player into the
// "play" state where they can issue game actions.
func drainChunks(t *testing.T, serverPackets <-chan serverPkt) {
	t.Helper()
	for i := 0; i < 600; i++ {
		pkt := readPacket(t, serverPackets, 2*time.Second)
		if pkt.id == v772.PlayHeldItemSlot {
			// Spawn sequence complete.
			return
		}
	}
	t.Fatal("did not receive HeldItemSlot — spawn sequence incomplete")
}

// encodePos is a test helper that encodes a block position (x, y, z) into
// the 64-bit format used by vanilla Minecraft packets (block_dig,
// use_item_on, etc.). Vanilla 1.21.8 wire format:
//
//	x: 26 bits, z: 26 bits, y: 12 bits
//	packed = (x & 0x3FFFFFF) << 38 | (z & 0x3FFFFFF) << 12 | (y & 0xFFF)
func encodePos(x, y, z int) int64 {
	return (int64(x)&0x3FFFFFF)<<38 | (int64(z)&0x3FFFFFF)<<12 | (int64(y) & 0xFFF)
}

// waitFor polls cond() every waitForPollInterval until cond() returns
// true OR timeout elapses, then returns the last cond() value.
//
// This replaces the legacy `time.Sleep(N); if !cond() { t.Fatal(...) }`
// pattern that was sprinkled across the integration tests. The poll
// loop (a) avoids the worst-case 10× timeout penalty when the event
// fires early, (b) surfaces slow paths via the failure message
// showing how long we actually waited, and (c) is safe under
// `go test -race` — the poll does not interact with goroutine
// state in a way that race detector would flag.
//
// Phase 3.6 refactor (REFACTORING_PLAN.md §3.6). Use:
//
//	if !waitFor(t, 2*time.Second, func() bool {
//	    return checkCondition(...)
//	}) {
//	    t.Fatalf("condition not met within timeout")
//	}
func waitFor(t *testing.T, timeout time.Duration, cond func() bool) bool {
	t.Helper()
	const pollInterval = 10 * time.Millisecond
	deadline := time.Now().Add(timeout)
	// Fast-path: condition may already be true on entry.
	if cond() {
		return true
	}
	for time.Now().Before(deadline) {
		time.Sleep(pollInterval)
		if cond() {
			return true
		}
	}
	return cond()
}
