package model

import "time"

// QueueEntry represents a player waiting in the matchmaking queue.
type QueueEntry struct {
	PlayerID string
	DeckID   int64
	JoinedAt time.Time
}

// MatchResult represents a successful match between two players.
type MatchResult struct {
	Player1ID   string
	Player1Deck int64
	Player2ID   string
	Player2Deck int64
}
