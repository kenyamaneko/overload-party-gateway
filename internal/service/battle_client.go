package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// BattleClient is the interface for communicating with the battle server REST API.
// Gateway uses this to delegate game creation, action processing, and state retrieval.
type BattleClient interface {
	// StartNPCBattle creates a new NPC game for the player.
	StartNPCBattle(ctx context.Context, playerID string, deckID int64, npcFaction string) (*GameCreatedResult, error)
	// CreatePvPGame creates a new PvP game (called after matchmaking completes).
	CreatePvPGame(ctx context.Context, player1ID string, player1DeckID int64, player2ID string, player2DeckID int64) (*GameCreatedResult, error)
	// ProcessAction processes a game action and returns the result.
	ProcessAction(ctx context.Context, gameID, playerID, actionType string, data json.RawMessage) (*ActionResult, error)
	// GetGameStateForPlayer returns the info-hidden game state for a specific player.
	GetGameStateForPlayer(ctx context.Context, gameID, playerID string) (json.RawMessage, error)
	// GetTurnControlsForPlayer returns the turn controls for a specific player, or nil if not their turn.
	GetTurnControlsForPlayer(ctx context.Context, gameID, playerID string) (*TurnControls, error)
}

// GameCreatedResult is returned when a new game is created.
type GameCreatedResult struct {
	GameID    string `json:"game_id"`
	Player1ID string `json:"player1_id"`
	Player2ID string `json:"player2_id"`
}

// ActionResult is returned after a game action is processed.
type ActionResult struct {
	GameOver  bool   `json:"game_over"`
	WinnerNum int64  `json:"winner_num"`
	WinReason string `json:"win_reason"`
}

// TurnControls describes available in-game controls for the active player.
type TurnControls struct {
	CanEndPhase     bool `json:"can_end_phase"`
	DiscardRequired int  `json:"discard_required"`
}

// battleClient implements BattleClient by calling the battle server REST API.
type battleClient struct {
	baseURL string
	client  *http.Client
}

// NewBattleClient creates a BattleClient that connects to the battle server at baseURL.
func NewBattleClient(baseURL string) BattleClient {
	return &battleClient{
		baseURL: baseURL,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (c *battleClient) StartNPCBattle(ctx context.Context, playerID string, deckID int64, npcFaction string) (*GameCreatedResult, error) {
	body := map[string]any{
		"PlayerID":   playerID,
		"DeckID":     deckID,
		"NpcFaction": npcFaction,
	}
	var result GameCreatedResult
	if err := c.post(ctx, "/api/v1/games/npc", body, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *battleClient) CreatePvPGame(ctx context.Context, player1ID string, player1DeckID int64, player2ID string, player2DeckID int64) (*GameCreatedResult, error) {
	body := map[string]any{
		"Player1ID":     player1ID,
		"Player1DeckID": player1DeckID,
		"Player2ID":     player2ID,
		"Player2DeckID": player2DeckID,
	}
	var result GameCreatedResult
	if err := c.post(ctx, "/api/v1/games/pvp", body, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *battleClient) ProcessAction(ctx context.Context, gameID, playerID, actionType string, data json.RawMessage) (*ActionResult, error) {
	body := map[string]any{
		"PlayerID":   playerID,
		"ActionType": actionType,
		"Data":       data,
	}
	var result ActionResult
	if err := c.post(ctx, fmt.Sprintf("/api/v1/games/%s/actions", gameID), body, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *battleClient) GetGameStateForPlayer(ctx context.Context, gameID, playerID string) (json.RawMessage, error) {
	path := fmt.Sprintf("/api/v1/games/%s/state/%s", gameID, playerID)
	return c.getRaw(ctx, path)
}

func (c *battleClient) GetTurnControlsForPlayer(ctx context.Context, gameID, playerID string) (*TurnControls, error) {
	path := fmt.Sprintf("/api/v1/games/%s/controls/%s", gameID, playerID)
	raw, err := c.getRaw(ctx, path)
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var tc TurnControls
	if err := json.Unmarshal(raw, &tc); err != nil {
		return nil, fmt.Errorf("unmarshal turn controls: %w", err)
	}
	return &tc, nil
}

// --- HTTP helpers ---

func (c *battleClient) post(ctx context.Context, path string, body any, result any) error {
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(jsonBody))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("battle server request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var errResp struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(respBody, &errResp) == nil && errResp.Error != "" {
			return fmt.Errorf("%s", errResp.Error)
		}
		return fmt.Errorf("battle server returned %d: %s", resp.StatusCode, string(respBody))
	}

	if result != nil {
		if err := json.Unmarshal(respBody, result); err != nil {
			return fmt.Errorf("unmarshal response: %w", err)
		}
	}
	return nil
}

func (c *battleClient) getRaw(ctx context.Context, path string) (json.RawMessage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("battle server request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		var errResp struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(body, &errResp) == nil && errResp.Error != "" {
			return nil, fmt.Errorf("%s", errResp.Error)
		}
		return nil, fmt.Errorf("battle server returned %d: %s", resp.StatusCode, string(body))
	}

	return json.RawMessage(body), nil
}
