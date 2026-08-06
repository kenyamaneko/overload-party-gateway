package ws

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apiaccount "github.com/kenyamaneko/overload-party-account/packages/api-account"
	apibattle "github.com/kenyamaneko/overload-party-battle/packages/api-battle-rpc-go"
	gamelogic "github.com/kenyamaneko/overload-party-battle/packages/game-logic-constants-go"
	apicard "github.com/kenyamaneko/overload-party-card/packages/api-card"

	genws "github.com/kenyamaneko/overload-party-gateway/packages/ws-constants"
)

// validNpcDeckFn は card.GetDeckFn の正常応答（有効なデッキ）。
func validNpcDeckFn(string) (int, any) {
	return http.StatusOK, apicard.DeckDetailResponse{
		Deck:  apicard.Deck{RoutineID: "RTN-TST01", SpecialID: "SPC-TST01"},
		Cards: []apicard.DeckCard{{CardID: "TST-0001", ArtNo: 1, Count: 1}},
	}
}

// npcAdvanceResponse は AdvanceNpcTurn 1 回分の応答を表す。
type npcAdvanceResponse struct {
	status int
	body   any
}

// advanceNpcTurnScript は AdvanceNpcTurnFn の呼出ごとに entries を順番に返す。
type advanceNpcTurnScript struct {
	t       *testing.T
	entries []npcAdvanceResponse

	mu    sync.Mutex
	calls int
}

func newAdvanceNpcTurnScript(t *testing.T, entries ...npcAdvanceResponse) *advanceNpcTurnScript {
	t.Helper()
	return &advanceNpcTurnScript{t: t, entries: entries}
}

func (s *advanceNpcTurnScript) fn(string) (int, any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.calls >= len(s.entries) {
		s.calls++
		s.t.Errorf("AdvanceNpcTurnFn called more than the scripted %d times (call #%d)", len(s.entries), s.calls)
		return http.StatusOK, apibattle.ActionResult{NpcPending: false}
	}
	e := s.entries[s.calls]
	s.calls++
	return e.status, e.body
}

func (s *advanceNpcTurnScript) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

// createNpcGameRecorder は battle.CreateNpcGameFn への呼出内容を記録する。
type createNpcGameRecorder struct {
	mu  sync.Mutex
	req apibattle.NpcBattleRequest
}

func (r *createNpcGameRecorder) fn(req apibattle.NpcBattleRequest) (int, any) {
	r.mu.Lock()
	r.req = req
	r.mu.Unlock()
	return http.StatusOK, apibattle.GameCreatedResult{GameID: "TST-GAME-NPC-1"}
}

func (r *createNpcGameRecorder) snapshotRequest() apibattle.NpcBattleRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.req
}

