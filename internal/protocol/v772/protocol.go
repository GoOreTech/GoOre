package v772

import (
	"strconv"

	"goore/internal/protocol"
)

// Protocol implements protocol.ProtocolVersion for Minecraft 1.21.8 (protocol 772).
type Protocol struct{}

// Compile-time check that Protocol satisfies ProtocolVersion.
var _ protocol.ProtocolVersion = (*Protocol)(nil)

// New creates a new Protocol instance for 1.21.8.
func New() *Protocol { return &Protocol{} }

// WriteStatusResponse encodes a Status Response packet (0x00) with JSON payload.
// JSON shape is fixed by vanilla server-list-ping: { version, players, description, favicon? }.
// Built by hand (raw concatenation) to keep the "sample":[] trick tight.
func (p *Protocol) WriteStatusResponse(version, protoVer int32, description, favicon string,
	playersOnline, maxPlayers int32) []byte {
	descJSON := strconv.AppendQuote(nil, description)
	// Build the inner fields; the root object is closed LAST so the
	// favicon branch can splice in an extra field before the closing
	// brace. Previously the root `}` was only written in the favicon
	// branch, which left the no-favicon JSON unbalanced — the vanilla
	// client rejected it as invalid JSON and the server list showed
	// "can't connect" even though direct join (login flow) worked.
	inner := `"version":{"name":"GoOre ` + VersionName + `","protocol":` +
		strconv.FormatInt(int64(protoVer), 10) +
		`},"players":{"max":` + strconv.FormatInt(int64(maxPlayers), 10) +
		`,"online":` + strconv.FormatInt(int64(playersOnline), 10) +
		`,"sample":[]},"description":{"text":` + string(descJSON) + `}`
	if favicon != "" {
		favJSON := strconv.AppendQuote(nil, favicon)
		inner += `,"favicon":` + string(favJSON)
	}
	json := `{` + inner + `}`

	var w protocol.WireWriter
	w.String(json)
	if w.Err() != nil {
		return nil
	}
	return protocol.MakePacket(StatusServerInfo, w.Bytes())
}

// Version returns the protocol version number.
func (p *Protocol) Version() int32 { return Version }

// VersionName returns the Minecraft version string.
func (p *Protocol) VersionName() string { return VersionName }

// PacketIDs returns the packet ID map for dispatching.
func (p *Protocol) PacketIDs() protocol.PacketIDMap { return PacketIDs() }

// WriteLoginSuccess encodes a Login Success packet (0x02).
func (p *Protocol) WriteLoginSuccess(uuid [16]byte, name string) []byte {
	var w protocol.WireWriter
	w.UUID(uuid)
	w.String(name)
	w.VarInt(0) // 0 properties
	if w.Err() != nil {
		return nil
	}
	pkt := protocol.MakePacket(LoginSuccess, w.Bytes())
	return pkt
}

// WriteSelectKnownPacks encodes a Select Known Packs packet (0x0E).
// Sends minecraft:core version 1.21.8; client uses built-in registry data (hasData=false).
func (p *Protocol) WriteSelectKnownPacks() []byte {
	var w protocol.WireWriter
	w.VarInt(1)           // 1 known pack: minecraft:core
	w.String("minecraft") // namespace
	w.String("core")      // id
	w.String("1.21.8")    // version
	if w.Err() != nil {
		return nil
	}
	return protocol.MakePacket(ConfigSelectKnownPacksCli, w.Bytes())
}

// WriteFinishConfiguration encodes a Finish Configuration packet (0x03).
func (p *Protocol) WriteFinishConfiguration() []byte {
	return protocol.MakePacket(ConfigFinishConfig, nil)
}

// WriteRegistries encodes all Registry Data packets (0x07); one packet per registry.
func (p *Protocol) WriteRegistries() [][]byte {
	return configRegistries()
}

