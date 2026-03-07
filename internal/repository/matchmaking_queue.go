package repository

import (
	"slices"
	"sync"
	"time"

	"github.com/kenyamaneko/overload-party-gateway/internal/model"
)

// MatchmakingQueue is an in-memory matchmaking queue.
type MatchmakingQueue struct {
	mu      sync.Mutex
	entries map[string]*model.QueueEntry // playerID → entry
}

func NewMatchmakingQueue() *MatchmakingQueue {
	return &MatchmakingQueue{
		entries: make(map[string]*model.QueueEntry),
	}
}

// Join adds a player to the queue. Idempotent: if already queued, updates the deck.
func (q *MatchmakingQueue) Join(playerID string, deckID int64) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if existing, ok := q.entries[playerID]; ok {
		existing.DeckID = deckID
		return nil
	}

	q.entries[playerID] = &model.QueueEntry{
		PlayerID: playerID,
		DeckID:   deckID,
		JoinedAt: time.Now(),
	}
	return nil
}

// Leave removes a player from the queue.
func (q *MatchmakingQueue) Leave(playerID string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	delete(q.entries, playerID)
}

// GetWaiting returns a snapshot of all waiting players sorted by join time.
func (q *MatchmakingQueue) GetWaiting() []model.QueueEntry {
	q.mu.Lock()
	defer q.mu.Unlock()

	entries := make([]model.QueueEntry, 0, len(q.entries))
	for _, e := range q.entries {
		entries = append(entries, *e)
	}

	slices.SortFunc(entries, func(a, b model.QueueEntry) int {
		return a.JoinedAt.Compare(b.JoinedAt)
	})

	return entries
}

// Remove removes specific players from the queue (used after matching).
func (q *MatchmakingQueue) Remove(playerIDs ...string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, id := range playerIDs {
		delete(q.entries, id)
	}
}

// Count returns the number of players in the queue.
func (q *MatchmakingQueue) Count() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.entries)
}

// IsQueued checks if a player is in the queue.
func (q *MatchmakingQueue) IsQueued(playerID string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	_, exists := q.entries[playerID]
	return exists
}
