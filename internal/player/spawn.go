// Package player — first-frame spawn sequence. 30+ packets in a fixed order:
// sendLoginFrame, sendChunkBatches, sendInventorySeed, sendPostSpawn. Bails on
// first error (after a partial spawn, the client's renderer is in an
// indeterminate state). See docs/player.md §First-frame spawn sequence.
package player

import (
	"log/slog"

	"goore/internal/protocol"
	"goore/internal/world"
)

// Send runs the full first-frame spawn sequence. Returns the first error from any sub-phase; subsequent phases are NOT attempted after a failure.
func Send(p *Player) error {
	if err := sendLoginFrame(p); err != nil {
		slog.Warn("spawn loginFrame failed", "name", p.Name, "err", err)
		return err
	}
	if err := sendChunkBatches(p); err != nil {
		slog.Warn("spawn chunkBatches failed", "name", p.Name, "err", err)
		return err
	}
	if err := sendInventorySeed(p); err != nil {
		slog.Warn("spawn inventorySeed failed", "name", p.Name, "err", err)
		return err
	}
	if err := sendPostSpawn(p); err != nil {
		slog.Warn("spawn postSpawn failed", "name", p.Name, "err", err)
		return err
	}
	return nil
}

// sendLoginFrame sends the 7 packets that establish identity, position, and view setup. Order matters.
func sendLoginFrame(p *Player) error {
	// 1) Login Play — EID, gamemode, dimensions, view distance.
	gm := p.Vitals().Gamemode
	if err := p.SendPacket(p.Proto.WriteLoginPlay(
		p.EID,
		false, // hardcore
		gm,    // gamemode (from config / saved state)
		[]string{"minecraft:overworld", "minecraft:the_nether", "minecraft:the_end"},
		"minecraft:overworld",
		p.World.Seed,
		20,             // max players
		p.Cfg.ViewDist, // view distance
		p.Cfg.ViewDist, // simulation distance
		false,          // reduced debug
		true,           // enable respawn screen
		false,          // limited crafting
		0,              // dimension ID
		"minecraft:overworld",
		[8]byte{},
		0, // portal cooldown
	)); err != nil {
		return err
	}

	// 2) Spawn position — world spawn (used for compass, respawn).
	spawnPos := protocol.BlockPos{X: 0, Y: 4, Z: 0}
	if err := p.SendPacket(p.Proto.WriteSpawnPosition(spawnPos, 0)); err != nil {
		return err
	}

	// 3) Abilities — per gamemode (creative/spectator can fly; survival can't).
	if err := p.SendPacket(p.Proto.WriteAbilities(abilitiesFor(gm))); err != nil {
		return err
	}

	// 4) Position — teleport ID -1 (no ack expected).
	px, py, pz, yaw, pitch, _ := p.Pos()
	if err := p.SendPacket(p.Proto.WritePosition(px, py, pz, yaw, pitch, 0x00, -1)); err != nil {
		return err
	}

	// 5) Game event 13: start waiting for level chunks (must come BEFORE chunk data).
	if err := p.SendPacket(p.Proto.WriteStartWaitingForChunks()); err != nil {
		return err
	}

	// 6) Set center chunk — for load-priority purposes.
	if err := p.SendPacket(p.Proto.WriteSetCenterChunk(0, 0)); err != nil {
		return err
	}

	// 7) View distance.
	return p.SendPacket(p.Proto.WriteSetViewDistance(p.Cfg.ViewDist))
}