func TestWSNpcBattleStart(t *testing.T) {
	t.Run("NPC対戦開始", func(t *testing.T) {
		t.Run("オンボーディング未完了のとき、再試行不可のnpc_battle_errorが返る", func(t *testing.T) {
			srv := newWSTestServer(t, nil)
			srv.seedAccount("uid-npc-onboarding", "player-npc-onboarding")
			srv.account.GetPlayerFn = func() (int, any) {
				return http.StatusOK, apiaccount.PlayerResponse{
					PlayerID: "self", OnboardingStatus: apiaccount.OnboardingStatusNameSet,
				}
			}
			conn := srv.dial(t, "uid-npc-onboarding")

			require.NoError(t, conn.WriteJSON(WSMessage{
				Type: genws.WSClientMsgNpcBattleStart,
				Data: mustMarshal(NPCBattleStartMessage{DeckID: 1, NpcModel: "TST-NPC-A"}),
			}))

			errMsg := readUntilType(t, conn, genws.WSServerMsgError)
			errBody := decodeError(t, errMsg)
			assert.Equal(t, "npc_battle_error", errBody.ErrorCode)
			assert.False(t, errBody.Retryable)
			assert.Equal(t, "onboarding not completed", errBody.Message)
		})

		t.Run("当日のバトル回数上限に達しているとき、再試行不可のnpc_battle_errorが返る", func(t *testing.T) {
			srv := newWSTestServer(t, nil)
			srv.seedAccount("uid-npc-limit", "player-npc-limit")
			name := "TST-PLAYER"
			srv.account.GetPlayerFn = func() (int, any) {
				return http.StatusOK, apiaccount.PlayerResponse{
					PlayerID: "self", OnboardingStatus: apiaccount.OnboardingStatusCompleted, Name: &name,
				}
			}
			srv.account.GetBattleLimitFn = func() (int, any) {
				return http.StatusOK, apiaccount.BattleLimitResponse{CanBattle: false}
			}
			conn := srv.dial(t, "uid-npc-limit")

			require.NoError(t, conn.WriteJSON(WSMessage{
				Type: genws.WSClientMsgNpcBattleStart,
				Data: mustMarshal(NPCBattleStartMessage{DeckID: 1, NpcModel: "TST-NPC-A"}),
			}))

			errMsg := readUntilType(t, conn, genws.WSServerMsgError)
			errBody := decodeError(t, errMsg)
			assert.Equal(t, "npc_battle_error", errBody.ErrorCode)
			assert.False(t, errBody.Retryable)
			assert.Equal(t, "daily battle limit reached", errBody.Message)
		})

		t.Run("デッキ検証に失敗すると、再試行不可のnpc_battle_errorが返る", func(t *testing.T) {
			srv := newWSTestServer(t, nil)
			srv.seedAccount("uid-npc-deck-invalid", "player-npc-deck-invalid")
			setOnboardedAccount(srv.account, "TST-PLAYER", 1)
			srv.card.ValidateDeckForBattleFn = func(string) (int, any) {
				return http.StatusBadRequest, "deck invalid"
			}
			conn := srv.dial(t, "uid-npc-deck-invalid")

			require.NoError(t, conn.WriteJSON(WSMessage{
				Type: genws.WSClientMsgNpcBattleStart,
				Data: mustMarshal(NPCBattleStartMessage{DeckID: 1, NpcModel: "TST-NPC-A"}),
			}))

			errMsg := readUntilType(t, conn, genws.WSServerMsgError)
			errBody := decodeError(t, errMsg)
			assert.Equal(t, "npc_battle_error", errBody.ErrorCode)
			assert.False(t, errBody.Retryable)
		})

		t.Run("デッキの解決に失敗すると、再試行可のnpc_battle_errorが返る", func(t *testing.T) {
			srv := newWSTestServer(t, nil)
			srv.seedAccount("uid-npc-deck-fail", "player-npc-deck-fail")
			setOnboardedAccount(srv.account, "TST-PLAYER", 1)
			srv.card.GetDeckFn = func(string) (int, any) {
				return http.StatusInternalServerError, "deck lookup failed"
			}
			conn := srv.dial(t, "uid-npc-deck-fail")

			require.NoError(t, conn.WriteJSON(WSMessage{
				Type: genws.WSClientMsgNpcBattleStart,
				Data: mustMarshal(NPCBattleStartMessage{DeckID: 1, NpcModel: "TST-NPC-A"}),
			}))

			errMsg := readUntilType(t, conn, genws.WSServerMsgError)
			errBody := decodeError(t, errMsg)
			assert.Equal(t, "npc_battle_error", errBody.ErrorCode)
			assert.True(t, errBody.Retryable)
			assert.Equal(t, "failed to resolve deck", errBody.Message)
		})

		t.Run("NPCモデル一覧の取得に失敗すると、再試行可のnpc_battle_errorが返る", func(t *testing.T) {
			srv := newWSTestServer(t, nil)
			srv.seedAccount("uid-npc-models-fail", "player-npc-models-fail")
			setOnboardedAccount(srv.account, "TST-PLAYER", 1)
			srv.card.GetDeckFn = validNpcDeckFn
			srv.battle.ListNpcModelsFn = func() (int, any) {
				return http.StatusNotFound, nil
			}
			conn := srv.dial(t, "uid-npc-models-fail")

			require.NoError(t, conn.WriteJSON(WSMessage{
				Type: genws.WSClientMsgNpcBattleStart,
				Data: mustMarshal(NPCBattleStartMessage{DeckID: 1, NpcModel: "TST-NPC-A"}),
			}))

			errMsg := readUntilType(t, conn, genws.WSServerMsgError)
			errBody := decodeError(t, errMsg)
			assert.Equal(t, "npc_battle_error", errBody.ErrorCode)
			assert.True(t, errBody.Retryable)
			assert.Equal(t, "failed to resolve npc model: battle returned no npc models", errBody.Message)
		})

		t.Run("指定したNPCモデルが一覧に無いとき、再試行可のnpc_battle_errorが返る", func(t *testing.T) {
			srv := newWSTestServer(t, nil)
			srv.seedAccount("uid-npc-model-missing", "player-npc-model-missing")
			setOnboardedAccount(srv.account, "TST-PLAYER", 1)
			srv.card.GetDeckFn = validNpcDeckFn
			srv.battle.ListNpcModelsFn = func() (int, any) {
				return http.StatusOK, apibattle.NpcModelsResponse{
					Models: []apibattle.NpcModelEntry{{Model: "TST-NPC-OTHER", DisplayName: "Other NPC"}},
				}
			}
			conn := srv.dial(t, "uid-npc-model-missing")

			require.NoError(t, conn.WriteJSON(WSMessage{
				Type: genws.WSClientMsgNpcBattleStart,
				Data: mustMarshal(NPCBattleStartMessage{DeckID: 1, NpcModel: "TST-NPC-UNKNOWN"}),
			}))

			errMsg := readUntilType(t, conn, genws.WSServerMsgError)
			errBody := decodeError(t, errMsg)
			assert.Equal(t, "npc_battle_error", errBody.ErrorCode)
			assert.True(t, errBody.Retryable)
			assert.Equal(t, "failed to resolve npc model: npc model not found: TST-NPC-UNKNOWN", errBody.Message)
		})

		t.Run("ゲームの作成に失敗すると、再試行可のnpc_battle_errorが返る", func(t *testing.T) {
			srv := newWSTestServer(t, nil)
			srv.seedAccount("uid-npc-create-fail", "player-npc-create-fail")
			setOnboardedAccount(srv.account, "TST-PLAYER", 1)
			srv.card.GetDeckFn = validNpcDeckFn
			srv.battle.ListNpcModelsFn = func() (int, any) {
				return http.StatusOK, apibattle.NpcModelsResponse{
					Models: []apibattle.NpcModelEntry{{Model: "TST-NPC-A", DisplayName: "Test NPC A"}},
				}
			}
			srv.battle.CreateNpcGameFn = func(apibattle.NpcBattleRequest) (int, any) {
				return http.StatusInternalServerError, map[string]string{"error": "npc capacity exceeded"}
			}
			conn := srv.dial(t, "uid-npc-create-fail")

			require.NoError(t, conn.WriteJSON(WSMessage{
				Type: genws.WSClientMsgNpcBattleStart,
				Data: mustMarshal(NPCBattleStartMessage{DeckID: 1, NpcModel: "TST-NPC-A"}),
			}))

			errMsg := readUntilType(t, conn, genws.WSServerMsgError)
			errBody := decodeError(t, errMsg)
			assert.Equal(t, "npc_battle_error", errBody.ErrorCode)
			assert.True(t, errBody.Retryable)
			assert.Equal(t, "npc capacity exceeded", errBody.Message)
		})

		t.Run("オンボーディング完了済みでデッキ検証を通過すると、npc_battle_createdが返りスロット1に登録される", func(t *testing.T) {
			srv := newWSTestServer(t, nil)
			srv.seedAccount("uid-npc-ok", "player-npc-ok")
			setOnboardedAccount(srv.account, "TST-PLAYER", 1)
			srv.card.GetDeckFn = validNpcDeckFn
			srv.card.ValidateDeckForBattleFn = func(string) (int, any) { return http.StatusOK, nil }
			srv.battle.ListNpcModelsFn = func() (int, any) {
				return http.StatusOK, apibattle.NpcModelsResponse{
					Models: []apibattle.NpcModelEntry{{Model: "TST-NPC-A", DisplayName: "Test NPC A"}},
				}
			}
			rec := &createNpcGameRecorder{}
			srv.battle.CreateNpcGameFn = rec.fn
			conn := srv.dial(t, "uid-npc-ok")

			require.NoError(t, conn.WriteJSON(WSMessage{
				Type: genws.WSClientMsgNpcBattleStart,
				Data: mustMarshal(NPCBattleStartMessage{DeckID: 1, NpcModel: "TST-NPC-A"}),
			}))

			created := readUntilType(t, conn, genws.WSServerMsgNpcBattleCreated)
			var body NPCBattleCreatedMessage
			require.NoError(t, json.Unmarshal(created.Data, &body))
			assert.Equal(t, "TST-GAME-NPC-1", body.GameID)
			assert.Equal(t, "Test NPC A", rec.snapshotRequest().Player2Summary.Name)

			playerNum, err := srv.gamePlayerRepo.LookupPlayerNum(context.Background(), "TST-GAME-NPC-1", "player-npc-ok")
			require.NoError(t, err)
			assert.Equal(t, 1, playerNum)
		})
	})
}

