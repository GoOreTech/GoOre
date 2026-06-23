package world_test

import (
	"testing"

	"goore/internal/world"
)

func TestFoodForItem(t *testing.T) {
	cases := []struct {
		id       int32
		wantFood int32
		wantSat  float32
		wantOk   bool
	}{
		{857, 4, 19.2, true},   // apple
		{912, 5, 60.0, true},   // bread
		{939, 8, 204.8, true},  // cooked_porkchop
		{1308, 6, 14.4, true},  // honey_bottle
		{1, 0, 0, false},       // stone — not edible
		{0, 0, 0, false},       // air
	}
	for _, c := range cases {
		f, ok := world.FoodForItem(c.id)
		if ok != c.wantOk {
			t.Errorf("item %d: ok = %v, want %v", c.id, ok, c.wantOk)
			continue
		}
		if !c.wantOk {
			continue
		}
		if f.FoodPoints != c.wantFood {
			t.Errorf("item %d: foodPoints = %d, want %d", c.id, f.FoodPoints, c.wantFood)
		}
		if f.Saturation != c.wantSat {
			t.Errorf("item %d: saturation = %v, want %v", c.id, f.Saturation, c.wantSat)
		}
	}
}

// TestFoodTableHasExpectedCount is a sanity guard against an accidental
// truncation of the table during edits. minecraft-data 1.21.8 ships 40 foods.
func TestFoodTableHasExpectedCount(t *testing.T) {
	if got := len(world.FoodTable); got != 40 {
		t.Errorf("FoodTable has %d entries, want 40", got)
	}
}

// TestFoodTableValuesInBounds guards against typos that would make a food
// absurdly strong or weak (saturation must be > 0; food points 1..12).
func TestFoodTableValuesInBounds(t *testing.T) {
	for id, f := range world.FoodTable {
		if f.FoodPoints < 1 || f.FoodPoints > 12 {
			t.Errorf("item %d: foodPoints %d out of bounds [1,12]", id, f.FoodPoints)
		}
		if f.Saturation <= 0 {
			t.Errorf("item %d: saturation %v must be > 0", id, f.Saturation)
		}
	}
}