// WriteLoginPlay encodes the Login (Play) packet (0x2B). Fields match vanilla 1.21.8 ClientboundLoginPacket.
// See docs/protocol.md §Login Play for the CommonPlayerSpawnInfo block.
func (p *Protocol) WriteLoginPlay(entityID int32, hardcore bool, gamemode uint8,
	dimensionNames []string, dimensionType string,
	seed int64, maxPlayers int32, viewDistance int32,
	simulationDistance int32, reducedDebugInfo bool,
	enableRespawnScreen bool, doLimitedCrafting bool,
	dimensionID int32, deathDimension string,
	deathLocation [8]byte, portalCooldown int32) []byte {

	var w protocol.WireWriter
	w.Int32(entityID)
	w.Bool(hardcore)
	w.VarInt(int32(len(dimensionNames)))
	for _, name := range dimensionNames {
		w.String(name)
	}
	w.VarInt(maxPlayers)
	w.VarInt(viewDistance)
	w.VarInt(simulationDistance)
	w.Bool(reducedDebugInfo)
	w.Bool(enableRespawnScreen)
	w.Bool(doLimitedCrafting)

	// CommonPlayerSpawnInfo: dimensionType=overworld(0), inline dimension name, seed, gamemode, prevGamemode=-1,
	// isDebug=false, isFlat=true (superflat), hasDeath=false, portalCooldown, seaLevel=63.
	w.VarInt(0)
	w.String(dimensionNames[dimensionID])
	w.Int64(seed)
	w.Byte(gamemode)
	w.Byte(0xFF) // previous game type = -1
	w.Bool(false)
	w.Bool(true)  // is flat
	w.Bool(false) // has death location
	w.VarInt(portalCooldown)
	w.VarInt(63)  // sea level
	w.Bool(false) // enforces secure chat

	if w.Err() != nil {
		return nil
	}
	return protocol.MakePacket(PlayLogin, w.Bytes())
}

// WritePosition encodes a Synchronize Player Position packet (0x41).
// 1.21.8 format: teleportID + pos(3×f64) + velocity(3×f64) + yaw/pitch(2×f32) + flags(i32).
func (p *Protocol) WritePosition(x, y, z float64, yaw, pitch float32, flags uint8, teleportID int32) []byte {
	var w protocol.WireWriter
	w.VarInt(teleportID)
	w.Float64(x)
	w.Float64(y)
	w.Float64(z)
	w.Float64(0) // velocity x
	w.Float64(0) // velocity y
	w.Float64(0) // velocity z
	w.Float32(yaw)
	w.Float32(pitch)
	w.Int32(0) // relative flags: all absolute
	if w.Err() != nil {
		return nil
	}
	return protocol.MakePacket(PlayPlayerPos, w.Bytes())
}

// WriteAbilities encodes a Player Abilities packet (0x39). Format: flags(i8) + flyingSpeed(f32) + walkingSpeed(f32).
// creative_mode flag (0x08) is required for creative behaviour; see docs/protocol.md §Player Abilities.
func (p *Protocol) WriteAbilities(flags uint8) []byte {
	var w protocol.WireWriter
	w.Byte(flags)
	w.Float32(0.05) // flying speed
	w.Float32(0.1)  // walking speed (NOT fovModifier — that's a 1.20.x leftover)
	if w.Err() != nil {
		return nil
	}
	return protocol.MakePacket(PlayAbilities, w.Bytes())
}

// WriteChunkData encodes a map_chunk packet (0x27). chunkData is the body after X/Z (heightmaps + sections + block entities + light data).
func (p *Protocol) WriteChunkData(chunkData []byte, pos protocol.ChunkPos) []byte {
	var w protocol.WireWriter
	w.Int32(pos.X)
	w.Int32(pos.Z)
	w.RawWrite(chunkData)

	if w.Err() != nil {
		return nil
	}
	return protocol.MakePacket(PlayMapChunk, w.Bytes())
}

// WriteUpdateLight encodes an update_light packet (0x2A). Unused — light data is inline in map_chunk.
func (p *Protocol) WriteUpdateLight(chunkData []byte, pos protocol.ChunkPos) []byte {
	var w protocol.WireWriter
	w.VarInt(pos.X)
	w.VarInt(pos.Z)
	w.Bool(true) // trust edges
	w.VarInt(0)  // sky light mask (empty)
	w.VarInt(0)  // block light mask (empty)
	w.VarInt(0)  // empty sky light mask (all empty)
	w.VarInt(0)  // empty block light mask (all empty)
	w.VarInt(0)  // sky light arrays (none)
	w.VarInt(0)  // block light arrays (none)
	if w.Err() != nil {
		return nil
	}
	return protocol.MakePacket(PlayUpdateLight, w.Bytes())
}

