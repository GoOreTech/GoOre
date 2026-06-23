// Package v772 implements the Minecraft 1.21.8 (protocol 772) protocol.
package v772

import "goore/internal/protocol"

// Protocol version constants.
const (
	Version     int32  = 772
	VersionName string = "1.21.8"
)

// DamageType* are damage_type registry ids. The registry is sent to the
// client during configuration in the order defined by damageTypes() in
// registries.go; these ids MUST stay in lockstep with that order. They are
// the sourceTypeId field of the Damage Event packet (0x19) and the index used
// to look up the death/localization messages client-side.
const (
	DamageTypeInFire        int32 = 0
	DamageTypeCampfire      int32 = 1
	DamageTypeLightning     int32 = 2
	DamageTypeOnFire        int32 = 3
	DamageTypeLava          int32 = 4
	DamageTypeHotFloor      int32 = 5
	DamageTypeInWall        int32 = 6
	DamageTypeCramming      int32 = 7
	DamageTypeDrown         int32 = 8
	DamageTypeStarve        int32 = 9
	DamageTypeCactus        int32 = 10
	DamageTypeFall          int32 = 11
	DamageTypeEnderPearl    int32 = 12
	DamageTypeFlyIntoWall   int32 = 13
	DamageTypeOutOfWorld    int32 = 14 // void
	DamageTypeGeneric       int32 = 15
	DamageTypeMagic         int32 = 16
	DamageTypeWither        int32 = 17
	DamageTypeDragonBreath  int32 = 18
	DamageTypeGenericKill   int32 = 46
)

// EntityStatus op codes (Entity Event packet 0x1E). Only the ones GoOre uses.
const (
	EntityStatusPlayDeathSound int8 = 3 // living entity: death sound + tilt-over death animation
)

// =============================================================================
// Handshake (serverbound)
// =============================================================================
const (
	HandshakeIntention int32 = 0x00 // set_protocol
)

const (
	HandshakeStateStatus = 1
	HandshakeStateLogin  = 2
)

// =============================================================================
// Status (clientbound)
// =============================================================================
const (
	StatusServerInfo int32 = 0x00 // server_info
	StatusPing       int32 = 0x01 // pong_response
)

// Status (serverbound) — same IDs, different direction
const (
	StatusRequest int32 = 0x00 // status_request
	StatusPong    int32 = 0x01 // ping_request
)

// =============================================================================
// Login
// =============================================================================
const (
	// Serverbound
	LoginStart        int32 = 0x00 // login_start
	LoginAcknowledged int32 = 0x03 // login_acknowledged

	// Clientbound
	LoginDisconnect    int32 = 0x00 // disconnect
	LoginSuccess       int32 = 0x02 // success
	LoginCompress      int32 = 0x03 // compress
	LoginPluginRequest int32 = 0x04 // login_plugin_request
	LoginCookieRequest int32 = 0x05 // cookie_request
)

// =============================================================================
// Configuration
// =============================================================================
const (
	// Serverbound
	ConfigSettings            int32 = 0x00 // settings (client_information in older versions)
	ConfigCookieResponse      int32 = 0x01 // cookie_response
	ConfigCustomPayloadServer int32 = 0x02 // custom_payload (plugin message) — serverbound
	ConfigFinishConfiguration int32 = 0x03 // finish_configuration
	ConfigKeepAlive           int32 = 0x04 // keep_alive (serverbound)
	ConfigPong                int32 = 0x05 // pong
	ConfigResourcePackReceive int32 = 0x06 // resource_pack_receive
	ConfigSelectKnownPacks    int32 = 0x07 // select_known_packs
	ConfigCustomClickAction   int32 = 0x08 // custom_click_action
	ConfigAcknowledged        int32 = 0x0F // configuration_acknowledged

	// Clientbound
	ConfigCookieRequest       int32 = 0x00 // cookie_request
	ConfigCustomPayloadClient int32 = 0x01 // custom_payload (plugin message) — clientbound
	ConfigDisconnect          int32 = 0x02 // disconnect
	ConfigFinishConfig        int32 = 0x03 // finish_configuration
	ConfigKeepAliveClient     int32 = 0x04 // keep_alive
	ConfigPing                int32 = 0x05 // ping
	ConfigResetChat           int32 = 0x06 // reset_chat
	ConfigRegistryData        int32 = 0x07 // registry_data
	ConfigRemoveResourcePack  int32 = 0x08 // remove_resource_pack
	ConfigAddResourcePack     int32 = 0x09 // add_resource_pack
	ConfigStoreCookie         int32 = 0x0A // store_cookie
	ConfigTransfer            int32 = 0x0B // transfer
	ConfigFeatureFlags        int32 = 0x0C // feature_flags
	ConfigTags                int32 = 0x0D // tags
	ConfigSelectKnownPacksCli int32 = 0x0E // select_known_packs
	ConfigCustomReportDetails int32 = 0x0F // custom_report_details
	ConfigServerLinks         int32 = 0x10 // server_links
)

