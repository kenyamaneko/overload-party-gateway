package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/kenyamaneko/overload-party-gateway/internal/cache"
	"github.com/kenyamaneko/overload-party-common/model"
	"github.com/kenyamaneko/overload-party-gateway/internal/repository"
)

// GameLogService provides human-readable game logs from stored events.
type GameLogService struct {
	gameRepo  repository.GameRepository
	cardCache *cache.CardCache
}

func NewGameLogService(gameRepo repository.GameRepository, cardCache *cache.CardCache) *GameLogService {
	return &GameLogService{gameRepo: gameRepo, cardCache: cardCache}
}

// GameLog holds a complete game history in human-readable form.
type GameLog struct {
	GameID     string        `json:"gameId"`
	Player1    PlayerSummary `json:"player1"`
	Player2    PlayerSummary `json:"player2"`
	Winner     *string       `json:"winner"`
	WinReason  string        `json:"winReason"`
	TotalTurns int64         `json:"totalTurns"`
	StartedAt  time.Time     `json:"startedAt"`
	FinishedAt *time.Time    `json:"finishedAt,omitempty"`
	Duration   string        `json:"duration,omitempty"`
	Actions    []ActionEntry `json:"actions"`
}

// PlayerSummary holds per-player metadata in a game log.
type PlayerSummary struct {
	PlayerID    string `json:"playerId"`
	IsNPC       bool   `json:"isNpc"`
	FinalBudget int64  `json:"finalBudget"`
}

// ActionEntry is a single human-readable action in the game log.
type ActionEntry struct {
	Seq         int64  `json:"seq"`
	Turn        int64  `json:"turn,omitempty"`
	Phase       string `json:"phase,omitempty"`
	PlayerID    string `json:"playerId,omitempty"`
	EventType   string `json:"eventType"`
	Description string `json:"description"`
	Timestamp   string `json:"timestamp,omitempty"`
}

// GetGameLog builds a human-readable log for a game.
func (s *GameLogService) GetGameLog(ctx context.Context, gameID string) (*GameLog, error) {
	game, err := s.gameRepo.GetGame(ctx, gameID)
	if err != nil {
		return nil, fmt.Errorf("get game: %w", err)
	}

	state, err := s.gameRepo.GetGameState(ctx, gameID)
	if err != nil {
		return nil, fmt.Errorf("get game state: %w", err)
	}

	events, err := s.gameRepo.GetEvents(ctx, gameID)
	if err != nil {
		return nil, fmt.Errorf("get events: %w", err)
	}

	log := &GameLog{
		GameID: gameID,
		Player1: PlayerSummary{
			PlayerID:    game.Player1ID,
			IsNPC:       strings.HasPrefix(game.Player1ID, "npc-"),
			FinalBudget: state.GetBudget(1),
		},
		Player2: PlayerSummary{
			PlayerID:    game.Player2ID,
			IsNPC:       strings.HasPrefix(game.Player2ID, "npc-"),
			FinalBudget: state.GetBudget(2),
		},
		TotalTurns: state.CurrentTurn,
		StartedAt:  game.CreatedAt,
		FinishedAt: game.FinishedAt,
		Winner:     game.WinnerID,
	}

	if game.FinishedAt != nil {
		dur := game.FinishedAt.Sub(game.CreatedAt)
		log.Duration = dur.Truncate(time.Second).String()
	}

	// Convert events to action entries
	log.Actions = make([]ActionEntry, 0, len(events))
	for _, evt := range events {
		entry := s.eventToActionEntry(evt, game)
		log.Actions = append(log.Actions, entry)
	}

	// Extract win reason from last event
	if len(events) > 0 {
		last := events[len(events)-1]
		if last.EventType == model.EventGameOver {
			var data map[string]interface{}
			if json.Unmarshal(last.EventData, &data) == nil {
				if reason, ok := data["reason"].(string); ok {
					log.WinReason = reason
				}
			}
		}
	}

	return log, nil
}

// GetGameLogText returns a plain-text version of the game log.
func (s *GameLogService) GetGameLogText(ctx context.Context, gameID string) (string, error) {
	log, err := s.GetGameLog(ctx, gameID)
	if err != nil {
		return "", err
	}

	var b strings.Builder
	fmt.Fprintf(&b, "=== Game %s ===\n", log.GameID)
	fmt.Fprintf(&b, "P1: %s%s  vs  P2: %s%s\n",
		log.Player1.PlayerID, npcTag(log.Player1.IsNPC),
		log.Player2.PlayerID, npcTag(log.Player2.IsNPC))

	winnerLabel := "none"
	if log.Winner != nil {
		if *log.Winner == log.Player1.PlayerID {
			winnerLabel = "P1"
		} else {
			winnerLabel = "P2"
		}
	}
	fmt.Fprintf(&b, "Winner: %s", winnerLabel)
	if log.WinReason != "" {
		fmt.Fprintf(&b, " (%s)", log.WinReason)
	}
	fmt.Fprintf(&b, " | %d turns", log.TotalTurns)
	if log.Duration != "" {
		fmt.Fprintf(&b, " | %s", log.Duration)
	}
	fmt.Fprintln(&b)

	fmt.Fprintf(&b, "Final Budget: P1=%d  P2=%d\n", log.Player1.FinalBudget, log.Player2.FinalBudget)
	fmt.Fprintln(&b)

	for _, a := range log.Actions {
		fmt.Fprintf(&b, "[%d] %s\n", a.Seq, a.Description)
	}

	return b.String(), nil
}