// WriteKeepAlive encodes a Keep Alive packet (0x26).
func (p *Protocol) WriteKeepAlive(id int64) []byte {
	var w protocol.WireWriter
	w.Int64(id)
	if w.Err() != nil {
		return nil
	}
	return protocol.MakePacket(PlayKeepAlive, w.Bytes())
}

// WriteUpdateTime encodes an Update Time packet (0x6A).
func (p *Protocol) WriteUpdateTime(age, time int64) []byte {
	var w protocol.WireWriter
	w.Int64(age)
	w.Int64(time)
	if w.Err() != nil {
		return nil
	}
	return protocol.MakePacket(PlayUpdateTime, w.Bytes())
}

// WriteBlockUpdate encodes a Block Update packet (0x08).
func (p *Protocol) WriteBlockUpdate(pos protocol.BlockPos, blockStateID int32) []byte {
	var w protocol.WireWriter
	w.Int64((int64(pos.X)&0x3FFFFFF)<<38 | (int64(pos.Z)&0x3FFFFFF)<<12 | (int64(pos.Y) & 0xFFF))
	w.VarInt(blockStateID)
	if w.Err() != nil {
		return nil
	}
	return protocol.MakePacket(PlayBlockUpdate, w.Bytes())
}

// WriteAckPlayerDigging encodes an Acknowledge Player Digging packet (0x04). Format: sequenceId(VarInt).
func (p *Protocol) WriteAckPlayerDigging(sequenceID int32) []byte {
	var w protocol.WireWriter
	w.VarInt(sequenceID)
	if w.Err() != nil {
		return nil
	}
	return protocol.MakePacket(PlayAckPlayerDigging, w.Bytes())
}

// WriteSetSlot encodes a Set Container Slot packet (0x14). Format: windowId(u8) + stateId(VarInt) + slot(i16) + item(Slot).
func (p *Protocol) WriteSetSlot(windowID uint8, stateID int32, slot int16, item protocol.Slot) []byte {
	var w protocol.WireWriter
	w.Byte(windowID)
	w.VarInt(stateID)
	w.Int16(slot)
	encodeSlot(&w, item)
	if w.Err() != nil {
		return nil
	}
	return protocol.MakePacket(PlaySetSlot, w.Bytes())
}

// WriteHeldItemSlot encodes a Held Item Slot packet (0x62). Format: slot(VarInt).
func (p *Protocol) WriteHeldItemSlot(slot uint8) []byte {
	var w protocol.WireWriter
	w.VarInt(int32(slot))
	if w.Err() != nil {
		return nil
	}
	return protocol.MakePacket(PlayHeldItemSlot, w.Bytes())
}

// WriteSpawnPosition encodes a Spawn Position packet (0x5A).
func (p *Protocol) WriteSpawnPosition(pos protocol.BlockPos, angle float32) []byte {
	var w protocol.WireWriter
	w.Int64((int64(pos.X)&0x3FFFFFF)<<38 | (int64(pos.Z)&0x3FFFFFF)<<12 | (int64(pos.Y) & 0xFFF))
	w.Float32(angle)
	if w.Err() != nil {
		return nil
	}
	return protocol.MakePacket(PlaySpawnPos, w.Bytes())
}

// WriteHealth encodes an Update Health packet (0x61).
func (p *Protocol) WriteHealth(health float32, food int32, saturation float32) []byte {
	var w protocol.WireWriter
	w.Float32(health)
	w.VarInt(food)
	w.Float32(saturation)
	if w.Err() != nil {
		return nil
	}
	return protocol.MakePacket(PlayUpdateHealth, w.Bytes())
}

// WriteSystemChat encodes a System Chat packet (0x72).
func (p *Protocol) WriteSystemChat(message string) []byte {
	var w protocol.WireWriter
	w.String(message)
	w.Bool(false) // overlay = false
	if w.Err() != nil {
		return nil
	}
	return protocol.MakePacket(PlaySystemChat, w.Bytes())
}

// WriteGameEvent encodes a Game Event packet (0x22).
func (p *Protocol) WriteGameEvent(event int8, value float32) []byte {
	var w protocol.WireWriter
	w.Byte(uint8(event))
	w.Float32(value)
	if w.Err() != nil {
		return nil
	}
	return protocol.MakePacket(PlayGameStateChange, w.Bytes())
}

