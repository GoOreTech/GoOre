// Package server — player visibility. One-way operations:
// "make player WHO visible to player TO" (5-packet sequence) and
// "remove WHO from TO's view" (2-packet sequence). Order is an
// invariant the vanilla client depends on. See docs/server.md §Player visibility.
package server

import (
	"log/slog"

	"goore/internal/player"
	"goore/internal/protocol"
)

// MakeVisible sends the 5-packet sequence that makes who visible to to. Order is an INVARIANT — the vanilla decoder reads packets in this exact order. See docs/server.md §MakeVisible.
func MakeVisible(who, to *player.Player) error {
	proto := to.Proto

	// 1) player_info_update (MUST come before spawn_entity)
	infoPkt := proto.WritePlayerInfoUpdate(
		protocol.PlayerInfoActionAddPlayer|
			protocol.PlayerInfoActionUpdateGamemode|
			protocol.PlayerInfoActionUpdateListed|
			protocol.PlayerInfoActionUpdateLatency,
		[]protocol.PlayerInfoEntry{{
			UUID:     who.UUID,
			Name:     who.Name,
			Gamemode: 1, // creative
			Listed:   true,
			Latency:  0,
			ShowHat:  true,
		}},
	)
	if err := to.SendPacket(infoPkt); err != nil {
		slog.Warn("send player_info failed", "from", who.EID, "to", to.EID, "err", err)
		return err
	}

	// 2) spawn_entity (headPitch=0; position-tick follows up with head rotation)
	whoX, whoY, whoZ, whoYaw, _, _ := who.Pos()
	spawnPkt := proto.WriteSpawnEntity(
		who.EID, who.UUID, PlayerEntityTypeID,
		whoX, whoY, whoZ, 0, 0, 0,
	)
	if err := to.SendPacket(spawnPkt); err != nil {
		slog.Warn("send spawn_entity failed", "from", who.EID, "to", to.EID, "err", err)
		return err
	}

	// 3) entity_metadata (minimum set for the client to render the player)
	metaPkt := proto.WriteEntityMetadata(who.EID, []protocol.MetadataEntry{
		protocol.MetadataByte(0, 0),     // shared_flags
		protocol.MetadataVarInt(1, 300), // air_supply
		protocol.MetadataByte(17, 0x7F), // skin_parts (all layers on)
	})
	if err := to.SendPacket(metaPkt); err != nil {
		slog.Warn("send entity_metadata failed", "from", who.EID, "to", to.EID, "err", err)
		return err
	}

	// 4) entity_equipment (main hand, slot 0). Always sent, even when empty.
	_, _, whoHeldItem := who.HotbarSnapshot()
	mainHandItem := protocol.Slot{}
	if whoHeldItem != 0 {
		mainHandItem = protocol.Slot{Present: true, ItemID: whoHeldItem, Count: 64}
	}
	equipPkt := proto.WriteEntityEquipment(who.EID, 0, mainHandItem)
	if err := to.SendPacket(equipPkt); err != nil {
		slog.Warn("send entity_equipment failed", "from", who.EID, "to", to.EID, "err", err)
		return err
	}

	// 5) entity_head_rotation (head-only channel, seeded with current yaw)
	headYaw := float32(whoYaw) * 256.0 / 360.0
	headPkt := proto.WriteEntityHeadRotation(who.EID, int8(headYaw))
	if err := to.SendPacket(headPkt); err != nil {
		slog.Warn("send entity_head_rotation failed", "from", who.EID, "to", to.EID, "err", err)
		return err
	}

	return nil
}

// MakeInvisible sends the 2-packet despawn sequence. remove_entities FIRST, then player_remove (otherwise a one-frame flicker where the player exists as a nameless entity).
func MakeInvisible(who, to *player.Player) error {
	proto := to.Proto
	rmEntityPkt := proto.WriteRemoveEntities([]int32{who.EID})
	if err := to.SendPacket(rmEntityPkt); err != nil {
		slog.Warn("send remove_entities failed", "from", who.EID, "to", to.EID, "err", err)
		return err
	}
	rmInfoPkt := proto.WritePlayerRemove([][16]byte{who.UUID})
	if err := to.SendPacket(rmInfoPkt); err != nil {
		slog.Warn("send player_remove failed", "from", who.EID, "to", to.EID, "err", err)
		return err
	}
	return nil
}
