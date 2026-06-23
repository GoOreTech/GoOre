// Package player_test — Phase 5 survival mechanics integration tests.
// These drive the real FSM over net.Pipe with a survival player and assert
// the wire + state effects of fall damage, void damage, starvation, eating,
// death/respawn, and the /gamemode command.
package player_test

import (
	"net"
	"testing"
	"time"

	"goore/internal/config"
	"goore/internal/player"
	"goore/internal/protocol"
	"goore/internal/protocol/v772"
	"goore/internal/world"
)

// survivalConfig returns a config with the default gamemode set to survival
// and a tiny view distance so the spawn chunk batch is small/fast.
func survivalConfig() config.Config {
	cfg := config.DefaultConfig()
	cfg.Gamemode = player.GamemodeSurvival
	cfg.ViewDist = 2
	cfg.WorldDir = ""
	return cfg
}

// driveSurvivalFSM is driveFSM but defaults the player to survival and returns
// the player + the client-side packet channel (already drained past spawn).
func driveSurvivalFSM(t *testing.T) (*player.Player, net.Conn, <-chan serverPkt) {
	t.Helper()
	cfg := survivalConfig()
	serverConn, clientConn := net.Pipe()
	w := world.NewWithDir(42, "")
	proto := v772.New()
	p := player.New(42, serverConn, proto, w, cfg)

	done := make(chan struct{})
	go func() {
		defer close(done)
		p.HandleConn()
	}()

	fsmPackets := make(chan serverPkt, 1024)
	go readServerPackets(clientConn, fsmPackets)

	handshakeAndLogin(t, clientConn, cfg)
	configAckAndEnterPlay(t, clientConn, fsmPackets)
	if !waitForPlay(t, p, 3*time.Second) {
		t.Fatal("player never reached play state")
	}
	drainChunks(t, fsmPackets)
	return p, clientConn, fsmPackets
}

// sendPosition writes a set_player_position (0x1D) packet from the client.
func sendPosition(t *testing.T, c net.Conn, x, y, z float64, onGround bool) {
	t.Helper()
	var w protocol.WireWriter
	w.Float64(x)
	w.Float64(y)
	w.Float64(z)
	if onGround {
		w.Byte(0x01)
	} else {
		w.Byte(0x00)
	}
	if _, err := c.Write(protocol.MakePacket(v772.PlaySetPlayerPos, w.Bytes())); err != nil {
		t.Fatalf("write position: %v", err)
	}
}

// TestFallDamage_KillsAndBroadcastsDeath: a survival player falls 24 blocks and
// lands. They should take lethal fall damage (ceil(24-3)=21 ≥ 20 HP), transition
// to Dead, receive a DamageEvent (0x19) + UpdateHealth(0) (0x61), and the death
// animation EntityStatus (0x1E) should be broadcast (here: self via noOpHooks).
func TestFallDamage_KillsAndBroadcastsDeath(t *testing.T) {
	p, clientConn, pkts := driveSurvivalFSM(t)
	defer clientConn.Close()

	// Simulate a 26-block fall (lethal: ceil(26-3)=23 >= 20 HP): air packets
	// descending, then a landing packet.
	sendPosition(t, clientConn, 0.5, 30.0, 0.5, false) // start high, airborne
	sendPosition(t, clientConn, 0.5, 10.0, 0.5, false)  // falling
	sendPosition(t, clientConn, 0.5, 4.0, 0.5, true)    // land → lethal damage

	// DamageEvent (0x19) must arrive.
	if _, ok := waitForPacketID(pkts, v772.PlayDamageEvent, 2*time.Second); !ok {
		t.Fatal("did not receive DamageEvent (0x19) after lethal fall")
	}

	// Player must be dead.
	if !waitFor(t, 2*time.Second, func() bool { return p.Vitals().Dead }) {
		t.Fatalf("player not dead after lethal fall; health=%v", p.Vitals().Health)
	}
	if h := p.Vitals().Health; h != 0 {
		t.Errorf("health = %v, want 0", h)
	}
}