// WriteStartWaitingForChunks encodes a Game Event (0x22) with event type 13 (start waiting for level chunks).
func (p *Protocol) WriteStartWaitingForChunks() []byte {
	var w protocol.WireWriter
	w.Byte(13)     // event type: start waiting for level chunks
	w.Float32(0.0) // value
	if w.Err() != nil {
		return nil
	}
	return protocol.MakePacket(PlayGameStateChange, w.Bytes())
}

// WriteSetCenterChunk encodes an Update View Position packet (0x57).
func (p *Protocol) WriteSetCenterChunk(x, z int32) []byte {
	var w protocol.WireWriter
	w.VarInt(x)
	w.VarInt(z)
	if w.Err() != nil {
		return nil
	}
	return protocol.MakePacket(PlayViewPosition, w.Bytes())
}

// WriteChunkBatchStart encodes a Chunk Batch Start packet (0x0C). 1.21+ requires batch wrapping around chunks; see docs/protocol.md §Chunk Batch System.
func (p *Protocol) WriteChunkBatchStart() []byte {
	return protocol.MakePacket(PlayChunkBatchStart, nil)
}

// WriteChunkBatchFinished encodes a Chunk Batch Finished packet (0x0B). batchSize = total wire bytes of chunks in the batch.
func (p *Protocol) WriteChunkBatchFinished(batchSize int32) []byte {
	var w protocol.WireWriter
	w.VarInt(batchSize)
	if w.Err() != nil {
		return nil
	}
	return protocol.MakePacket(PlayChunkBatchFinished, w.Bytes())
}

// WriteSetViewDistance encodes an Update View Distance packet (0x58).
func (p *Protocol) WriteSetViewDistance(distance int32) []byte {
	var w protocol.WireWriter
	w.VarInt(distance)
	if w.Err() != nil {
		return nil
	}
	return protocol.MakePacket(PlayViewDistance, w.Bytes())
}

// WriteSetSimulationDistance encodes a Simulation Distance packet (0x68).
func (p *Protocol) WriteSetSimulationDistance(distance int32) []byte {
	var w protocol.WireWriter
	w.VarInt(distance)
	if w.Err() != nil {
		return nil
	}
	return protocol.MakePacket(PlaySimulationDistance, w.Bytes())
}

// WriteContainerItems encodes a Set Container Content packet (0x12). Layout: windowId + stateId + count + slots[count] + carriedItem (separate field).
func (p *Protocol) WriteContainerItems(windowID uint8, stateID int32, slots []protocol.Slot, carried protocol.Slot) []byte {
	var w protocol.WireWriter
	w.VarInt(int32(windowID))
	w.VarInt(stateID)
	w.VarInt(int32(len(slots))) // count = len(slots) only; carried is a separate field
	for _, s := range slots {
		encodeSlot(&w, s)
	}
	encodeSlot(&w, carried) // carriedItem (separate field)
	if w.Err() != nil {
		return nil
	}
	return protocol.MakePacket(PlayWindowItems, w.Bytes())
}

// encodeSlot writes the 1.21+ Slot data format: count(VarInt) + if>0: itemId(VarInt) + DataComponentPatch.
// Holder<Item> uses VarInt(registryId + 1); the inverse -1 is applied in handleSetCreativeSlot. See docs/protocol.md §Holder<Item>.
func encodeSlot(w *protocol.WireWriter, s protocol.Slot) {
	if !s.Present || s.Count == 0 {
		w.VarInt(0)
		return
	}
	w.VarInt(int32(s.Count))
	w.VarInt(s.ItemID + 1) // Holder<Item>: +1 offset (0 = inline from datapack)
	w.VarInt(0)            // DataComponentPatch: components_with_data_count
	w.VarInt(0)            // DataComponentPatch: components_without_data_count
}