// sendChunkBatches sends the world chunks in 64-chunk batches, wrapped in chunk_batch_start / chunk_batch_finished. batchSize = total wire bytes of chunks in the batch. See docs/protocol.md §Chunk Batch System.
func sendChunkBatches(p *Player) error {
	const chunkBatchSize = 64 // vanilla default

	vd := int(p.Cfg.ViewDist)
	if err := p.SendPacket(p.Proto.WriteChunkBatchStart()); err != nil {
		return err
	}
	batchBytes := int32(0)
	batchCount := 0
	for cx := -vd; cx <= vd; cx++ {
		for cz := -vd; cz <= vd; cz++ {
			pos := world.ChunkPos{X: int32(cx), Z: int32(cz)}
			c := p.World.GetChunk(pos)
			chunkPkt := p.Proto.WriteMapChunk(c)
			if err := p.SendPacket(chunkPkt); err != nil {
				return err
			}
			batchBytes += int32(len(chunkPkt))
			batchCount++
			if batchCount >= chunkBatchSize {
				if err := p.SendPacket(p.Proto.WriteChunkBatchFinished(batchBytes)); err != nil {
					return err
				}
				if err := p.SendPacket(p.Proto.WriteChunkBatchStart()); err != nil {
					return err
				}
				batchBytes = 0
				batchCount = 0
			}
		}
	}
	if batchCount > 0 {
		if err := p.SendPacket(p.Proto.WriteChunkBatchFinished(batchBytes)); err != nil {
			return err
		}
	}
	return nil
}

// sendInventorySeed populates the client's hotbar and main inventory. Sends set_slot per hotbar item (some 1.21.x clients miss hotbar updates in container_set_content) + container_set_content + held_item_slot. Also seeds internal Player.Hotbar and Player.HeldItem.
func sendInventorySeed(p *Player) error {
	// 46 slots: 0=crafting result, 1-4=crafting input, 5-8=armor, 9-35=main, 36-44=hotbar, 45=offhand.
	slots := make([]protocol.Slot, 46)
	hotbarItems := []struct {
		ItemID  int32
		Comment string
	}{
		{1, "stone"},
		{27, "grass_block"},
		{28, "dirt"},
		{35, "cobblestone"},
		{36, "oak_planks"},
		{58, "bedrock"},
		{59, "sand"},
		{195, "glass"},
		{0, ""}, // empty
	}
	for i, it := range hotbarItems {
		if it.ItemID == 0 {
			continue
		}
		wireSlot := HotbarWire(i)
		slots[wireSlot] = protocol.Slot{Present: true, ItemID: it.ItemID, Count: 1}
		// Belt-and-suspenders: also set_slot per hotbar item.
		if err := p.SendPacket(p.Proto.WriteSetSlot(0, 0, wireSlot,
			protocol.Slot{Present: true, ItemID: it.ItemID, Count: 64})); err != nil {
			return err
		}
	}
	carried := protocol.Slot{} // empty

	// Mirror hotbar into Player.Hotbar so handlers can look up items by ID.
	p.hotbarMu.Lock()
	for i, it := range hotbarItems {
		p.Hotbar[i] = it.ItemID
		if it.ItemID == 0 {
			p.HotbarCount[i] = 0
		} else {
			p.HotbarCount[i] = 64
		}
	}
	p.HeldItem = p.Hotbar[0]
	p.hotbarMu.Unlock()

	if err := p.SendPacket(p.Proto.WriteContainerItems(0, 0, slots, carried)); err != nil {
		return err
	}
	// Sync the survival HUD (health/food/saturation) BEFORE held_item_slot so the
	// client's first-frame drain (which terminates on held_item_slot) consumes it.
	// Sending it after held_item_slot shifts every post-spawn packet in tests and
	// breaks the "first packet after spawn" contract. See docs/player.md §Spawn.
	p.sendHealth()
	return p.SendPacket(p.Proto.WriteHeldItemSlot(0))
}

// sendPostSpawn starts the keep-alive ticker and the survival tick, and fires
// OnEnterPlay. (UpdateHealth is sent in sendInventorySeed, before held_item_slot,
// so it lands inside the first-frame drain window.) p.hooks is guaranteed non-nil.
func sendPostSpawn(p *Player) error {
	p.startKeepAlive()
	p.startSurvivalTick()
	p.hooks.OnEnterPlay(p)
	return nil
}
