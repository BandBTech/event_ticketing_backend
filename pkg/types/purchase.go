package types

import "github.com/google/uuid"

type TierSelection struct {
	TierID   uuid.UUID `json:"tier_id"`
	Quantity int       `json:"quantity"`
}
