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
	BattleDeckCard       = apibattle.BattleDeckCard
	GameCreatedResult    = apibattle.GameCreatedResult
	ActionEvent          = apibattle.ActionEvent
	ActionResult         = apibattle.ActionResult
	PlayerSummaryRequest = apibattle.PlayerSummaryRequest
	NpcModelEntry        = apibattle.NpcModelEntry
)

// BattleClient は battle server REST API との通信インターフェースです。
// gateway はゲーム作成、アクション処理、ステート取得をこのクライアントに委譲する。
// プレイヤー向けメソッドは playerID ではなく playerNum (1/2) を使用する。
//
// client 公開 path (`/api/v1/games/{id}/log[/text]`, `/api/v1/npc/models`) は
// gateway path-prefix forwarder が直接 forward するため本 interface には含めない。
type BattleClient interface {
	StartNPCBattle(ctx context.Context, deckCards []BattleDeckCard, npcModel string, player1Summary, player2Summary PlayerSummaryRequest) (*GameCreatedResult, error)
	CreatePvPGame(ctx context.Context, deck1Cards, deck2Cards []BattleDeckCard, player1Summary, player2Summary PlayerSummaryRequest) (*GameCreatedResult, error)
	ProcessAction(ctx context.Context, gameID string, playerNum int, actionType string, data json.RawMessage) (*ActionResult, error)
	GetGameStateForPlayer(ctx context.Context, gameID string, playerNum int) (json.RawMessage, error)
	GetTurnControlsForPlayer(ctx context.Context, gameID string, playerNum int) (json.RawMessage, error)
	AdvanceNpcTurn(ctx context.Context, gameID string) (*ActionResult, error)
	ListNpcModels(ctx context.Context) ([]NpcModelEntry, error)
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

func (c *battleClient) StartNPCBattle(ctx context.Context, deckCards []BattleDeckCard, npcModel string, player1Summary, player2Summary PlayerSummaryRequest) (*GameCreatedResult, error) {
	body := &apibattle.NpcBattleRequest{
		DeckCards:      deckCards,
		NpcModel:       npcModel,
		Player1Summary: player1Summary,
		Player2Summary: player2Summary,
	}
	var result GameCreatedResult
	if err := c.post(ctx, "/api/v1/games/npc", body, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *battleClient) CreatePvPGame(ctx context.Context, deck1Cards, deck2Cards []BattleDeckCard, player1Summary, player2Summary PlayerSummaryRequest) (*GameCreatedResult, error) {
	body := &apibattle.PvpBattleRequest{
		Deck1Cards:     deck1Cards,
		Deck2Cards:     deck2Cards,
		Player1Summary: player1Summary,
		Player2Summary: player2Summary,
	}
	var result GameCreatedResult
	if err := c.post(ctx, "/api/v1/games/pvp", body, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *battleClient) ListNpcModels(ctx context.Context) ([]NpcModelEntry, error) {
	raw, err := c.getRaw(ctx, "/api/v1/npc/models")
	if err != nil {
		return nil, err
	}
	var resp apibattle.NpcModelsResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal npc models: %w", err)
	}
	return resp.Models, nil
}

func (c *battleClient) ProcessAction(ctx context.Context, gameID string, playerNum int, actionType string, data json.RawMessage) (*ActionResult, error) {
	dataMap, err := rawToMap(data)
	if err != nil {
		return nil, err
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

// 原則に従ってパススルーしたいが、battle openapi が data を additionalProperties: true で
// 宣言しており api-battle-rpc-go の Data が map[] を要求するため、battle RPC 境界で
// map[string]interface{} に変換する。
func rawToMap(raw json.RawMessage) (map[string]interface{}, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var out map[string]interface{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("unmarshal action data: %w", err)
	}
	return out, nil
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
	defer func() { _ = resp.Body.Close() }()

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
	defer func() { _ = resp.Body.Close() }()

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