func TestWSNpcTurnRelay(t *testing.T) {
	t.Run("NPCターンの中継", func(t *testing.T) {
		t.Run("NPCがターン内に複数回行動するとき、行動のたびにaction_performedフレームが順に届く", func(t *testing.T) {
			srv := newWSTestServer(t, nil)
			const gameID = "TST-GAME-NPC-RELAY-OK"
			srv.setupNpcGame(gameID, "uid-npc-relay-ok", "p-npc-relay-ok")
			npcPlayerNum := int64(2)
			script := newAdvanceNpcTurnScript(t,
				npcAdvanceResponse{status: http.StatusOK, body: apibattle.ActionResult{NpcPending: false}},
				npcAdvanceResponse{status: http.StatusOK, body: apibattle.ActionResult{
					NpcPending: true,
					Events: []apibattle.ActionEvent{{
						EventType: gamelogic.EventTypePlayCard, PlayerNum: &npcPlayerNum, Sequence: 2,
						State: map[string]interface{}{"turn": 1},
					}},
				}},
				npcAdvanceResponse{status: http.StatusOK, body: apibattle.ActionResult{
					NpcPending: false,
					Events: []apibattle.ActionEvent{{
						EventType: gamelogic.EventTypePlayCard, PlayerNum: &npcPlayerNum, Sequence: 3,
						State: map[string]interface{}{"turn": 1},
					}},
				}},
			)
			srv.battle.AdvanceNpcTurnFn = script.fn

			conn := srv.enterGame(t, "uid-npc-relay-ok", gameID)
			srv.battle.ProcessActionFn = func(string, apibattle.GameActionRequest) (int, any) {
				return http.StatusOK, apibattle.ActionResult{NpcPending: true}
			}

			require.NoError(t, conn.WriteJSON(WSMessage{
				Type: genws.WSClientMsgGameAction,
				Data: mustMarshal(GameActionMessage{GameID: gameID, ActionType: gamelogic.ActionTypeEndPhase}),
			}))

			first := readUntilActionPerformed(t, conn, gamelogic.EventTypePlayCard)
			assert.EqualValues(t, 2, first.Sequence)
			second := readUntilActionPerformed(t, conn, gamelogic.EventTypePlayCard)
			assert.EqualValues(t, 3, second.Sequence)
			assert.Equal(t, 3, script.callCount())
		})

		t.Run("NPCターンの進行に失敗すると、npc_turn_errorフレームが届く", func(t *testing.T) {
			srv := newWSTestServer(t, nil)
			const gameID = "TST-GAME-NPC-RELAY-ERR"
			srv.setupNpcGame(gameID, "uid-npc-relay-err", "p-npc-relay-err")
			script := newAdvanceNpcTurnScript(t,
				npcAdvanceResponse{status: http.StatusOK, body: apibattle.ActionResult{NpcPending: false}},
				npcAdvanceResponse{status: http.StatusInternalServerError, body: map[string]string{"error": "npc engine crashed"}},
			)
			srv.battle.AdvanceNpcTurnFn = script.fn

			conn := srv.enterGame(t, "uid-npc-relay-err", gameID)
			srv.battle.ProcessActionFn = func(string, apibattle.GameActionRequest) (int, any) {
				return http.StatusOK, apibattle.ActionResult{NpcPending: true}
			}

			require.NoError(t, conn.WriteJSON(WSMessage{
				Type: genws.WSClientMsgGameAction,
				Data: mustMarshal(GameActionMessage{GameID: gameID, ActionType: gamelogic.ActionTypeEndPhase}),
			}))

			errMsg := readUntilType(t, conn, genws.WSServerMsgError)
			errBody := decodeError(t, errMsg)
			assert.Equal(t, "npc_turn_error", errBody.ErrorCode)
			assert.True(t, errBody.Retryable)
			assert.Equal(t, 2, script.callCount())
		})
	})
}
