package v772

import "goore/internal/protocol"

// Registry entry describes a single entry in a registry codec.
type registryEntry struct {
	ID      string
	HasData bool
	Data    []byte // NBT data (nil for no data)
}

func (e registryEntry) write(w *protocol.WireWriter) {
	w.String(e.ID)
	w.Bool(e.HasData)
	if e.HasData {
		w.RawWrite(e.Data)
	}
}

// configRegistries returns a slice of encoded registry_data packets.
// Each packet wraps a single registry with its entries.
// Entries have hasData=false so the client uses built-in defaults.
func configRegistries() [][]byte {
	var packets [][]byte

	registries := []struct {
		name    string
		entries []registryEntry
	}{
		{
			"minecraft:cat_variant",
			regEntry("tabby"),
		},
		{
			"minecraft:chicken_variant",
			regEntry("temperate"),
		},
		{
			"minecraft:cow_variant",
			regEntry("temperate"),
		},
		{
			"minecraft:frog_variant",
			regEntry("temperate"),
		},
		{
			"minecraft:painting_variant",
			regEntry("kebab"),
		},
		{
			"minecraft:pig_variant",
			regEntry("temperate"),
		},
		{
			"minecraft:wolf_sound_variant",
			regEntry("classic"),
		},
		{
			"minecraft:wolf_variant",
			regEntry("pale"),
		},
		{
			"minecraft:damage_type",
			damageTypes(),
		},
		{
			"minecraft:worldgen/biome",
			biomes(),
		},
		{
			"minecraft:dimension_type",
			regEntry("minecraft:overworld"),
		},
		// Additional required registries for 1.21.8
		{
			"minecraft:chat_type",
			regEntry("minecraft:chat"),
		},
		{
			"minecraft:trim_pattern",
			trimPatterns(),
		},
		{
			"minecraft:trim_material",
			trimMaterials(),
		},
	}

	for _, reg := range registries {
		var w protocol.WireWriter
		w.String(reg.name)
		w.VarInt(int32(len(reg.entries)))
		for _, e := range reg.entries {
			e.write(&w)
		}
		if w.Err() != nil {
			continue
		}
		packets = append(packets, protocol.MakePacket(ConfigRegistryData, w.Bytes()))
	}

	return packets
}

func regEntry(id string) []registryEntry {
	return []registryEntry{{ID: id, HasData: false}}
}

func regEntries(ids ...string) []registryEntry {
	entries := make([]registryEntry, len(ids))
	for i, id := range ids {
		entries[i] = registryEntry{ID: id, HasData: false}
	}
	return entries
}

func biomes() []registryEntry {
	return regEntries(
		"minecraft:plains",
		"minecraft:mangrove_swamp",
		"minecraft:desert",
		"minecraft:snowy_plains",
		"minecraft:beach",
	)
}

func damageTypes() []registryEntry {
	return regEntries(
		"minecraft:in_fire",
		"minecraft:campfire",
		"minecraft:lightning_bolt",
		"minecraft:on_fire",
		"minecraft:lava",
		"minecraft:hot_floor",
		"minecraft:in_wall",
		"minecraft:cramming",
		"minecraft:drown",
		"minecraft:starve",
		"minecraft:cactus",
		"minecraft:fall",
		"minecraft:ender_pearl",
		"minecraft:fly_into_wall",
		"minecraft:out_of_world",
		"minecraft:generic",
		"minecraft:magic",
		"minecraft:wither",
		"minecraft:dragon_breath",
		"minecraft:dry_out",
		"minecraft:sweet_berry_bush",
		"minecraft:freeze",
		"minecraft:stalagmite",
		"minecraft:falling_block",
		"minecraft:falling_anvil",
		"minecraft:falling_stalactite",
		"minecraft:sting",
		"minecraft:mob_attack",
		"minecraft:mob_attack_no_aggro",
		"minecraft:player_attack",
		"minecraft:arrow",
		"minecraft:trident",
		"minecraft:mob_projectile",
		"minecraft:spit",
		"minecraft:wind_charge",
		"minecraft:fireworks",
		"minecraft:fireball",
		"minecraft:unattributed_fireball",
		"minecraft:wither_skull",
		"minecraft:thrown",
		"minecraft:indirect_magic",
		"minecraft:thorns",
		"minecraft:explosion",
		"minecraft:player_explosion",
		"minecraft:sonic_boom",
		"minecraft:bad_respawn_point",
		"minecraft:outside_border",
		"minecraft:generic_kill",
		"minecraft:mace_smash",
	)
}

func trimPatterns() []registryEntry {
	return regEntries(
		"minecraft:sentry",
		"minecraft:dune",
		"minecraft:coast",
		"minecraft:wild",
		"minecraft:ward",
		"minecraft:eye",
		"minecraft:vex",
		"minecraft:tide",
		"minecraft:snout",
		"minecraft:rib",
		"minecraft:spire",
		"minecraft:wayfinder",
		"minecraft:shaper",
		"minecraft:silence",
		"minecraft:raiser",
		"minecraft:host",
		"minecraft:flow",
		"minecraft:bolt",
	)
}

func trimMaterials() []registryEntry {
	return regEntries(
		"minecraft:quartz",
		"minecraft:iron",
		"minecraft:gold",
		"minecraft:diamond",
		"minecraft:netherite",
		"minecraft:redstone",
		"minecraft:copper",
		"minecraft:emerald",
		"minecraft:lapis",
		"minecraft:amethyst",
		"minecraft:resin",
	)
}
