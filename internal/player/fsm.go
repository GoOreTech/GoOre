// This file contains the per-connection FSM and packet dispatcher.
// The state type and its constants are defined in player.go.

package player

import (
	"io"
	"log/slog"
	"runtime/debug"
	"time"

	"goore/internal/protocol"
	v772 "goore/internal/protocol/v772"
)

// HandleConn runs the full client lifecycle: handshake → login → config → play.
// A deferred recover wraps the whole lifecycle so a panic in a handler is logged
// and the connection is closed cleanly. See docs/player.md §HandleConn.
func (p *Player) HandleConn() {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("HandleConn panic",
				"name", p.Name, "eid", p.EID,
				"panic", r)
			// Best-effort: also surface the stack so a crash
			// log includes enough context to debug.
			slog.Error("HandleConn panic stack",
				"stack", string(debug.Stack()))
		}
	}()
	defer func() {
		prevState := p.State()
		p.setState(stateDisconnected)
		if p.keepAliveTicker != nil {
			p.keepAliveTicker.Stop()
		}
		// Wake the keep-alive goroutine so it can exit. Without
		// this close, the goroutine is stuck on
		//   select {
		//   case <-p.keepAliveTicker.C:  // stopped above → never fires
		//   case <-p.disconnect:          // never closed → never fires
		//   }
		// forever, leaking one goroutine per client connection.
		close(p.disconnect)
		slog.Info("player disconnected", "name", p.Name)

		// Save on disconnect only if the player actually reached statePlay —
		// skip handshake/status/login bailouts to avoid junk files (port scans).
		if prevState == statePlay {
			p.hooks.OnLeavePlay(p)
			if p.Cfg.WorldDir != "" {
				if err := p.hooks.OnDisconnect(p); err != nil {
					slog.Warn("save on disconnect failed", "name", p.Name, "err", err)
				} else {
					slog.Info("saved player on disconnect", "name", p.Name, "uuid", p.UUID)
				}
			}
		}
	}()

	tmp := make([]byte, 4096)
	var buf []byte // accumulated buffer — TCP may deliver multiple packets in one Read()

	for p.State() != stateDisconnected {
		// Only read from conn when buffer is empty — drain complete packets first
		n, err := p.Conn.Read(tmp)
		if err != nil {
			if err != io.EOF {
				slog.Warn("read error", "name", p.Name, "err", err)
			}
			return
		}
		buf = append(buf, tmp[:n]...)

		// Drain all complete packets from the buffer.
		for {
			if len(buf) == 0 {
				break
			}

			r := protocol.NewWireReader(buf)
			pktLen := r.VarInt()
			if r.Err() != nil {
				break // incomplete — need more bytes
			}
			pktLenBytes := len(buf) - r.Remaining()

			totalLen := pktLenBytes + int(pktLen)
			if len(buf) < totalLen {
				break // incomplete — need more bytes
			}

			body := buf[pktLenBytes:totalLen]
			bodyReader := protocol.NewWireReader(body)
			pktID := bodyReader.VarInt()
			if bodyReader.Err() != nil {
				slog.Warn("packet read error", "err", bodyReader.Err())
				return
			}

			idBytes := len(body) - bodyReader.Remaining()
			pktData := body[idBytes:]
			buf = buf[totalLen:]

			p.dispatch(pktID, pktData)
		}
	}
}

// dispatch routes a packet to its handler based on state and packet ID.
func (p *Player) dispatch(pktID int32, data []byte) {
	switch p.State() {
	case stateHandshake:
		p.handleHandshake(pktID, data)
	case stateStatus:
		p.handleStatus(pktID, data)
	case stateLogin:
		if pktID == v772.LoginStart {
			p.handleLogin(pktID, data)
		} else if pktID == v772.LoginAcknowledged {
			p.handleLoginAck(pktID)
		}
	case stateConfiguration:
		p.handleConfiguration(pktID, data)
	case statePlay:
		p.handlePlay(pktID, data)
	}
}

func (p *Player) handleHandshake(pktID int32, data []byte) {
	if pktID != v772.HandshakeIntention {
		return
	}
	r := protocol.NewWireReader(data)
	protoVer := r.VarInt()
	host := r.String()
	port := r.Uint16()
	nextState := r.VarInt()
	if r.Err() != nil {
		return
	}

	slog.Info("handshake", "version", protoVer, "host", host, "port", port, "next_state", nextState)

	switch nextState {
	case v772.HandshakeStateStatus:
		p.setState(stateStatus)
	case v772.HandshakeStateLogin:
		p.setState(stateLogin)
	}
}

func (p *Player) handleStatus(pktID int32, data []byte) {
	switch pktID {
	case v772.StatusRequest:
		pkt := p.Proto.WriteStatusResponse(772, v772.Version,
			p.Cfg.MOTD, "",
			0, int32(p.Cfg.MaxPlayers))
		p.SendPacket(pkt)
	case v772.StatusPong:
		// Echo back the timestamp; client disconnects after pong.
		r := protocol.NewWireReader(data)
		ts := r.Int64()
		if r.Err() != nil {
			return
		}
		var w protocol.WireWriter
		w.Int64(ts)
		if w.Err() == nil {
			p.SendPacket(protocol.MakePacket(v772.StatusPing, w.Bytes()))
		}
		p.setState(stateDisconnected)
	}
}