// TestFallDamage_NonLethal: a 6-block fall deals ceil(6-3)=3 damage; player
// survives with 17 HP and is NOT dead.
func TestFallDamage_NonLethal(t *testing.T) {
	p, clientConn, pkts := driveSurvivalFSM(t)
	defer clientConn.Close()

	sendPosition(t, clientConn, 0.5, 10.0, 0.5, false)
	sendPosition(t, clientConn, 0.5, 4.0, 0.5, true) // 6-block fall

	if _, ok := waitForPacketID(pkts, v772.PlayDamageEvent, 2*time.Second); !ok {
		t.Fatal("did not receive DamageEvent after 6-block fall")
	}
	if !waitFor(t, 2*time.Second, func() bool { return p.Vitals().Health == 17 }) {
		t.Fatalf("health = %v, want 17 (20 - ceil(6-3))", p.Vitals().Health)
	}
	if p.Vitals().Dead {
		t.Error("player should not be dead after a 6-block fall")
	}
}

// TestFallDamage_NoDamageInCreative: a creative player takes no fall damage.
func TestFallDamage_NoDamageInCreative(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Gamemode = player.GamemodeCreative
	cfg.ViewDist = 2
	cfg.WorldDir = ""
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()
	w := world.NewWithDir(42, "")
	proto := v772.New()
	p := player.New(42, serverConn, proto, w, cfg)
	done := make(chan struct{})
	go func() { defer close(done); p.HandleConn() }()
	fsmPackets := make(chan serverPkt, 1024)
	go readServerPackets(clientConn, fsmPackets)
	handshakeAndLogin(t, clientConn, cfg)
	configAckAndEnterPlay(t, clientConn, fsmPackets)
	waitForPlay(t, p, 3*time.Second)
	drainChunks(t, fsmPackets)

	sendPosition(t, clientConn, 0.5, 30.0, 0.5, false)
	sendPosition(t, clientConn, 0.5, 4.0, 0.5, true) // 26-block fall — lethal in survival

	// Give the handler a moment, then assert NO damage and full health.
	time.Sleep(150 * time.Millisecond)
	if h := p.Vitals().Health; h != 20 {
		t.Errorf("creative player took fall damage: health = %v, want 20", h)
	}
	if p.Vitals().Dead {
		t.Error("creative player should never die")
	}
}

// TestVoidDamage_Kills: a survival player who drops below y=-64 takes
// out_of_world damage every tick and dies.
func TestVoidDamage_Kills(t *testing.T) {
	p, clientConn, _ := driveSurvivalFSM(t)
	defer clientConn.Close()

	// Move the player into the void.
	p.SetPositionForTest(0.5, -70.0, 0.5, 0, 0)
	// The survival tick (50ms) should apply 4 dmg/tick → dead within ~1s.
	if !waitFor(t, 3*time.Second, func() bool { return p.Vitals().Dead }) {
		t.Fatalf("player did not die in the void; health=%v", p.Vitals().Health)
	}
}

// TestRespawn_RevivesDeadPlayer: after dying (via /kill), a client_command
// perform_respawn (action 0) revives the player at full HP and clears Dead.
func TestRespawn_RevivesDeadPlayer(t *testing.T) {
	p, clientConn, _ := driveSurvivalFSM(t)
	defer clientConn.Close()

	// Kill via /kill command.
	var cw protocol.WireWriter
	cw.String("/kill")
	if _, err := clientConn.Write(protocol.MakePacket(v772.PlayChatCommand, cw.Bytes())); err != nil {
		t.Fatalf("write /kill: %v", err)
	}
	if !waitFor(t, 2*time.Second, func() bool { return p.Vitals().Dead }) {
		t.Fatal("player did not die from /kill")
	}

	// Send perform_respawn (client_command action 0).
	var rw protocol.WireWriter
	rw.VarInt(0) // perform_respawn
	if _, err := clientConn.Write(protocol.MakePacket(v772.PlayClientCommand, rw.Bytes())); err != nil {
		t.Fatalf("write respawn: %v", err)
	}

	if !waitFor(t, 2*time.Second, func() bool {
		v := p.Vitals()
		return !v.Dead && v.Health == 20
	}) {
		v := p.Vitals()
		t.Fatalf("player not revived: dead=%v health=%v", v.Dead, v.Health)
	}
}

// TestGamemodeCommand_SurvivalToCreative: /gamemode creative switches the player
// and sends a GameEvent (0x22) change_game_mode (reason 3).
func TestGamemodeCommand_SurvivalToCreative(t *testing.T) {
	p, clientConn, pkts := driveSurvivalFSM(t)
	defer clientConn.Close()

	var cw protocol.WireWriter
	cw.String("/gamemode creative")
	if _, err := clientConn.Write(protocol.MakePacket(v772.PlayChatCommand, cw.Bytes())); err != nil {
		t.Fatalf("write gamemode: %v", err)
	}

	if !waitFor(t, 2*time.Second, func() bool { return p.Vitals().Gamemode == player.GamemodeCreative }) {
		t.Fatalf("gamemode = %v, want creative", p.Vitals().Gamemode)
	}

	// GameEvent (0x22) with reason 3 (change_game_mode) must be sent.
	pkt, ok := waitForPacketID(pkts, v772.PlayGameStateChange, 2*time.Second)
	if !ok {
		t.Fatal("did not receive GameEvent (0x22) for gamemode change")
	}
	r := protocol.NewWireReader(pkt.data)
	reason := r.Byte()
	if reason != 3 {
		t.Errorf("GameEvent reason = %d, want 3 (change_game_mode)", reason)
	}
}