// =============================================================================
// Play
// =============================================================================
const (
	// Serverbound
	PlayTeleportConfirm          int32 = 0x00 // teleport_confirm
	PlayQueryBlockNBT            int32 = 0x01 // query_block_nbt
	PlaySelectBundleItem         int32 = 0x02 // select_bundle_item
	PlaySetDifficulty            int32 = 0x03 // set_difficulty
	PlayChangeGamemode           int32 = 0x04 // change_gamemode
	PlayMessageAcknowledgement   int32 = 0x05 // message_acknowledgement
	PlayChatCommand              int32 = 0x06 // chat_command
	PlayChatCommandSigned        int32 = 0x07 // chat_command_signed
	PlayChatMessage              int32 = 0x08 // chat_message
	PlayChatSessionUpdate        int32 = 0x09 // chat_session_update
	PlayChunkBatchReceived       int32 = 0x0A // chunk_batch_received
	PlayClientCommand            int32 = 0x0B // client_command
	PlayTickEnd                  int32 = 0x0C // tick_end
	PlaySettings                 int32 = 0x0D // settings (client info in play)
	PlayTabComplete              int32 = 0x0E // tab_complete
	PlayConfigAcknowledged       int32 = 0x0F // configuration_acknowledged
	PlayClickContainer           int32 = 0x11 // click_container
	PlayCloseContainer           int32 = 0x12 // close_container
	PlayPluginMessage            int32 = 0x13 // custom_payload
	PlayPlayerCommand            int32 = 0x14 // player_command
	PlayPlayerInput              int32 = 0x15 // player_input
	PlayPong                     int32 = 0x16 // pong
	PlayRecipeBookChangeSettings int32 = 0x17 // recipe_book_change_settings
	PlaySetSlotState             int32 = 0x18 // set_slot_state
	PlayInteract                 int32 = 0x19 // interact
	PlayJigsawGenerate           int32 = 0x1A // jigsaw_generate
	PlayKeepAliveServerbound     int32 = 0x1B // keep_alive (serverbound)
	PlayDifficultyLock           int32 = 0x1C // difficulty_lock
	// 1.21.8 — IDs swapped vs 1.20.x: position=0x1D, position_look=0x1E.
	// Source: node_modules/minecraft-data/data/pc/1.21.8/protocol.json
	// toServer.types.packet mapper (authoritative).
	PlaySetPlayerPos            int32 = 0x1D // position: x,y,z,flags
	PlaySetPlayerPosRot         int32 = 0x1E // position_look: x,y,z,yaw,pitch,flags
	PlaySetPlayerRot            int32 = 0x1F // look: yaw,pitch,flags
	PlaySetPlayerMovementFlags  int32 = 0x20 // flying: flags only
	PlaySetPlayerOnGround       int32 = 0x21 // (not present in proto.yml — check!)
	PlayMoveVehicle             int32 = 0x21 // move_vehicle
	PlayEntityAction            int32 = 0x29 // entity_action: sprint/sneak/etc
	PlayPaddleBoat              int32 = 0x22 // paddle_boat
	PlayPickItemFromEntity      int32 = 0x24 // pick_item_from_entity
	PlaySwingArm                int32 = 0x27 // swing_arm
	PlayPlayerAction            int32 = 0x28 // block_dig (1.21.8: ServerboundPlayerActionPacket)
	PlayPlayerLoaded            int32 = 0x2B // player_loaded (1.21.8: ID 0x2B, NOT 0x39)
	PlaySetHeldItem             int32 = 0x34 // held_item_slot (1.21.8: ID 0x34, formerly known as "set_held_item" in 1.20.x)
	PlayCreativeInventoryAction int32 = 0x37 // set_creative_slot (1.21.8)
	PlayBlockPlace              int32 = 0x3F // block_place (1.21.8: was "use_item_on" in 1.20.x)
	PlayUseItem                 int32 = 0x40 // use_item (1.21.8: right-click on air)

	// Clientbound
	PlayBundleDelimiter      int32 = 0x00 // bundle_delimiter
	PlaySpawnEntity          int32 = 0x01 // spawn_entity
	PlayRemoveEntities       int32 = 0x46 // remove_entities
	PlayAnimation            int32 = 0x02 // animation
	PlayStatistics           int32 = 0x03 // statistics
	PlayAckPlayerDigging     int32 = 0x04 // acknowledge_player_digging
	PlayBlockBreakAnimation  int32 = 0x05 // block_break_animation
	PlayTileEntityData       int32 = 0x06 // tile_entity_data
	PlayBlockAction          int32 = 0x07 // block_action
	PlayBlockUpdate          int32 = 0x08 // block_change
	PlayBossBar              int32 = 0x09 // boss_bar
	PlayDifficulty           int32 = 0x0A // difficulty
	PlayChunkBatchFinished   int32 = 0x0B // chunk_batch_finished
	PlayChunkBatchStart      int32 = 0x0C // chunk_batch_start
	PlayChunkBiomes          int32 = 0x0D // chunk_biomes
	PlayClearTitles          int32 = 0x0E // clear_titles
	PlayTabCompleteResp      int32 = 0x0F // tab_complete
	PlayDeclareCommands      int32 = 0x10 // declare_commands
	PlayCloseWindow          int32 = 0x11 // close_window
	PlayWindowItems          int32 = 0x12 // window_items
	PlayCraftProgressBar     int32 = 0x13 // craft_progress_bar
	PlaySetSlot              int32 = 0x14 // set_slot
	PlaySetCooldown          int32 = 0x16 // set_cooldown
	PlayCustomPayload        int32 = 0x18 // custom_payload
	PlayDamageEvent          int32 = 0x19 // damage_event
	PlayHideMessage          int32 = 0x1B // hide_message
	PlayDisconnect           int32 = 0x1C // kick_disconnect
	PlayProfilelessChat      int32 = 0x1D // profileless_chat
	PlayEntityStatus         int32 = 0x1E // entity_status
	PlaySyncEntityPosition   int32 = 0x1F // sync_entity_position
	PlayExplosion            int32 = 0x20 // explosion
	PlayUnloadChunk          int32 = 0x21 // unload_chunk
	PlayGameStateChange      int32 = 0x22 // game_state_change
	PlayOpenHorseWindow      int32 = 0x23 // open_horse_window
	PlayHurtAnimation        int32 = 0x24 // hurt_animation
	PlayInitWorldBorder      int32 = 0x25 // initialize_world_border
	PlayKeepAlive            int32 = 0x26 // keep_alive
	PlayMapChunk             int32 = 0x27 // map_chunk
	PlayWorldEvent           int32 = 0x28 // world_event
	PlayWorldParticles       int32 = 0x29 // world_particles
	PlayUpdateLight          int32 = 0x2A // update_light
	PlayLogin                int32 = 0x2B // login (formerly JoinGame)
	PlayMap                  int32 = 0x2C // map
	PlayTradeList            int32 = 0x2D // trade_list
	PlayRelEntityMove        int32 = 0x2E // rel_entity_move
	PlayEntityMoveLook       int32 = 0x2F // entity_move_look
	PlayMoveMinecart         int32 = 0x30 // move_minecart
	PlayEntityLook           int32 = 0x31 // entity_look
	PlayVehicleMove          int32 = 0x32 // vehicle_move
	PlayOpenBook             int32 = 0x33 // open_book
	PlayOpenWindow           int32 = 0x34 // open_window
	PlayOpenSignEntity       int32 = 0x35 // open_sign_entity
	PlayPing                 int32 = 0x36 // ping
	PlayPingResponse         int32 = 0x37 // ping_response
	PlayCraftRecipeResponse  int32 = 0x38 // craft_recipe_response
	PlayAbilities            int32 = 0x39 // abilities
	PlayPlayerChat           int32 = 0x3A // player_chat
	PlayEndCombatEvent       int32 = 0x3B // end_combat_event
	PlayEnterCombatEvent     int32 = 0x3C // enter_combat_event
	PlayDeathCombatEvent     int32 = 0x3D // death_combat_event
	PlayPlayerRemove         int32 = 0x3E // player_remove
	PlayPlayerInfoUpdate     int32 = 0x3F // player_info
	PlayFacePlayer           int32 = 0x40 // face_player
	PlayPlayerPos            int32 = 0x41 // position
	PlayPlayerRot            int32 = 0x42 // player_rotation
	PlayRecipeBookAdd        int32 = 0x43 // recipe_book_add
	PlayRecipeBookRemove     int32 = 0x44 // recipe_book_remove
	PlayRecipeBookSettings   int32 = 0x45 // recipe_book_settings
	PlayEntityDestroy        int32 = 0x46 // entity_destroy
	PlayRemoveEntityEffect   int32 = 0x47 // remove_entity_effect
	PlayResetScore           int32 = 0x48 // reset_score
	PlayRemoveResourcePack   int32 = 0x49 // remove_resource_pack
	PlayAddResourcePack      int32 = 0x4A // add_resource_pack
	PlayRespawn              int32 = 0x4B // respawn
	PlayEntityHeadRotation   int32 = 0x4C // entity_head_rotation
	PlayMultiBlockChange     int32 = 0x4D // multi_block_change
	PlaySelectAdvancementTab int32 = 0x4E // select_advancement_tab
	PlayServerData           int32 = 0x4F // server_data
	PlayActionBar            int32 = 0x50 // action_bar
	PlayWorldBorderCenter    int32 = 0x51 // world_border_center
	PlayWorldBorderLerpSize  int32 = 0x52 // world_border_lerp_size
	PlayWorldBorderSize      int32 = 0x53 // world_border_size
	PlayWorldBorderWarnDelay int32 = 0x54 // world_border_warning_delay
	PlayWorldBorderWarnReach int32 = 0x55 // world_border_warning_reach
	PlayCamera               int32 = 0x56 // camera
	PlayViewPosition         int32 = 0x57 // update_view_position
	PlayViewDistance         int32 = 0x58 // update_view_distance
	PlaySetCursorItem        int32 = 0x59 // set_cursor_item
	PlaySpawnPos             int32 = 0x5A // spawn_position
	PlayScoreboardObjDisplay int32 = 0x5B // scoreboard_display_objective
	PlayEntityMetadata       int32 = 0x5C // entity_metadata
	PlayAttachEntity         int32 = 0x5D // attach_entity
	PlayEntityVelocity       int32 = 0x5E // entity_velocity
	PlayEntityEquipment      int32 = 0x5F // entity_equipment
	PlayExperience           int32 = 0x60 // experience
	PlayUpdateHealth         int32 = 0x61 // update_health
	PlayHeldItemSlot         int32 = 0x62 // held_item_slot
	PlayScoreboardObjective  int32 = 0x63 // scoreboard_objective
	PlaySetPassengers        int32 = 0x64 // set_passengers
	PlaySetPlayerInventory   int32 = 0x65 // set_player_inventory
	PlayTeams                int32 = 0x66 // teams
	PlayScoreboardScore      int32 = 0x67 // scoreboard_score
	PlaySimulationDistance   int32 = 0x68 // simulation_distance
	PlaySetTitleSubtitle     int32 = 0x69 // set_title_subtitle
	PlayUpdateTime           int32 = 0x6A // update_time
	PlaySetTitleText         int32 = 0x6B // set_title_text
	PlaySetTitleTime         int32 = 0x6C // set_title_time
	PlayEntitySoundEffect    int32 = 0x6D // entity_sound_effect
	PlaySoundEffect          int32 = 0x6E // sound_effect
	PlayStartConfiguration   int32 = 0x6F // start_configuration
	PlayStopSound            int32 = 0x70 // stop_sound
	PlayStoreCookie          int32 = 0x71 // store_cookie
	PlaySystemChat           int32 = 0x72 // system_chat
	PlayPlayerlistHeader     int32 = 0x73 // playerlist_header
	PlayNBTQueryResponse     int32 = 0x74 // nbt_query_response
	PlayCollect              int32 = 0x75 // collect
	PlayEntityTeleport       int32 = 0x76 // entity_teleport
	PlayTestInstanceBlock    int32 = 0x77 // test_instance_block_status
	PlaySetTickingState      int32 = 0x78 // set_ticking_state
	PlayStepTick             int32 = 0x79 // step_tick
	PlayTransfer             int32 = 0x7A // transfer
	PlayAdvancements         int32 = 0x7B // advancements
	PlayEntityUpdateAttrs    int32 = 0x7C // entity_update_attributes
	PlayEntityEffect         int32 = 0x7D // entity_effect
	PlayDeclareRecipes       int32 = 0x7E // declare_recipes
	PlayTags                 int32 = 0x7F // tags
	PlaySetProjectilePower   int32 = 0x80 // set_projectile_power
	PlayCustomReportDetails  int32 = 0x81 // custom_report_details
	PlayServerLinks          int32 = 0x82 // server_links
	PlayTrackedWaypoint      int32 = 0x83 // tracked_waypoint
	PlayClearDialog          int32 = 0x84 // clear_dialog
	PlayShowDialog           int32 = 0x85 // show_dialog
)

