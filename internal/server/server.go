package server

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"goore/internal/config"
	"goore/internal/player"
	"goore/internal/protocol/v772"
	"goore/internal/world"
)

// Server is the Minecraft server instance.
//
// players is sync.Map keyed by EID → *player.Player. The pattern is
// "write once (Store on join, Delete on disconnect), read many (Range
// from position tick, broadcast, spawn/despawn)", which matches
// sync.Map's sweet spot. Race-on-*Player is handled at the Player
// level via posMu / hotbarMu / stateAtomic. See docs/server.md.
type Server struct {
	cfg     config.Config
	proto   *v772.Protocol
	world   *world.World
	players sync.Map // eid (int32) → *player.Player
	nextEID atomic.Int32

	posInterval time.Duration
	posMu       sync.Mutex
	lastPos     map[int32]playerPos
}

// playerPos is the snapshot of a player's last broadcast position.
type playerPos struct {
	X, Y, Z    float64
	Yaw, Pitch float32
	OnGround   bool
}

// New creates a new Server. If cfg.WorldDir is non-empty, the world is loaded from / saved to that directory and a background flusher is started.
func New(cfg config.Config) *Server {
	var w *world.World
	if cfg.WorldDir != "" {
		w = world.NewWithDir(cfg.Seed, cfg.WorldDir)
		if err := w.LoadAll(); err != nil {
			slog.Warn("world load failed; starting with fresh seed", "err", err)
			w = world.NewWithDir(cfg.Seed, cfg.WorldDir)
		}
	} else {
		w = world.New(cfg.Seed)
	}
	return &Server{
		cfg:         cfg,
		proto:       v772.New(),
		world:       w,
		posInterval: 50 * time.Millisecond, // 20 Hz default
		lastPos:     make(map[int32]playerPos),
	}
}

// World returns the server's world. Used by main.go for explicit saves.
func (s *Server) World() *world.World { return s.world }

// SaveAll flushes the world to disk. Safe to call concurrently.
func (s *Server) SaveAll() error {
	if s.world.Dir() == "" {
		return nil
	}
	return s.world.SaveAll()
}

// SetPositionBroadcastInterval sets the period of the position-broadcast tick. Pass 0 to disable the tick (useful for tests).
func (s *Server) SetPositionBroadcastInterval(d time.Duration) {
	s.posMu.Lock()
	s.posInterval = d
	s.posMu.Unlock()
}

// getPosInterval returns the current position-broadcast interval under lock. Returns 0 if the tick is disabled.
func (s *Server) getPosInterval() time.Duration {
	s.posMu.Lock()
	defer s.posMu.Unlock()
	return s.posInterval
}

// Serve accepts and handles incoming connections on l. It blocks until either the listener is closed or ctx is cancelled. See docs/server.md §Graceful shutdown for the full sequence.
func (s *Server) Serve(ctx context.Context, l net.Listener) error {
	// Start the world flusher if persistence is enabled.
	var stopFlusher func()
	if s.world.Dir() != "" && s.cfg.SaveInterval > 0 {
		stopFlusher = s.world.StartFlusher(s.cfg.SaveInterval)
	}

	// Start the position-broadcast tick.
	posStop := make(chan struct{})
	posDone := make(chan struct{})
	go func() {
		defer close(posDone)
		s.positionTick(posStop)
	}()

	// ctx watcher: cancellation closes the listener to unblock Accept.
	go func() {
		<-ctx.Done()
		slog.Info("server context cancelled; initiating graceful shutdown",
			"err", context.Cause(ctx))
		_ = l.Close()
	}()

	acceptErr := s.acceptLoop(l)

	// Shutdown sequence (best-effort — partial failures don't leak the process).
	if stopFlusher != nil {
		stopFlusher()
	}
	close(posStop)
	<-posDone

	s.closeAllPlayerConns()
	s.waitForPlayersToFinish(5 * time.Second)

	// Cancellation path returns nil; external Close() surfaces the real error.
	if ctx.Err() != nil {
		return nil
	}
	if acceptErr != nil && !errors.Is(acceptErr, net.ErrClosed) {
		return acceptErr
	}
	return nil
}

// acceptLoop is the per-connection accept + spawn loop.
func (s *Server) acceptLoop(l net.Listener) error {
	for {
		conn, err := l.Accept()
		if err != nil {
			return err
		}
		eid := s.nextEID.Add(1) - 1
		p := player.New(eid, conn, s.proto, s.world, s.cfg)
		p.SetHooks(&serverPlayerHooks{server: s, originEID: eid})
		s.players.Store(eid, p)
		go func() {
			defer s.players.Delete(eid)
			p.HandleConn()
		}()

		slog.Info("player joined", "eid", eid)
	}
}

// closeAllPlayerConns iterates s.players and calls Close on each connection. Best-effort kick — HandleConn's read loop does the actual cleanup via its defer.
func (s *Server) closeAllPlayerConns() {
	s.players.Range(func(_, value any) bool {
		p := value.(*player.Player)
		_ = p.Close() // type-asserted close; real net.Conn and net.Pipe both implement io.Closer
		return true
	})
}

// waitForPlayersToFinish polls s.players until it is empty or timeout elapses.
func (s *Server) waitForPlayersToFinish(timeout time.Duration) {
	const pollInterval = 20 * time.Millisecond
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		empty := true
		s.players.Range(func(_, _ any) bool {
			empty = false
			return false // stop iteration
		})
		if empty {
			return
		}
		time.Sleep(pollInterval)
	}
}

