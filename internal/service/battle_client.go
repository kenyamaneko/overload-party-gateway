package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

// BattleDeckCard represents a card in a deck snapshot sent to the battle server.
type BattleDeckCard struct {
	CardID string `json:"CardId"`
	ArtNo  int64  `json:"ArtNo"`
}

// BattleClient is the interface for communicating with the battle server REST API.
// Gateway uses this to delegate game creation, action processing, and state retrieval.
type BattleClient interface {
	// GetNPCModels returns the list of available NPC models from the battle server.
	GetNPCModels(ctx context.Context) (json.RawMessage, error)
	// StartNPCBattle creates a new NPC game for the player.
	StartNPCBattle(ctx context.Context, playerID string, deckID int64, cards []BattleDeckCard, npcModel string) (*GameCreatedResult, error)
	// CreatePvPGame creates a new PvP game (called after matchmaking completes).
	CreatePvPGame(ctx context.Context, player1ID string, player1DeckID int64, player1Cards []BattleDeckCard, player2ID string, player2DeckID int64, player2Cards []BattleDeckCard) (*GameCreatedResult, error)
	// ProcessAction processes a game action and returns the result.
	ProcessAction(ctx context.Context, gameID, playerID, actionType string, data json.RawMessage) (*ActionResult, error)
	// GetGameStateForPlayer returns the info-hidden game state for a specific player.
	GetGameStateForPlayer(ctx context.Context, gameID, playerID string) (json.RawMessage, error)
	// GetTurnControlsForPlayer returns the turn controls JSON for a specific player, or nil if not their turn.
	GetTurnControlsForPlayer(ctx context.Context, gameID, playerID string) (json.RawMessage, error)
	// AdvanceNpcTurn runs the NPC turn if the active player is NPC.
	// Returns action events so the gateway can relay them to the human player.
	AdvanceNpcTurn(ctx context.Context, gameID, playerID string) (*ActionResult, error)
	// GetGameLog returns the game log (replay) as JSON.
	GetGameLog(ctx context.Context, gameID string) (json.RawMessage, error)
	// GetGameLogText returns the game log (replay) as plain text.
	GetGameLogText(ctx context.Context, gameID string) ([]byte, error)
}

// GameCreatedResult is returned when a new game is created.
type GameCreatedResult struct {
	GameID    string `json:"game_id"`
	Player1ID string `json:"player1_id"`
	Player2ID string `json:"player2_id"`
}

// ActionEvent represents a single game event returned by the battle server.
type ActionEvent struct {
	Sequence  int64                  `json:"sequence"`
	EventType string                 `json:"event_type"`
	PlayerID  string                 `json:"player_id"`
	EventData json.RawMessage        `json:"event_data"`
	State     json.RawMessage        `json:"state"`
}

// ActionResult is returned after a game action is processed.
type ActionResult struct {
	GameOver  bool          `json:"game_over"`
	WinnerNum int64         `json:"winner_num"`
	WinReason string        `json:"win_reason"`
	Events    []ActionEvent `json:"events"`
}

const battleClientTimeout = 30 * time.Second

type battleClient struct {
	baseURL string
	client  *http.Client
}

func NewBattleClient(baseURL string) BattleClient {
	return &battleClient{
		baseURL: baseURL,
		client:  &http.Client{Timeout: battleClientTimeout},
	}
}

func (c *battleClient) GetNPCModels(ctx context.Context) (json.RawMessage, error) {
	return c.getRaw(ctx, "/api/v1/npc/models")
}

func (c *battleClient) StartNPCBattle(ctx context.Context, playerID string, deckID int64, cards []BattleDeckCard, npcModel string) (*GameCreatedResult, error) {
	body := map[string]any{
		"PlayerID": playerID,
		"DeckID":   deckID,
		"Cards":    cards,
		"NpcModel": npcModel,
	}
	var result GameCreatedResult
	if err := c.post(ctx, "/api/v1/games/npc", body, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *battleClient) CreatePvPGame(ctx context.Context, player1ID string, player1DeckID int64, player1Cards []BattleDeckCard, player2ID string, player2DeckID int64, player2Cards []BattleDeckCard) (*GameCreatedResult, error) {
	body := map[string]any{
		"Player1ID":     player1ID,
		"Player1DeckID": player1DeckID,
		"Player1Cards":  player1Cards,
		"Player2ID":     player2ID,
		"Player2DeckID": player2DeckID,
		"Player2Cards":  player2Cards,
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

func (c *battleClient) AdvanceNpcTurn(ctx context.Context, gameID, playerID string) (*ActionResult, error) {
	body := map[string]any{
		"PlayerID": playerID,
	}
	var result ActionResult
	if err := c.post(ctx, fmt.Sprintf("/api/v1/games/%s/advance-npc", gameID), body, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *battleClient) GetGameStateForPlayer(ctx context.Context, gameID, playerID string) (json.RawMessage, error) {
	path := fmt.Sprintf("/api/v1/games/%s/state/%s", gameID, playerID)
	return c.getRaw(ctx, path)
}

func (c *battleClient) GetTurnControlsForPlayer(ctx context.Context, gameID, playerID string) (json.RawMessage, error) {
	path := fmt.Sprintf("/api/v1/games/%s/controls/%s", gameID, playerID)
	raw, err := c.getRaw(ctx, path)
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	return raw, nil
}

func (c *battleClient) GetGameLog(ctx context.Context, gameID string) (json.RawMessage, error) {
	return c.getRaw(ctx, fmt.Sprintf("/api/v1/games/%s/log", gameID))
}

func (c *battleClient) GetGameLogText(ctx context.Context, gameID string) ([]byte, error) {
	path := fmt.Sprintf("/api/v1/games/%s/log/text", gameID)
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

	if resp.StatusCode != http.StatusOK {
		return nil, parseBattleError(resp.StatusCode, body)
	}

	return body, nil
}

// parseBattleError attempts to extract a structured error message from the battle server response body.
// Falls back to including the raw status code and body.
func parseBattleError(statusCode int, body []byte) error {
	var errResp struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &errResp); err != nil {
		log.Printf("failed to parse battle error response: %v", err)
	} else if errResp.Error != "" {
		return errors.New(errResp.Error)
	}
	return fmt.Errorf("battle server returned %d: %s", statusCode, string(body))
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

	if resp.StatusCode != http.StatusOK {
		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("read error response: %w", err)
		}
		return parseBattleError(resp.StatusCode, respBody)
	}

	// 成功時はストリームから直接デコードし、中間[]byteバッファの確保を省く。
	if result != nil {
		if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
			return fmt.Errorf("decode response: %w", err)
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
		return nil, parseBattleError(resp.StatusCode, body)
	}

	return json.RawMessage(body), nil
}
