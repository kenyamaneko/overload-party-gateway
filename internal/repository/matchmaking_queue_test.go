package repository

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMatchmakingQueue_JoinAndCount(t *testing.T) {
	q := NewMatchmakingQueue()

	require.NoError(t, q.Join("p1", 10))
	require.NoError(t, q.Join("p2", 20))

	assert.Equal(t, 2, q.Count())
}

func TestMatchmakingQueue_JoinIdempotent(t *testing.T) {
	q := NewMatchmakingQueue()

	require.NoError(t, q.Join("p1", 10))
	require.NoError(t, q.Join("p1", 99))

	assert.Equal(t, 1, q.Count())

	entries := q.GetWaiting()
	require.Len(t, entries, 1)
	assert.Equal(t, int64(99), entries[0].DeckID)
}

func TestMatchmakingQueue_Leave(t *testing.T) {
	q := NewMatchmakingQueue()

	require.NoError(t, q.Join("p1", 10))
	q.Leave("p1")

	assert.Equal(t, 0, q.Count())
	assert.False(t, q.IsQueued("p1"))
}

func TestMatchmakingQueue_LeaveNonexistent(t *testing.T) {
	q := NewMatchmakingQueue()

	// Should not panic.
	q.Leave("nobody")

	assert.Equal(t, 0, q.Count())
}

func TestMatchmakingQueue_GetWaiting_Sorted(t *testing.T) {
	q := NewMatchmakingQueue()

	require.NoError(t, q.Join("p1", 10))
	time.Sleep(5 * time.Millisecond)
	require.NoError(t, q.Join("p2", 20))
	time.Sleep(5 * time.Millisecond)
	require.NoError(t, q.Join("p3", 30))

	entries := q.GetWaiting()
	require.Len(t, entries, 3)

	assert.Equal(t, "p1", entries[0].PlayerID)
	assert.Equal(t, "p2", entries[1].PlayerID)
	assert.Equal(t, "p3", entries[2].PlayerID)

	assert.True(t, entries[0].JoinedAt.Before(entries[1].JoinedAt))
	assert.True(t, entries[1].JoinedAt.Before(entries[2].JoinedAt))
}

func TestMatchmakingQueue_Remove(t *testing.T) {
	q := NewMatchmakingQueue()

	require.NoError(t, q.Join("p1", 10))
	require.NoError(t, q.Join("p2", 20))
	require.NoError(t, q.Join("p3", 30))

	q.Remove("p1", "p3")

	assert.Equal(t, 1, q.Count())
	assert.False(t, q.IsQueued("p1"))
	assert.True(t, q.IsQueued("p2"))
	assert.False(t, q.IsQueued("p3"))
}

func TestMatchmakingQueue_IsQueued(t *testing.T) {
	q := NewMatchmakingQueue()

	require.NoError(t, q.Join("p1", 10))
	assert.True(t, q.IsQueued("p1"))

	q.Leave("p1")
	assert.False(t, q.IsQueued("p1"))
}

func TestMatchmakingQueue_EmptyQueue(t *testing.T) {
	q := NewMatchmakingQueue()

	entries := q.GetWaiting()
	assert.Empty(t, entries)
	assert.Equal(t, 0, q.Count())
}
