// Package player_test — E2E regression test for the user-reported bug
// "сохранение происходит только когда останавливается сервер и при
// этом игрок находится на сервере".
//
// The test starts a real server (using server.New + Serve on a
// loopback listener), connects a fake client, drives the full login
// FSM, then closes the client connection. The server's OnDisconnect
// hook — wired in server.go's AcceptLoop — must fire and write the
// player file. If the wiring is missing (e.g. somebody removes the
// OnDisconnect assignment from server.go), the file is not created
// and the test fails.
//
// This test is intentionally placed in the player_test package so
// it can use the handshakeAndLogin / configAckAndEnterPlay helpers
// from test_helpers_test.go.
package player_test

import (
	"net"
	"context"
	"path/filepath"
	"testing"
	"time"

	"goore/internal/config"
	"goore/internal/server"
)

func TestServerWiresOnDisconnect_E2E(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.WorldDir = dir
	cfg.ViewDist = 2
	cfg.SaveOnDisconnect = true
	cfg.Port = 25567 // not used; we provide our own listener

	// Start a real server on a loopback listener.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	// We close the listener explicitly at the end of the test
	// (after the file is found) to make the server's Serve loop
	// return and let the HandleConn goroutine finish. Without
	// this, t.TempDir() cleanup races with the still-running
	// server goroutine and fails with "directory not empty".

	srv := server.New(cfg)
	serveDone := make(chan struct{})
	go func() {
		defer close(serveDone)
		_ = srv.Serve(context.Background(), ln)
	}()

	// Connect from a fake client and drive the FSM.
	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	// We do NOT defer conn.Close() — we close it explicitly to
	// trigger the disconnect, then verify the file is created.

	// Use the same helpers as the other player integration tests.
	// We need to read server-bound packets in a goroutine.
	serverPackets := make(chan serverPkt, 32)
	go readServerPackets(conn, serverPackets)

	handshakeAndLogin(t, conn, cfg)
	configAckAndEnterPlay(t, conn, serverPackets)

	// Wait for the player to reach the play state on the server
	// side. There's no direct handle to the *Player in this test
	// (the server created it), so we just give it a moment to
	// settle and then disconnect.
	time.Sleep(200 * time.Millisecond)

	// Disconnect by closing the client side. The server's
	// HandleConn defer should fire and call OnDisconnect →
	// SavePlayer.
	conn.Close()

	// Wait for the file to appear. The OnDisconnect hook runs
	// inside the HandleConn goroutine, which may take a few ms
	// after the TCP close is observed.
	deadline := time.Now().Add(3 * time.Second)
	var found []string
	for time.Now().Before(deadline) {
		matches, _ := filepath.Glob(filepath.Join(dir, "players", "*.dat"))
		if len(matches) > 0 {
			found = matches
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(found) == 0 {
		t.Fatalf("no player file created on disconnect (dir=%s) — server.OnDisconnect wiring broken", dir)
	}
	t.Logf("player file created on disconnect: %s", found[0])

	// Stop the server and wait for the goroutines to exit before
	// t.TempDir() cleanup runs. We close the listener to make
	// Serve return; the HandleConn goroutine already ran its
	// defer (and wrote the file) by this point.
	ln.Close()
	<-serveDone
}
