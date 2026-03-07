package service

import (
	"context"
	"log"
	"time"

	"github.com/kenyamaneko/overload-party-gateway/internal/model"
	"github.com/kenyamaneko/overload-party-gateway/internal/repository"
)

// MatchHandler is called when a match is found.
type MatchHandler func(ctx context.Context, result model.MatchResult)

// MatchmakingService runs a periodic loop to find player pairs using FIFO ordering.
type MatchmakingService struct {
	queue    *repository.MatchmakingQueue
	handler  MatchHandler
	interval time.Duration
}

func NewMatchmakingService(queue *repository.MatchmakingQueue, handler MatchHandler) *MatchmakingService {
	return &MatchmakingService{
		queue:    queue,
		handler:  handler,
		interval: 1 * time.Second,
	}
}

// Run starts the matching loop. Blocks until context is cancelled.
func (s *MatchmakingService) Run(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.matchPlayers(ctx)
		}
	}
}

func (s *MatchmakingService) matchPlayers(ctx context.Context) {
	waiting := s.queue.GetWaiting()
	if len(waiting) < 2 {
		return
	}

	for i := 0; i+1 < len(waiting); i += 2 {
		p1 := waiting[i]
		p2 := waiting[i+1]

		s.queue.Remove(p1.PlayerID, p2.PlayerID)

		result := model.MatchResult{
			Player1ID:   p1.PlayerID,
			Player1Deck: p1.DeckID,
			Player2ID:   p2.PlayerID,
			Player2Deck: p2.DeckID,
		}

		go func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("match handler panic: %v", r)
				}
			}()
			s.handler(ctx, result)
		}()
	}
}