// WriteSpawnEntity encodes a Spawn Entity packet (0x01). Used to make a player entity visible to other clients. See docs/protocol.md §Spawn Entity.
func (p *Protocol) WriteSpawnEntity(entityID int32, uuid [16]byte, entityType int32, x, y, z float64, pitch, yaw, headPitch int8) []byte {
	var w protocol.WireWriter
	w.VarInt(entityID)
	w.UUID(uuid)
	w.VarInt(entityType)
	w.Float64(x)
	w.Float64(y)
	w.Float64(z)
	w.Byte(uint8(pitch))
	w.Byte(uint8(yaw))
	w.Byte(uint8(headPitch))
	w.VarInt(0) // objectData
	w.Int16(0)  // velocityX
	w.Int16(0)  // velocityY
	w.Int16(0)  // velocityZ
	if w.Err() != nil {
		return nil
	}
	return protocol.MakePacket(PlaySpawnEntity, w.Bytes())
}

// WriteRemoveEntities encodes a Remove Entities packet (0x46).
func (p *Protocol) WriteRemoveEntities(ids []int32) []byte {
	var w protocol.WireWriter
	w.VarInt(int32(len(ids)))
	for _, id := range ids {
		w.VarInt(id)
	}
	if w.Err() != nil {
		return nil
	}
	return protocol.MakePacket(PlayRemoveEntities, w.Bytes())
}

// WriteRelEntityMove encodes a small relative entity move (0x2E). NOT YET WIRED — kept for Phase 5 bandwidth optimization.
func (p *Protocol) WriteRelEntityMove(entityID int32, deltaX, deltaY, deltaZ int16, onGround bool) []byte {
	var w protocol.WireWriter
	w.VarInt(entityID)
	w.Int16(deltaX)
	w.Int16(deltaY)
	w.Int16(deltaZ)
	w.Bool(onGround)
	if w.Err() != nil {
		return nil
	}
	return protocol.MakePacket(PlayRelEntityMove, w.Bytes())
}

// WriteEntityMoveLook encodes an entity move-and-rotation (0x2F). NOT YET WIRED.
func (p *Protocol) WriteEntityMoveLook(entityID int32, deltaX, deltaY, deltaZ int16, yaw, pitch int8, onGround bool) []byte {
	var w protocol.WireWriter
	w.VarInt(entityID)
	w.Int16(deltaX)
	w.Int16(deltaY)
	w.Int16(deltaZ)
	w.Byte(uint8(yaw))
	w.Byte(uint8(pitch))
	w.Bool(onGround)
	if w.Err() != nil {
		return nil
	}
	return protocol.MakePacket(PlayEntityMoveLook, w.Bytes())
}

// WriteEntityLook encodes an entity rotation-only update (0x31). NOT YET WIRED.
func (p *Protocol) WriteEntityLook(entityID int32, yaw, pitch int8, onGround bool) []byte {
	var w protocol.WireWriter
	w.VarInt(entityID)
	w.Byte(uint8(yaw))
	w.Byte(uint8(pitch))
	w.Bool(onGround)
	if w.Err() != nil {
		return nil
	}
	return protocol.MakePacket(PlayEntityLook, w.Bytes())
}

// WriteEntityHeadRotation encodes a head-only rotation (0x4C). Independent channel from body yaw.
func (p *Protocol) WriteEntityHeadRotation(entityID int32, headYaw int8) []byte {
	var w protocol.WireWriter
	w.VarInt(entityID)
	w.Byte(uint8(headYaw))
	if w.Err() != nil {
		return nil
	}
	return protocol.MakePacket(PlayEntityHeadRotation, w.Bytes())
}

// WriteEntityEquipment encodes a Set Equipment packet (0x5F).
// top bit (0x80) of the slot byte is CONTINUE_MASK (inverted from minecraft-data docs). For a single/last entry, the top bit MUST be clear. See docs/protocol.md §Set Equipment.
func (p *Protocol) WriteEntityEquipment(entityID int32, slot int8, item protocol.Slot) []byte {
	var w protocol.WireWriter
	w.VarInt(entityID)
	w.Byte(uint8(slot)) // top bit CLEAR: this is the last (and only) entry
	encodeSlot(&w, item)
	if w.Err() != nil {
		return nil
	}
	return protocol.MakePacket(PlayEntityEquipment, w.Bytes())
}

