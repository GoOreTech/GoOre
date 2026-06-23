// Phase 3.7 graceful shutdown test for Server.Serve.
//
// The pre-Phase-3.7 Serve signature was Serve(l net.Listener) error.
// Shutdown was triggered by closing the listener, which caused
// l.Accept() to return an error. Active player connections were
// NOT closed by the server — they kept running until the OS closed
// the sockets (e.g. process exit). The user-reported symptom: a
// SIGINT that races with active players sometimes left the world's
// dirty chunks unsaved because HandleConn's defer fired AFTER
// os.Exit(0) had already started tearing down the process.
//
// The new Serve signature is Serve(ctx context.Context, l net.Listener) error.
// Cancellation flows like this:
//
//  1. main() receives SIGINT/SIGTERM and calls cancel().
//  2. Serve's ctx-watcher goroutine sees ctx.Done() and closes the
//     listener (unblocks Accept).
//  3. acceptLoop returns.
//  4. Serve stops the flusher and position-broadcast tick.
//  5. Serve calls closeAllPlayerConns() to forcibly close any
//     active player sockets (HandleConn's read loop will then
//     return an error and the OnDisconnect hook will fire).
//  6. Serve waits for s.players to drain.
//  7. Serve returns nil.
//
// The test below pins the new behavior: starting a real server,
// driving one player to the play state, then cancelling ctx MUST
// cause Serve to return within a reasonable timeout. The active
// player connection MUST also close (their OnDisconnect hook
// fires). The test does NOT need to do much beyond starting the
// loopback listener, dialing once, and asserting both effects.
package server

import (
	"context"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"goore/internal/config"
)

func TestServe_ContextCancellation_TriggersGracefulShutdown(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.WorldDir = "" // no persistence — focus on shutdown, not saves
	cfg.ViewDist = 2
	cfg.SaveInterval = 0
	cfg.MaxPlayers = 4

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()

	srv := New(cfg)

	ctx, cancel := context.WithCancel(context.Background())
	serveReturned := make(chan error, 1)
	go func() {
		// With the new Serve(ctx, l) signature, cancelling ctx
		// must cause Serve to return nil (not an Accept error).
		serveReturned <- srv.Serve(ctx, ln)
	}()

	// Connect a client so we have an active player. We don't drive
	// the FSM — just open a socket so the server has a connection
	// to close during shutdown.
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// Give the server a beat to accept the connection.
	time.Sleep(50 * time.Millisecond)

	// Active player count should be 1.
	if got := srv.PlayerCount(); got != 1 {
		t.Fatalf("PlayerCount after dial = %d, want 1", got)
	}

	// Cancel ctx. The shutdown sequence is:
	//   close listener → close all player conns → wait for s.players
	//   to drain → return nil.
	cancel()

	// Serve must return within 5 s. A 50 ms typical-shutdown is
	// realistic; the 5 s budget is just a regression sentinel.
	select {
	case err := <-serveReturned:
		if err != nil {
			t.Errorf("Serve returned error after ctx cancel: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not return within 5 s after ctx cancel — shutdown is stuck")
	}

	// After Serve returns, the player should have been removed
	// from s.players (HandleConn saw the closed conn and the
	// defer fired). A small polling window covers the final
	// s.players.Delete() happening on a different goroutine.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if srv.PlayerCount() == 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got := srv.PlayerCount(); got != 0 {
		t.Errorf("PlayerCount after shutdown = %d, want 0", got)
	}
}

// TestServe_ContextAlreadyCancelled_ReturnsImmediately covers the
// edge case where ctx is already cancelled before Serve is called.
// Accept will return immediately on the closed listener, the
// shutdown sequence will find no active players, and Serve should
// return nil without blocking.
func TestServe_ContextAlreadyCancelled_ReturnsImmediately(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.WorldDir = ""
	cfg.SaveInterval = 0

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	srv := New(cfg)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel BEFORE Serve starts

	// Closing the listener first so Accept returns immediately
	// on the first iteration (otherwise we'd race the cancel
	// goroutine). This simulates the post-cancel state.
	done := make(chan error, 1)
	var beforeAccept atomic.Bool
	go func() {
		beforeAccept.Store(true)
		done <- srv.Serve(ctx, ln)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Serve returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Serve blocked when ctx was already cancelled — should return immediately")
	}
}
