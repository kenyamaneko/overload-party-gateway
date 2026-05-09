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

	apibattle "github.com/kenyamaneko/overload-party-battle/packages/api-battle-rpc-go"
)

// 生成済み battle RPC 型の re-export。
// 呼び出し側が service.ActionResult 等をそのまま使えるようにする。
type (
	BattleDeckCard    = apibattle.BattleDeckCard
	GameCreatedResult = apibattle.GameCreatedResult
	ActionEvent       = apibattle.ActionEvent
	ActionResult      = apibattle.ActionResult
)

// BattleClient は battle server REST API との通信インターフェースです。
// gateway はゲーム作成、アクション処理、ステート取得をこのクライアントに委譲する。
// プレイヤー向けメソッドは playerID ではなく playerNum (1/2) を使用する。
type BattleClient interface {
	GetNPCModels(ctx context.Context) (json.RawMessage, error)
	StartNPCBattle(ctx context.Context, deckCards []BattleDeckCard, npcModel string) (*GameCreatedResult, error)
	CreatePvPGame(ctx context.Context, deck1Cards, deck2Cards []BattleDeckCard) (*GameCreatedResult, error)
	ProcessAction(ctx context.Context, gameID string, playerNum int, actionType string, data json.RawMessage) (*ActionResult, error)
	GetGameStateForPlayer(ctx context.Context, gameID string, playerNum int) (json.RawMessage, error)
	GetTurnControlsForPlayer(ctx context.Context, gameID string, playerNum int) (json.RawMessage, error)
	AdvanceNpcTurn(ctx context.Context, gameID string) (*ActionResult, error)
	GetGameLog(ctx context.Context, gameID string) (json.RawMessage, error)
	GetGameLogText(ctx context.Context, gameID string) ([]byte, error)
}

const battleClientTimeout = 30 * time.Second

type battleClient struct {
	baseURL string
	client  *http.Client
}

// NewBattleClient は battle server への HTTP クライアントを生成します
func NewBattleClient(baseURL string) BattleClient {
	return &battleClient{
		baseURL: baseURL,
		client:  &http.Client{Timeout: battleClientTimeout},
	}
}

func (c *battleClient) GetNPCModels(ctx context.Context) (json.RawMessage, error) {
	return c.getRaw(ctx, "/api/v1/npc/models")
}

func (c *battleClient) StartNPCBattle(ctx context.Context, deckCards []BattleDeckCard, npcModel string) (*GameCreatedResult, error) {
	body := &apibattle.NpcBattleRequest{
		DeckCards: deckCards,
		NpcModel:  npcModel,
	}
	var result GameCreatedResult
	if err := c.post(ctx, "/api/v1/games/npc", body, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *battleClient) CreatePvPGame(ctx context.Context, deck1Cards, deck2Cards []BattleDeckCard) (*GameCreatedResult, error) {
	body := &apibattle.PvpBattleRequest{
		Deck1Cards: deck1Cards,
		Deck2Cards: deck2Cards,
	}
	var result GameCreatedResult
	if err := c.post(ctx, "/api/v1/games/pvp", body, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *battleClient) ProcessAction(ctx context.Context, gameID string, playerNum int, actionType string, data json.RawMessage) (*ActionResult, error) {
	var dataMap map[string]interface{}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &dataMap); err != nil {
			return nil, fmt.Errorf("battleClient: unmarshal action data: %w", err)
		}
	}
	body := &apibattle.GameActionRequest{
		PlayerNum:  int64(playerNum),
		ActionType: actionType,
		Data:       dataMap,
	}
	var result ActionResult
	if err := c.post(ctx, fmt.Sprintf("/api/v1/games/%s/actions", gameID), body, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *battleClient) AdvanceNpcTurn(ctx context.Context, gameID string) (*ActionResult, error) {
	var result ActionResult
	if err := c.post(ctx, fmt.Sprintf("/api/v1/games/%s/advance-npc", gameID), nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *battleClient) GetGameStateForPlayer(ctx context.Context, gameID string, playerNum int) (json.RawMessage, error) {
	path := fmt.Sprintf("/api/v1/games/%s/state/%d", gameID, playerNum)
	return c.getRaw(ctx, path)
}

func (c *battleClient) GetTurnControlsForPlayer(ctx context.Context, gameID string, playerNum int) (json.RawMessage, error) {
	path := fmt.Sprintf("/api/v1/games/%s/controls/%d", gameID, playerNum)
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

// parseBattleError は battle server のレスポンスから構造化エラーメッセージを抽出します。
// 抽出できない場合はステータスコードと body をそのまま含めたエラーを返す。
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

// --- HTTP ヘルパー ---

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
