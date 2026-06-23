// Block placement domain logic. The "what happens when a player right-clicks
// with a placeable item" decision is decoupled from the wire-protocol decode
// in player.handleBlockPlace. Vanilla 1.21.8 face enum: 0=-Y, 1=+Y, 2=-Z, 3=+Z,
// 4=-X, 5=+X. See docs/CONTEXT.md §Block Placement.
package world

import "goore/internal/protocol"

// Face direction enum. Matches vanilla 1.21.8 block_place.face field byte-for-byte. Do NOT renumber — the wire format is the contract.
const (
	FaceDown  int32 = 0
	FaceUp    int32 = 1
	FaceNorth int32 = 2
	FaceSouth int32 = 3
	FaceWest  int32 = 4
	FaceEast  int32 = 5
)

// TargetForFace returns the block position one step in the direction of `face` from `clicked`.
// Out-of-range face values (outside 0..5) return `clicked` unchanged (no-op; TryPlace rejects with PlaceTargetOccupied).
func TargetForFace(clicked protocol.BlockPos, face int32) protocol.BlockPos {
	switch face {
	case FaceDown:
		clicked.Y--
	case FaceUp:
		clicked.Y++
	case FaceNorth:
		clicked.Z--
	case FaceSouth:
		clicked.Z++
	case FaceWest:
		clicked.X--
	case FaceEast:
		clicked.X++
	}
	return clicked
}

// PlaceRequest is the input to TryPlace. HeldItem is the raw registryId (e.g. stone=1), NOT the wire value — wire-decode applies -1 before calling TryPlace.
type PlaceRequest struct {
	Clicked  protocol.BlockPos
	Face     int32
	HeldItem int32
	PlayerX  float64
	PlayerY  float64
	PlayerZ  float64
}

// PlaceReason is the machine-readable explanation of why a placement was rejected. PlaceOK (zero value) = success.
type PlaceReason int

const (
	PlaceOK               PlaceReason = iota // success: Placed=true, StateID set
	PlaceNotPlaceable                        // HeldItem is not in ItemIDToBlockState
	PlaceIntersectsPlayer                    // target overlaps the player's AABB
	PlaceTargetOccupied                      // target is not air (replaceable blocks not modeled)
)

// PlaceResult is the output of TryPlace. Successful placement triggers block_update broadcast + hotbar refill (creative); rejected placement is silent (matches vanilla's no-op).
type PlaceResult struct {
	// Placed is true iff the world was mutated by this call.
	Placed bool
	// StateID is the block state that was placed (populated only
	// if Placed=true; matches HeldItem via ItemIDToBlockState).
	StateID Block
	// Target is the block coordinate of the (attempted) placement.
	// Populated for both success and failure so the caller can
	// log it consistently.
	Target protocol.BlockPos
	// Reason is PlaceOK on success, or the matching rejection
	// enum value on failure. PlaceOK is the zero value.
	Reason PlaceReason
}

// TryPlace attempts to place the block corresponding to req.HeldItem at the position adjacent to req.Clicked in req.Face direction. Mutates w iff placement succeeds. Rejection rules in order: not_placeable, intersects_player, target_occupied. See docs/CONTEXT.md §Block Placement.
func TryPlace(w *World, req PlaceRequest) PlaceResult {
	target := TargetForFace(req.Clicked, req.Face)

	stateID, ok := ItemIDToBlockState[req.HeldItem]
	if !ok {
		return PlaceResult{Target: target, Reason: PlaceNotPlaceable}
	}

	if IntersectsAABB(req.PlayerX, req.PlayerY, req.PlayerZ,
		float64(target.X), float64(target.Y), float64(target.Z)) {
		return PlaceResult{Target: target, Reason: PlaceIntersectsPlayer}
	}

	if w.GetBlock(int(target.X), int(target.Y), int(target.Z)) != BlockAir {
		return PlaceResult{Target: target, Reason: PlaceTargetOccupied}
	}

	w.SetBlock(int(target.X), int(target.Y), int(target.Z), stateID)
	return PlaceResult{
		Placed:  true,
		StateID: stateID,
		Target:  target,
		Reason:  PlaceOK,
	}
}

// IntersectsAABB returns true if a unit cube at integer coordinates (bx, by, bz) overlaps the player's AABB. Player is 0.6 wide (xz), 1.8 tall. Exposed as a general geometric primitive.
func IntersectsAABB(pX, pY, pZ, bx, by, bZ float64) bool {
	const halfWidth = 0.3
	const height = 1.8
	pMinX, pMaxX := pX-halfWidth, pX+halfWidth
	pMinY, pMaxY := pY, pY+height
	pMinZ, pMaxZ := pZ-halfWidth, pZ+halfWidth
	bMaxX := bx + 1
	bMaxY := by + 1
	bMaxZ := bZ + 1
	return pMinX < bMaxX && pMaxX > bx &&
		pMinY < bMaxY && pMaxY > by &&
		pMinZ < bMaxZ && pMaxZ > bZ
}