// WriteEntityTeleport encodes an absolute teleport (0x76). 1.21.2+ format (PositionMoveRotation); pre-1.21.2 format is REJECTED by vanilla 1.21.8. For absolute teleport: delta=0, relatives=0 (all bits clear = all absolute). See docs/protocol.md §Entity Teleport.
func (p *Protocol) WriteEntityTeleport(entityID int32, x, y, z float64, yaw, pitch float32, onGround bool) []byte {
	var w protocol.WireWriter
	w.VarInt(entityID)
	w.Float64(x) // pos
	w.Float64(y)
	w.Float64(z)
	w.Float64(0) // delta (0 for absolute)
	w.Float64(0)
	w.Float64(0)
	w.Float32(yaw) // yRot, xRot (f32 degrees, NOT i8 as in pre-1.21.2)
	w.Float32(pitch)
	w.Uint32(0) // Set<Relative> bitmask (u32 BE) — all-absolute
	w.Bool(onGround)
	if w.Err() != nil {
		return nil
	}
	return protocol.MakePacket(PlayEntityTeleport, w.Bytes())
}

// WriteEntityMetadata encodes entity metadata (0x5C). Entries are (key, type, value), terminated by 0xFF. See docs/protocol.md §Entity Metadata.
func (p *Protocol) WriteEntityMetadata(entityID int32, entries []protocol.MetadataEntry) []byte {
	var w protocol.WireWriter
	w.VarInt(entityID)
	for _, e := range entries {
		w.Byte(e.Key)
		w.VarInt(int32(e.Type))
		switch e.Type {
		case protocol.MetadataTypeByte:
			if v, ok := e.Value.(int8); ok {
				w.Byte(uint8(v))
			}
		case protocol.MetadataTypeBoolean:
			if v, ok := e.Value.(bool); ok {
				w.Bool(v)
			}
		case protocol.MetadataTypeVarInt, protocol.MetadataTypePose,
			protocol.MetadataTypeBlockSt, protocol.MetadataTypeParticle:
			if v, ok := e.Value.(int32); ok {
				w.VarInt(v)
			}
		case protocol.MetadataTypeFloat:
			if v, ok := e.Value.(float32); ok {
				w.Float32(v)
			}
		case protocol.MetadataTypeString:
			if v, ok := e.Value.(string); ok {
				w.String(v)
			}
		default:
			// Unsupported type — skip (still write terminator)
		}
	}
	w.Byte(0xFF) // terminator
	if w.Err() != nil {
		return nil
	}
	return protocol.MakePacket(PlayEntityMetadata, w.Bytes())
}

// WritePlayerInfoUpdate encodes a Player Info Update packet (0x3F). MUST come before spawn_entity. See docs/protocol.md §Player Info Update.
func (p *Protocol) WritePlayerInfoUpdate(actions uint8, entries []protocol.PlayerInfoEntry) []byte {
	var w protocol.WireWriter
	w.Byte(actions)
	w.VarInt(int32(len(entries)))
	for _, e := range entries {
		w.UUID(e.UUID)
		if actions&protocol.PlayerInfoActionAddPlayer != 0 {
			w.String(e.Name)
			w.VarInt(int32(len(e.Properties)))
			for _, prop := range e.Properties {
				w.String(prop.Name)
				w.String(prop.Value)
				w.Bool(prop.Signature != "")
				if prop.Signature != "" {
					w.String(prop.Signature)
				}
			}
		}
		if actions&protocol.PlayerInfoActionInitChat != 0 {
			// chat_session: not implemented. Always omit bit 0x02 unless we have a real session.
		}
		if actions&protocol.PlayerInfoActionUpdateGamemode != 0 {
			w.VarInt(e.Gamemode)
		}
		if actions&protocol.PlayerInfoActionUpdateListed != 0 {
			if e.Listed {
				w.VarInt(1)
			} else {
				w.VarInt(0)
			}
		}
		if actions&protocol.PlayerInfoActionUpdateLatency != 0 {
			w.VarInt(e.Latency)
		}
		if actions&0x20 != 0 {
			// displayName: optional chat — always omitted.
		}
		if actions&protocol.PlayerInfoActionUpdateHat != 0 {
			w.Bool(e.ShowHat)
		}
		if actions&0x80 != 0 {
			w.VarInt(e.ListPriority)
		}
	}
	if w.Err() != nil {
		return nil
	}
	return protocol.MakePacket(PlayPlayerInfoUpdate, w.Bytes())
}

