package player_test

import (
	"testing"

	"goore/internal/player"
	"goore/internal/world"
)

func TestFallDamageFor(t *testing.T) {
	cases := []struct {
		fall float32
		want float32
	}{
		{0, 0},
		{1, 0},
		{3, 0},      // 3 blocks free
		{4, 1},      // ceil(4-3)=1
		{5, 2},      // ceil(5-3)=2
		{4.5, 2},    // ceil(1.5)=2
		{10.9, 8},   // ceil(7.9)=8
		{23, 20},    // lethal
	}
	for _, c := range cases {
		if got := player.FallDamageFor(c.fall); got != c.want {
			t.Errorf("fallDamageFor(%v) = %v, want %v", c.fall, got, c.want)
		}
	}
}

// TestTickFoodState_ExhaustionDrainSaturation: exhaustion ≥ 4 drains saturation first.
func TestTickFoodState_ExhaustionDrainSaturation(t *testing.T) {
	v := player.Vitals{Health: 20, Food: 20, Saturation: 5, Exhaustion: 4}
	heal, starve := player.TickFoodState(&v, true, 20)
	if heal != 0 || starve != 0 {
		t.Errorf("no heal/starve expected, got heal=%v starve=%v", heal, starve)
	}
	if v.Exhaustion != 0 {
		t.Errorf("exhaustion = %v, want 0 (drained 4)", v.Exhaustion)
	}
	if v.Saturation != 4 {
		t.Errorf("saturation = %v, want 4 (drained by 1)", v.Saturation)
	}
	if v.Food != 20 {
		t.Errorf("food = %v, want 20 (untouched while saturation>0)", v.Food)
	}
}

// TestTickFoodState_ExhaustionDrainsFoodWhenNoSaturation.
func TestTickFoodState_ExhaustionDrainsFood(t *testing.T) {
	v := player.Vitals{Health: 20, Food: 10, Saturation: 0, Exhaustion: 4}
	player.TickFoodState(&v, true, 20)
	if v.Food != 9 {
		t.Errorf("food = %v, want 9", v.Food)
	}
	if v.Saturation != 0 {
		t.Errorf("saturation = %v, want 0", v.Saturation)
	}
}

// TestTickFoodState_Regen: at food≥18 + health<20, every 80 ticks heal 1 and add 3 exhaustion.
func TestTickFoodState_Regen(t *testing.T) {
	v := player.Vitals{Health: 19, Food: 20, Saturation: 5, Exhaustion: 0}
	var totalHeal float32
	for i := 0; i < 80; i++ {
		heal, _ := player.TickFoodState(&v, true, 19+totalHeal)
		totalHeal += heal
	}
	if totalHeal != 1 {
		t.Errorf("after 80 ticks heal = %v, want 1", totalHeal)
	}
	// regen cost: 3 exhaustion added on the healing tick
	if v.Exhaustion != 3 {
		t.Errorf("exhaustion = %v, want 3 (regen cost)", v.Exhaustion)
	}
	if v.FoodTick != 0 {
		t.Errorf("foodTick = %v, want 0 (reset after heal)", v.FoodTick)
	}
}

// TestTickFoodState_RegenDisabledForCreative: regenEnabled=false suppresses regen.
func TestTickFoodState_RegenDisabledForCreative(t *testing.T) {
	v := player.Vitals{Health: 19, Food: 20, Saturation: 5, Exhaustion: 0}
	var totalHeal float32
	for i := 0; i < 160; i++ {
		heal, _ := player.TickFoodState(&v, false, 19)
		totalHeal += heal
	}
	if totalHeal != 0 {
		t.Errorf("creative regen heal = %v, want 0", totalHeal)
	}
}

// TestTickFoodState_Starvation: at food=0, every 80 ticks deal 1 damage (can kill).
func TestTickFoodState_Starvation(t *testing.T) {
	v := player.Vitals{Health: 5, Food: 0, Saturation: 0, Exhaustion: 0}
	var totalStarve float32
	for i := 0; i < 160; i++ {
		_, starve := player.TickFoodState(&v, true, 5-totalStarve)
		totalStarve += starve
	}
	if totalStarve != 2 {
		t.Errorf("after 160 ticks starve damage = %v, want 2", totalStarve)
	}
}

// TestTickFoodState_NoRegenAtFullHealth: food≥18 but health=20 → no heal, foodTick resets.
func TestTickFoodState_NoRegenAtFullHealth(t *testing.T) {
	v := player.Vitals{Health: 20, Food: 20, Saturation: 5, Exhaustion: 0}
	heal, starve := player.TickFoodState(&v, true, 20)
	if heal != 0 || starve != 0 {
		t.Errorf("no effects expected, got heal=%v starve=%v", heal, starve)
	}
	if v.FoodTick != 0 {
		t.Errorf("foodTick = %v, want 0 (default branch reset)", v.FoodTick)
	}
}

// TestApplyEat_Clamps verifies Food clamps at 20 and Saturation clamps at the new Food.
func TestApplyEat_Clamps(t *testing.T) {
	v := player.Vitals{Food: 18, Saturation: 0}
	// cooked_beef: 8 food, 204.8 saturation — saturation must clamp to Food (20).
	player.ApplyEat(&v, world.FoodInfo{FoodPoints: 8, Saturation: 204.8})
	if v.Food != 20 {
		t.Errorf("food = %v, want 20 (clamped)", v.Food)
	}
	if v.Saturation != 20 {
		t.Errorf("saturation = %v, want 20 (clamped to food)", v.Saturation)
	}
}

// TestApplyEat_PartialFill: apple (4 food, 19.2 sat) on a fresh player keeps saturation within food.
func TestApplyEat_PartialFill(t *testing.T) {
	v := player.Vitals{Food: 0, Saturation: 0}
	player.ApplyEat(&v, world.FoodInfo{FoodPoints: 4, Saturation: 19.2})
	if v.Food != 4 {
		t.Errorf("food = %v, want 4", v.Food)
	}
	if v.Saturation != 4 {
		t.Errorf("saturation = %v, want 4 (clamped to food=4)", v.Saturation)
	}
}