func (p *Player) handleLogin(pktID int32, data []byte) {
	if pktID != v772.LoginStart {
		return
	}
	r := protocol.NewWireReader(data)
	name := r.String()
	pUUID := r.UUID()
	if r.Err() != nil {
		return
	}

	p.Name = name
	p.UUID = pUUID

	slog.Info("login", "name", name, "uuid", pUUID)

	// Load saved state (pos, hotbar, etc.) BEFORE LoginSuccess so the client gets
	// the saved position in the same packet flow. Don't fail the login for a corrupt save.
	if err := p.LoadStateFromDisk(p.Cfg.WorldDir); err != nil {
		slog.Warn("failed to load saved player state; using defaults", "name", name, "err", err)
	} else {
		px, py, pz, yaw, pitch, _ := p.Pos()
		p.hotbarMu.RLock()
		heldSlot := p.HeldSlot
		p.hotbarMu.RUnlock()
		slog.Info("login state", "name", name, "x", px, "y", py, "z", pz,
			"yaw", yaw, "pitch", pitch, "held_slot", heldSlot)
	}

	// Validate the loaded position is safe (not inside a solid block). See docs/regressions.md #6.
	if p.World != nil {
		px, py, pz, _, _, _ := p.Pos()
		sx, sy, sz := p.World.FindSafeSpawn(px, py, pz)
		if sx != px || sy != py || sz != pz {
			slog.Warn("saved position is unsafe, moved to safe spawn",
				"saved_x", px, "saved_y", py, "saved_z", pz,
				"safe_x", sx, "safe_y", sy, "safe_z", sz)
			p.posMu.Lock()
			p.X, p.Y, p.Z = sx, sy, sz
			p.posMu.Unlock()
		}
	}

	p.SendPacket(p.Proto.WriteLoginSuccess(pUUID, name))
	p.setState(stateLogin) // wait for login ack
}

func (p *Player) handleLoginAck(pktID int32) {
	if pktID != v772.LoginAcknowledged {
		return
	}
	slog.Info("login ack", "name", p.Name)
	p.setState(stateConfiguration)
	p.sendConfiguration()
}

func (p *Player) sendConfiguration() {
	// Known packs — empty
	p.SendPacket(p.Proto.WriteSelectKnownPacks())

	for _, rpkt := range p.Proto.WriteRegistries() {
		p.SendPacket(rpkt)
	}

	p.SendPacket(p.Proto.WriteFinishConfiguration())
}

func (p *Player) handleConfiguration(pktID int32, data []byte) {
	switch pktID {
	case v772.ConfigFinishConfig: // 0x03 serverbound — client done with config
		slog.Info("config finish", "name", p.Name)
		p.enterPlay()
	case v772.ConfigSelectKnownPacks: // 0x07 serverbound
		// Client selected known packs
	case v772.ConfigSettings: // 0x00 serverbound
		// Client settings — accepted silently
	case v772.ConfigAcknowledged: // 0x0F serverbound
		slog.Info("config ack", "name", p.Name)
		// Already in play, nothing to do
	}
}

func (p *Player) enterPlay() {
	p.setState(statePlay)
	// First-frame choreography lives in spawn.go (Send + 4 sub-phases).
	if err := Send(p); err != nil {
		slog.Warn("spawn sequence failed", "name", p.Name, "err", err)
	}
}

// startKeepAlive launches the keep-alive goroutine (5s ticker). Exits when p.disconnect is closed.
func (p *Player) startKeepAlive() {
	p.keepAliveTicker = time.NewTicker(5 * time.Second)
	go func() {
		for {
			select {
			case <-p.keepAliveTicker.C:
				id := p.keepAliveID.Add(1)
				pkt := p.Proto.WriteKeepAlive(id)
				p.SendPacket(pkt)
			case <-p.disconnect:
				return
			}
		}
	}()
}

// handlePlay dispatches play-state serverbound packets. See docs/player.md §Dispatch table.
func (p *Player) handlePlay(pktID int32, data []byte) {
	ids := p.Proto.PacketIDs()

	switch {
	case pktID == ids.Serverbound["keep_alive"]:
		// ignore
	case pktID == ids.Serverbound["set_player_position"]:
		p.handlePosition(data)
	case pktID == ids.Serverbound["set_player_position_and_rotation"]:
		p.handlePositionAndRotation(data)
	case pktID == ids.Serverbound["set_player_rotation"]:
		p.handleRotation(data)
	case pktID == ids.Serverbound["set_player_movement_flags"]:
		_ = data // on_ground/horizontal_collision; not stored server-side
	case pktID == ids.Serverbound["flying"]:
		_ = data
	case pktID == ids.Serverbound["tick_end"]:
		_ = data
	case pktID == ids.Serverbound["entity_action"]:
		_ = data // sneak/sprint — not tracked
	case pktID == ids.Serverbound["player_input"]:
		_ = data
	case pktID == ids.Serverbound["teleport_confirm"]:
		// ignore
	case pktID == ids.Serverbound["chat_command"]:
		p.handleChatCommand(data)
	case pktID == ids.Serverbound["player_action"]:
		p.handlePlayerAction(data)
	case pktID == ids.Serverbound["block_place"]:
		// 1.21.8: right-click. See docs/regressions.md #8.
		p.handleBlockPlace(data)
	case pktID == ids.Serverbound["use_item"]:
		// Right-click on air: start eating if the held item is food.
		p.beginEating()
	case pktID == ids.Serverbound["swing_arm"]:
		// ignore
	case pktID == ids.Serverbound["held_item_slot"]:
		p.handleHeldItemSlot(data)
		p.cancelEating() // switching slots interrupts eating
	case pktID == ids.Serverbound["player_loaded"]:
		slog.Info("player loaded all chunks", "name", p.Name)
	case pktID == ids.Serverbound["chunk_batch_received"]:
		// No-op; client uses for throttling, no response needed.
	case pktID == ids.Serverbound["set_creative_slot"]:
		p.handleSetCreativeSlot(data)
	case pktID == ids.Serverbound["client_command"]:
		p.handleClientCommand(data)
	default:
		slog.Warn("unknown packet", "packet_id", pktID, "name", p.Name)
	}
}