// WritePlayerRemove encodes a Player Remove packet (0x3E). Format: count(VarInt) + UUID[count].
func (p *Protocol) WritePlayerRemove(uuids [][16]byte) []byte {
	var w protocol.WireWriter
	w.VarInt(int32(len(uuids)))
	for _, u := range uuids {
		w.UUID(u)
	}
	if w.Err() != nil {
		return nil
	}
	return protocol.MakePacket(PlayPlayerRemove, w.Bytes())
}

// WriteDamageEvent encodes a Damage Event packet (0x19). See docs/protocol.md §Damage Event.
// sourceCauseID / sourceDirectID use the vanilla OptionalInt convention: -1 means
// "no entity". The trailing sourcePosition is a boolean-prefixed Optional<Vec3>.
func (p *Protocol) WriteDamageEvent(entityID, sourceTypeID, sourceCauseID, sourceDirectID int32,
	hasSourcePos bool, sourceX, sourceY, sourceZ float64) []byte {
	var w protocol.WireWriter
	w.VarInt(entityID)
	w.VarInt(sourceTypeID)
	w.VarInt(sourceCauseID)
	w.VarInt(sourceDirectID)
	w.Bool(hasSourcePos)
	if hasSourcePos {
		w.Float64(sourceX)
		w.Float64(sourceY)
		w.Float64(sourceZ)
	}
	if w.Err() != nil {
		return nil
	}
	return protocol.MakePacket(PlayDamageEvent, w.Bytes())
}

// WriteEntityStatus encodes an Entity Event packet (0x1E). entityId is i32 BE
// (NOT a VarInt — a recurring trap, see docs/protocol.md §Entity Event); status
// is an i8 op code. status 3 triggers the death animation/sound for living entities.
func (p *Protocol) WriteEntityStatus(entityID int32, status int8) []byte {
	var w protocol.WireWriter
	w.Int32(entityID)
	w.Byte(uint8(status))
	if w.Err() != nil {
		return nil
	}
	return protocol.MakePacket(PlayEntityStatus, w.Bytes())
}

// WriteHurtAnimation encodes a Hurt Animation packet (0x24): the red damage
// flash + tilt. yaw is the player's body yaw (degrees) the tilt direction is derived from.
func (p *Protocol) WriteHurtAnimation(entityID int32, yaw float32) []byte {
	var w protocol.WireWriter
	w.VarInt(entityID)
	w.Float32(yaw)
	if w.Err() != nil {
		return nil
	}
	return protocol.MakePacket(PlayHurtAnimation, w.Bytes())
}

// WriteRespawn encodes a Respawn packet (0x4B). The body is a CommonPlayerSpawnInfo
// (SpawnInfo) block followed by a u8 copyMetadata flag. hasDeath=true writes the
// optional death location (dimensionName + BlockPos); used when respawning after
// death so the client remembers where the player died.
func (p *Protocol) WriteRespawn(dimensionType int32, dimensionName string, hashedSeed int64,
	gamemode, prevGamemode uint8, isDebug, isFlat bool,
	hasDeath bool, deathDimensionName string, deathPos protocol.BlockPos,
	portalCooldown, seaLevel int32, copyMetadata bool) []byte {
	var w protocol.WireWriter
	// SpawnInfo
	w.VarInt(dimensionType)
	w.String(dimensionName)
	w.Int64(hashedSeed)
	w.Byte(gamemode)
	w.Byte(prevGamemode)
	w.Bool(isDebug)
	w.Bool(isFlat)
	w.Bool(hasDeath)
	if hasDeath {
		w.String(deathDimensionName)
		w.Int64((int64(deathPos.X)&0x3FFFFFF)<<38 | (int64(deathPos.Z)&0x3FFFFFF)<<12 | (int64(deathPos.Y) & 0xFFF))
	}
	w.VarInt(portalCooldown)
	w.VarInt(seaLevel)
	// copyMetadata (u8): 0 = fresh metadata, 1 = keep metadata on respawn (used by
	// vanilla for non-death respawns so attributes like sneaking survive).
	if copyMetadata {
		w.Byte(1)
	} else {
		w.Byte(0)
	}
	if w.Err() != nil {
		return nil
	}
	return protocol.MakePacket(PlayRespawn, w.Bytes())
}