// Broadcast sends a packet to all connected players.
func (s *Server) Broadcast(pkt []byte) {
	s.players.Range(func(key, value any) bool {
		p := value.(*player.Player)
		p.SendPacket(pkt)
		return true
	})
}

// PlayerCount returns the number of connected players.
func (s *Server) PlayerCount() int {
	count := 0
	s.players.Range(func(key, value any) bool {
		count++
		return true
	})
	return count
}

// SaveAllPlayers writes each connected player's data to disk. Called on SIGINT/SIGTERM or when a player disconnects (if cfg.SaveOnDisconnect).
func (s *Server) SaveAllPlayers() {
	if s.cfg.WorldDir == "" {
		return
	}
	s.players.Range(func(key, value any) bool {
		p := value.(*player.Player)
		if err := player.SavePlayer(s.cfg.WorldDir, p); err != nil {
			slog.Warn("save player on disconnect failed", "name", p.Name, "err", err)
		}
		return true
	})
}

// PlayerEntityTypeID is the minecraft-data entity type ID for a player (minecraft:player).
const PlayerEntityTypeID = 149

// SpawnPlayerForOthers makes p visible to every OTHER player, and also makes every OTHER player visible to p. See docs/server.md §Spawn / despawn iterators.
func (s *Server) SpawnPlayerForOthers(p *player.Player) {
	s.players.Range(func(_, value any) bool {
		other := value.(*player.Player)
		if other.EID == p.EID {
			return true
		}
		_ = MakeVisible(p, other) // p is visible to other
		_ = MakeVisible(other, p) // other is visible to p
		return true
	})
}

// DespawnPlayerForOthers removes p from every OTHER currently connected player. Called from OnLeavePlay BEFORE OnDisconnect.
func (s *Server) DespawnPlayerForOthers(p *player.Player) {
	s.players.Range(func(_, value any) bool {
		other := value.(*player.Player)
		if other.EID == p.EID {
			return true
		}
		_ = MakeInvisible(p, other)
		return true
	})
}

// registerPositionBroadcaster seeds the lastPos map for p with its current position. Called from OnEnterPlay.
func (s *Server) registerPositionBroadcaster(p *player.Player) {
	px, py, pz, yaw, pitch, onGround := p.Pos()
	s.posMu.Lock()
	s.lastPos[p.EID] = playerPos{X: px, Y: py, Z: pz, Yaw: yaw, Pitch: pitch, OnGround: onGround}
	s.posMu.Unlock()
}

// unregisterPositionBroadcaster removes p from the lastPos map. Called from OnLeavePlay.
func (s *Server) unregisterPositionBroadcaster(p *player.Player) {
	s.posMu.Lock()
	delete(s.lastPos, p.EID)
	s.posMu.Unlock()
}

// positionTick runs for the lifetime of the server, broadcasting entity_teleport (0x76) for moved players. Tests disable via SetPositionBroadcastInterval(0). See docs/server.md §Position-broadcast tick.
func (s *Server) positionTick(stop <-chan struct{}) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("positionTick panic",
				"panic", r,
				"stack", string(debug.Stack()))
		}
	}()
	for {
		interval := s.getPosInterval()
		if interval <= 0 {
			// Tick disabled — exit so we don't leak a goroutine.
			return
		}
		select {
		case <-stop:
			return
		case <-time.After(interval):
		}
		s.broadcastPositionOnce()
	}
}

// broadcastPositionOnce is one iteration of the position-tick. Extracted so tests can drive it directly. See docs/server.md §broadcastPositionOnce.
func (s *Server) broadcastPositionOnce() {
	type entry struct {
		eid  int32
		pos  playerPos
		prev playerPos
	}
	var moved []entry
	s.players.Range(func(_, value any) bool {
		p := value.(*player.Player)
		px, py, pz, yaw, pitch, onGround := p.Pos()
		cur := playerPos{X: px, Y: py, Z: pz, Yaw: yaw, Pitch: pitch, OnGround: onGround}
		s.posMu.Lock()
		prev, seeded := s.lastPos[p.EID]
		if !seeded {
			s.lastPos[p.EID] = cur
			s.posMu.Unlock()
			return true
		}
		s.posMu.Unlock()
		if cur != prev {
			moved = append(moved, entry{eid: p.EID, pos: cur, prev: prev})
		}
		return true
	})
	for _, m := range moved {
		s.posMu.Lock()
		s.lastPos[m.eid] = m.pos
		s.posMu.Unlock()
		pkt := s.proto.WriteEntityTeleport(
			m.eid, m.pos.X, m.pos.Y, m.pos.Z,
			m.pos.Yaw, m.pos.Pitch, m.pos.OnGround,
		)
		s.broadcastExcept(m.eid, pkt)

		// entity_head_rotation (0x4C) is the head-only channel — independent from the body yaw.
		headYaw := float32(m.pos.Yaw) * 256.0 / 360.0
		headPkt := s.proto.WriteEntityHeadRotation(m.eid, int8(headYaw))
		s.broadcastExcept(m.eid, headPkt)
	}
}

// broadcastExcept sends pkt to all connected players EXCEPT the one with EID == exceptEID.
func (s *Server) broadcastExcept(exceptEID int32, pkt []byte) {
	s.players.Range(func(_, value any) bool {
		other := value.(*player.Player)
		if other.EID == exceptEID {
			return true
		}
		_ = other.SendPacket(pkt)
		return true
	})
}
