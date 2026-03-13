package repository

import "context"

// FactionRepo defines the data access contract for player faction ownership.
type FactionRepo interface {
	AddPlayerFaction(ctx context.Context, playerID, faction, source string) error
	GetPlayerFactions(ctx context.Context, playerID string) ([]string, error)
}