// packetIDs is the cached packet ID map for version 1.21.8. It is
// built once at package init (zero per-call map allocations) and
// returned by PacketIDs() as a value copy. The maps themselves
// are shared across all callers and must NOT be mutated by them —
// the convention is enforced by convention, not by the type
// system (Go map values are reference types).
//
// Phase 3.8 (REFACTORING_PLAN.md §3.8): the pre-Phase-3.8 PacketIDs()
// allocated two fresh map[string]int32 on every call. The function
// is on the per-packet hot path (handlePlay calls it for every
// inbound packet), so the allocation pressure was significant
// under load. Caching as a package var is safe because the contents
// never change after init.
var packetIDs = protocol.PacketIDMap{
	Clientbound: map[string]int32{
		"login_success":        LoginSuccess,
		"finish_configuration": ConfigFinishConfig,
		"registry_data":        ConfigRegistryData,
		"select_known_packs":   ConfigSelectKnownPacksCli,
		"feature_flags":        ConfigFeatureFlags,
		"keep_alive_config":    ConfigKeepAliveClient,
		"login_play":           PlayLogin,
		"keep_alive":           PlayKeepAlive,
		"map_chunk":            PlayMapChunk,
		"update_light":         PlayUpdateLight,
		"chunk_biomes":         PlayChunkBiomes,
		"block_update":         PlayBlockUpdate,
		"abilities":            PlayAbilities,
		"position":             PlayPlayerPos,
		"held_item_slot":       PlayHeldItemSlot,
		"spawn_position":       PlaySpawnPos,
		"update_health":        PlayUpdateHealth,
		"update_time":          PlayUpdateTime,
		"system_chat":          PlaySystemChat,
		"window_items":         PlayWindowItems,
		"set_slot":             PlaySetSlot,
		"view_position":        PlayViewPosition,
		"view_distance":        PlayViewDistance,
		"simulation_distance":  PlaySimulationDistance,
		"start_configuration":  PlayStartConfiguration,
		"disconnect":           PlayDisconnect,
		"player_info_update":   PlayPlayerInfoUpdate,
		"entity_destroy":       PlayEntityDestroy,
		"spawn_entity":         PlaySpawnEntity,
		"entity_teleport":      PlayEntityTeleport,
		"entity_head_rotation": PlayEntityHeadRotation,
		"entity_equipment":     PlayEntityEquipment,
		"entity_metadata":      PlayEntityMetadata,
		"entity_animation":     PlayAnimation,
		"damage_event":         PlayDamageEvent,
		"respawn":              PlayRespawn,
	},
	Serverbound: map[string]int32{
		"handshake":                        HandshakeIntention,
		"login_start":                      LoginStart,
		"login_acknowledged":               LoginAcknowledged,
		"config_settings":                  ConfigSettings,
		"config_finish":                    ConfigFinishConfiguration,
		"config_acknowledged":              ConfigAcknowledged,
		"config_keep_alive":                ConfigKeepAlive,
		"teleport_confirm":                 PlayTeleportConfirm,
		"set_player_position":              PlaySetPlayerPos,           // 0x1D in 1.21.8 (was 0x1E in 1.20.x)
		"set_player_position_and_rotation": PlaySetPlayerPosRot,        // 0x1E in 1.21.8 (was 0x1D in 1.20.x)
		"set_player_rotation":              PlaySetPlayerRot,           // 0x1F
		"set_player_movement_flags":        PlaySetPlayerMovementFlags, // 0x20
		"flying":                           PlaySetPlayerMovementFlags, // 1.21.8 name
		"player_position":                  PlaySetPlayerPos,           // 1.21.8 packet name
		"position":                         PlaySetPlayerPos,           // 1.21.8 packet name
		"position_look":                    PlaySetPlayerPosRot,        // 1.21.8 packet name
		"look":                             PlaySetPlayerRot,           // 1.21.8 packet name
		"entity_action":                    PlayEntityAction,           // 0x29 (sprint/sneak/etc)
		"player_input":                     PlayPlayerInput,            // 0x2A (WASD)
		"tick_end":                         PlayTickEnd,                // 0x0C (empty body)
		"player_action":                    PlayPlayerAction,
		"block_dig":                        PlayPlayerAction, // 1.21.8 name (0x28)
		"block_place":                      PlayBlockPlace,   // 1.21.8 name (0x3F, was "use_item_on")
		"use_item":                         PlayUseItem,      // 1.21.8 name (0x40, right-click on air)
		"use_item_on":                      PlayBlockPlace,   // legacy alias (1.20.x name for 0x3F)
		"swing_arm":                        PlaySwingArm,
		"arm_animation":                    PlaySwingArm,    // 1.21.8 alias
		"held_item_slot":                   PlaySetHeldItem, // 1.21.8 name (0x34)
		"set_held_item":                    PlaySetHeldItem, // legacy alias (1.20.x)
		"set_creative_slot":                PlayCreativeInventoryAction,
		"set_creative_mode_slot":           PlayCreativeInventoryAction,
		"pick_item_from_entity":            PlayPickItemFromEntity,
		"pick_item_from_block":             PlayPickItemFromEntity, // 1.21.8 (0x23): alias used internally
		"keep_alive":                       PlayKeepAliveServerbound,
		"chat_command":                     PlayChatCommand,
		"client_command":                   PlayClientCommand,
		"interact":                         PlayInteract,
		"creative_inventory_action":        PlayCreativeInventoryAction,
		"player_loaded":                    PlayPlayerLoaded,
		"click_container":                  PlayClickContainer,
		"close_container":                  PlayCloseContainer,
		"acknowledge_block_change":         PlayPickItemFromEntity, // not a real serverbound packet in 1.21.8 (the ack is clientbound 0x04)
		"chunk_batch_received":             PlayChunkBatchReceived,
	},
}

// PacketIDs returns the cached packet ID map for version 1.21.8.
// The returned struct shares the underlying maps with all other
// callers — DO NOT mutate them. See the package var above for the
// rationale and the Phase 3.8 perf comment.
func PacketIDs() protocol.PacketIDMap {
	return packetIDs
}
