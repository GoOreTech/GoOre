// Package world — food item nutrition table. Maps an item registry id (the same
// id stored in Player.Hotbar / used as Holder<Item> wire value - 1) to the
// food points and saturation restored when eaten.
//
// Data source: minecraft-data 1.21.8 foods.json. The `saturation` field in
// foods.json is already the absolute saturation to ADD (foodPoints *
// saturationModifier * 2), so it is stored here verbatim — no runtime math
// needed. The `foodPoints` field maps directly to the hunger shanks restored.
//
// Only items that are edible as-is are listed. Stews/soups return a container
// (bowl) on consumption; GoOre does not model container-return yet, so eating
// a stew simply decrements the stack (the bowl is lost). This matches the
// "full food mechanics" scope for Phase 5 — container return is Phase 7.
package world

// FoodInfo is the nutrition profile of a single edible item.
type FoodInfo struct {
	FoodPoints int32   // shanks restored (1 shank = 0.5 drumstick; player food max = 20)
	Saturation float32 // absolute saturation added on eat
}

// FoodTable maps item registry id → FoodInfo. Items not present are not edible.
var FoodTable = map[int32]FoodInfo{
	857:  {4, 19.2},          // apple
	906:  {6, 86.4},          // mushroom_stew
	912:  {5, 60.0},          // bread
	938:  {3, 10.8},          // porkchop (raw)
	939:  {8, 204.8},         // cooked_porkchop
	941:  {4, 76.8},          // golden_apple
	942:  {4, 76.8},          // enchanted_golden_apple
	1012: {2, 1.6},           // cod (raw)
	1013: {2, 1.6},           // salmon (raw)
	1014: {1, 0.4},           // tropical_fish
	1015: {1, 0.4},           // pufferfish
	1016: {5, 60.0},          // cooked_cod
	1017: {6, 115.2},         // cooked_salmon
	1057: {2, 1.6},           // cookie
	1061: {2, 4.8},           // melon_slice
	1062: {1, 1.2},           // dried_kelp
	1065: {3, 10.8},          // beef (raw)
	1066: {8, 204.8},         // cooked_beef
	1067: {2, 4.8},           // chicken (raw)
	1068: {6, 86.4},          // cooked_chicken
	1069: {4, 6.4},           // rotten_flesh
	1077: {2, 12.8},          // spider_eye
	1177: {3, 21.6},          // carrot
	1178: {1, 1.2},           // potato
	1179: {5, 60.0},          // baked_potato
	1180: {2, 4.8},           // poisonous_potato
	1182: {6, 172.8},         // golden_carrot
	1191: {8, 76.8},          // pumpkin_pie
	1199: {3, 10.8},          // rabbit (raw)
	1200: {5, 60.0},          // cooked_rabbit
	1201: {10, 240.0},        // rabbit_stew
	1212: {2, 4.8},           // mutton (raw)
	1213: {6, 115.2},         // cooked_mutton
	1231: {4, 19.2},          // chorus_fruit
	1235: {1, 2.4},           // beetroot
	1237: {6, 86.4},          // beetroot_soup
	1275: {6, 86.4},           // suspicious_stew
	1300: {2, 1.6},           // sweet_berries
	1301: {2, 1.6},           // glow_berries
	1308: {6, 14.4},          // honey_bottle
}

// FoodForItem returns the FoodInfo for an item registry id and whether the item
// is edible. Non-edible items (tools, blocks, etc.) return ok=false.
func FoodForItem(itemID int32) (FoodInfo, bool) {
	f, ok := FoodTable[itemID]
	return f, ok
}