func npcTag(isNPC bool) string {
	if isNPC {
		return " (NPC)"
	}
	return ""
}

// eventToActionEntry converts a raw GameEvent into a human-readable ActionEntry.
func (s *GameLogService) eventToActionEntry(evt *model.GameEvent, game *model.Game) ActionEntry {
	entry := ActionEntry{
		Seq:       evt.SequenceNumber,
		EventType: evt.EventType,
		Timestamp: evt.CreatedAt.Format(time.RFC3339),
	}
	if evt.PlayerID != nil {
		entry.PlayerID = *evt.PlayerID
	}

	var data map[string]interface{}
	if err := json.Unmarshal(evt.EventData, &data); err != nil {
		entry.Description = fmt.Sprintf("%s (unparseable data)", evt.EventType)
		return entry
	}

	playerLabel := s.playerLabel(evt.PlayerID, game)

	switch evt.EventType {
	case model.EventPlayCard:
		cardName := s.cardName(data, "cardId")
		zone := getString(data, "zone")
		cost := getInt(data, "cost")
		entry.Description = fmt.Sprintf("%s deployed \"%s\" to %s [-%d Budget]", playerLabel, cardName, zone, cost)

	case model.EventAttack:
		damage := getInt(data, "damage")
		entry.Description = fmt.Sprintf("%s attacked for %d damage", playerLabel, damage)

	case model.EventScaleUp:
		from := getString(data, "from")
		to := getString(data, "to")
		cost := getInt(data, "cost")
		entry.Description = fmt.Sprintf("%s scaled up %s → %s [-%d Budget]", playerLabel, from, to, cost)

	case model.EventDistributeYield:
		amount := getInt(data, "amount")
		entry.Description = fmt.Sprintf("%s distributed %d Yield", playerLabel, amount)

	case model.EventActivateEffect:
		effectID := getString(data, "effectId")
		entry.Description = fmt.Sprintf("%s activated effect: %s", playerLabel, effectID)

	case model.EventPhaseChange, model.EventPhaseEnd:
		prev := getString(data, "previousPhase")
		curr := getString(data, "currentPhase")
		if prev != "" && curr != "" {
			entry.Description = fmt.Sprintf("%s ended %s → %s", playerLabel, prev, curr)
		} else {
			entry.Description = fmt.Sprintf("Phase: %s", evt.EventType)
		}
		entry.Phase = curr

	case model.EventTurnEnd:
		nextTurn := getInt(data, "nextTurn")
		activePlayer := getInt(data, "activePlayer")
		entry.Description = fmt.Sprintf("Turn end → Turn %d (P%d)", nextTurn, activePlayer)
		entry.Turn = nextTurn

	case "draw":
		entry.Description = fmt.Sprintf("%s drew a card", playerLabel)

	case "yield":
		amount := getInt(data, "amount")
		entry.Description = fmt.Sprintf("%s generated %d Yield", playerLabel, amount)

	case "damage":
		amount := getInt(data, "amount")
		target := getString(data, "targetId")
		entry.Description = fmt.Sprintf("%s took %d damage on %s", playerLabel, amount, target)

	case "destroy":
		entry.Description = fmt.Sprintf("Resource destroyed for %s", playerLabel)

	case "discard":
		count := getInt(data, "count")
		entry.Description = fmt.Sprintf("%s discarded %d card(s)", playerLabel, count)

	case model.EventGameOver:
		reason := getString(data, "reason")
		winnerID := getString(data, "winner")
		winLabel := s.playerLabel(&winnerID, game)
		entry.Description = fmt.Sprintf("Game over: %s wins (%s)", winLabel, reason)

	default:
		entry.Description = fmt.Sprintf("%s: %s", evt.EventType, string(evt.EventData))
	}

	return entry
}

func (s *GameLogService) playerLabel(playerID *string, game *model.Game) string {
	if playerID == nil {
		return "System"
	}
	switch *playerID {
	case game.Player1ID:
		return "P1"
	case game.Player2ID:
		return "P2"
	default:
		return *playerID
	}
}

func (s *GameLogService) cardName(data map[string]interface{}, key string) string {
	cardNo := getInt(data, key)
	if cardNo == 0 {
		return "unknown"
	}
	card := s.cardCache.Get(int64(cardNo))
	if card == nil {
		return fmt.Sprintf("card#%d", cardNo)
	}
	return card.CardName
}

func getString(data map[string]interface{}, key string) string {
	v, ok := data[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

func getInt(data map[string]interface{}, key string) int64 {
	v, ok := data[key]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int64:
		return n
	default:
		return 0
	}
}
