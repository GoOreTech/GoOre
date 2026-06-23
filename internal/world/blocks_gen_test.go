package world

import "testing"

// TestBlocksGenSanity verifies invariants on the generated blocks_gen.go.
//
// If the generator changes, this test forces a thought-through review of
// the change. The actual data (entry count, key set) is verified separately
// by the generator's own test (in cmd/genblocks).
func TestBlocksGenSanity(t *testing.T) {
	// Generated map must be non-empty.
	if len(DefaultStateByName) == 0 {
		t.Fatal("DefaultStateByName is empty (generator not run?)")
	}

	// 1.21.8 has ~1105 base block types (more if counting individual states).
	if got, min := len(DefaultStateByName), 1000; got < min {
		t.Errorf("DefaultStateByName has %d entries, want >= %d", got, min)
	}

	// Invariant: all values must be valid block state IDs.
	// 1.21.8 has < 30000 total block states.
	for name, stateID := range DefaultStateByName {
		if stateID > 30000 {
			t.Errorf("block %q has implausible state ID %d", name, stateID)
		}
	}

	// Invariant: well-known block names must map to expected default states
	// matching the hand-verified constants in chunk.go.
	checks := map[string]Block{
		"air":         BlockAir,
		"stone":       BlockStone,
		"grass_block": BlockGrass,
		"dirt":        BlockDirt,
		"bedrock":     BlockBedrock,
	}
	for name, want := range checks {
		got, ok := DefaultStateByName[name]
		if !ok {
			t.Errorf("DefaultStateByName missing %q", name)
			continue
		}
		if got != want {
			t.Errorf("DefaultStateByName[%q] = %d, want %d", name, got, want)
		}
	}
}

// TestItemsGenSanity verifies the generated item→block mapping.
func TestItemsGenSanity(t *testing.T) {
	if len(ItemIDToBlockState) == 0 {
		t.Fatal("ItemIDToBlockState is empty (generator not run?)")
	}

	// 1.21.8 has ~600 placeable items.
	if got, min := len(ItemIDToBlockState), 400; got < min {
		t.Errorf("ItemIDToBlockState has %d entries, want >= %d", got, min)
	}

	// Well-known item IDs (from items.json) and their expected block default states.
	checks := []struct {
		itemID    int32
		wantState Block
		name      string
	}{
		{1, BlockStone, "stone"},
		{27, BlockGrass, "grass_block"},
		{28, BlockDirt, "dirt"},
		{35, 14, "cobblestone"}, // defaultState=14
		{58, BlockBedrock, "bedrock"},
	}
	for _, c := range checks {
		got, ok := ItemIDToBlockState[c.itemID]
		if !ok {
			t.Errorf("ItemIDToBlockState missing itemID=%d (%s)", c.itemID, c.name)
			continue
		}
		if got != c.wantState {
			t.Errorf("ItemIDToBlockState[%d (%s)] = %d, want %d", c.itemID, c.name, got, c.wantState)
		}
	}

	// Every value must be a valid block state ID.
	for itemID, stateID := range ItemIDToBlockState {
		if stateID > 30000 {
			t.Errorf("itemID=%d has implausible state ID %d", itemID, stateID)
		}
	}
}