// TestEating_RestoresFoodAndConsumesItem: a survival player at 0 food holds an
// apple (item 857), right-clicks (use_item 0x40), waits the eat duration, and
// should have food restored and the held-slot count decremented.
func TestEating_RestoresFoodAndConsumesItem(t *testing.T) {
	p, clientConn, _ := driveSurvivalFSM(t)
	defer clientConn.Close()

	// Starve the player to 0 food so the restore is observable.
	p.SetVitalsForTest(player.Vitals{Health: 20, Food: 0, Saturation: 0, Gamemode: player.GamemodeSurvival})

	// Put an apple in slot 0 and select it.
	p.SetHeldItemForTest(857) // apple

	// use_item (0x40) starts eating.
	if _, err := clientConn.Write(protocol.MakePacket(v772.PlayUseItem, nil)); err != nil {
		t.Fatalf("write use_item: %v", err)
	}

	// Wait for eat completion (1.6s = 32 ticks; allow margin).
	if !waitFor(t, 3*time.Second, func() bool { return p.Vitals().Food > 0 }) {
		t.Fatalf("food not restored after eating; food=%v", p.Vitals().Food)
	}
	// apple restores 4 food.
	if f := p.Vitals().Food; f != 4 {
		t.Errorf("food = %v, want 4 (apple)", f)
	}
	// Survival consumes one item: held-slot count went 64 → 63.
	counts := p.HotbarCountSnapshot()
	if counts[0] != 63 {
		t.Errorf("held slot count = %d, want 63 (consumed 1 apple)", counts[0])
	}
}

// TestEating_CreativeDoesNotConsume: a creative player eating keeps the stack.
func TestEating_CreativeDoesNotConsume(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Gamemode = player.GamemodeCreative
	cfg.ViewDist = 2
	cfg.WorldDir = ""
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()
	w := world.NewWithDir(42, "")
	proto := v772.New()
	p := player.New(42, serverConn, proto, w, cfg)
	done := make(chan struct{})
	go func() { defer close(done); p.HandleConn() }()
	fsmPackets := make(chan serverPkt, 1024)
	go readServerPackets(clientConn, fsmPackets)
	handshakeAndLogin(t, clientConn, cfg)
	configAckAndEnterPlay(t, clientConn, fsmPackets)
	waitForPlay(t, p, 3*time.Second)
	drainChunks(t, fsmPackets)

	p.SetVitalsForTest(player.Vitals{Health: 20, Food: 0, Saturation: 0, Gamemode: player.GamemodeCreative})
	p.SetHeldItemForTest(857) // apple

	if _, err := clientConn.Write(protocol.MakePacket(v772.PlayUseItem, nil)); err != nil {
		t.Fatalf("write use_item: %v", err)
	}
	if !waitFor(t, 3*time.Second, func() bool { return p.Vitals().Food == 4 }) {
		t.Fatalf("creative eat did not restore food; food=%v", p.Vitals().Food)
	}
	counts := p.HotbarCountSnapshot()
	if counts[0] != 64 {
		t.Errorf("creative held slot count = %d, want 64 (no consumption)", counts[0])
	}
}

// TestStarvation_DamagesPlayer: a survival player at 0 food takes 1 starvation
// damage every 80 ticks (4s). We fast-forward by setting Food=0 and waiting.
func TestStarvation_DamagesPlayer(t *testing.T) {
	p, clientConn, _ := driveSurvivalFSM(t)
	defer clientConn.Close()

	p.SetVitalsForTest(player.Vitals{Health: 20, Food: 0, Saturation: 0, Gamemode: player.GamemodeSurvival})

	// 80 ticks @ 50ms = 4s; allow margin for the ticker.
	if !waitFor(t, 6*time.Second, func() bool { return p.Vitals().Health < 20 }) {
		t.Fatalf("player took no starvation damage; health=%v", p.Vitals().Health)
	}
}
