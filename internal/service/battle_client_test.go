package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeBattleServerRecorder struct {
	calls int
	body  []byte
}

func newFakeBattleServer(t *testing.T, statusCode int, body string) (*httptest.Server, *fakeBattleServerRecorder) {
	t.Helper()

	rec := &fakeBattleServerRecorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.calls++
		b, _ := io.ReadAll(r.Body)
		rec.body = b

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	return srv, rec
}

func newTestBattleClient(t *testing.T, statusCode int, body string) (BattleClient, *fakeBattleServerRecorder) {
	t.Helper()

	srv, rec := newFakeBattleServer(t, statusCode, body)
	return NewBattleClient(srv.URL, srv.Client()), rec
}

func dummyDeckCards() []BattleDeckCard {
	return []BattleDeckCard{{CardID: "card-1", ArtNo: 1}}
}

func dummyDeckInitiatives() DeckInitiatives {
	return DeckInitiatives{RoutineID: "routine-1", SpecialID: "special-1"}
}

func dummyPlayerSummary(name string) PlayerSummaryRequest {
	return PlayerSummaryRequest{Name: name}
}

func TestParseBattleError(t *testing.T) {
	t.Run("[対戦サービス連携]battle サービスからのエラー応答内容の決定", func(t *testing.T) {
		t.Run("応答本文に構造化されたエラーメッセージが含まれるとき、そのメッセージがエラーの内容になる", func(t *testing.T) {
			err := parseBattleError(http.StatusBadRequest, []byte(`{"error":"invalid npc_model: xyz"}`))

			require.Error(t, err)
			assert.Equal(t, "invalid npc_model: xyz", err.Error())
		})

		tests := []struct {
			name       string
			statusCode int
			body       []byte
		}{
			{
				name:       "応答本文がJSONとして解釈できない形式のとき、ステータスコードと応答本文を含むエラーになる",
				statusCode: http.StatusNotFound,
				body:       []byte("not a json body"),
			},
			{
				name:       "応答本文のエラーメッセージが空文字であるとき、ステータスコードと応答本文を含むエラーになる",
				statusCode: http.StatusUnprocessableEntity,
				body:       []byte(`{"error":""}`),
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				err := parseBattleError(tt.statusCode, tt.body)

				require.Error(t, err)
				assert.Contains(t, err.Error(), strconv.Itoa(tt.statusCode))
				assert.Contains(t, err.Error(), string(tt.body))
			})
		}
	})
}

func TestStartNPCBattle(t *testing.T) {
	t.Run("[対戦サービス連携]NPC対戦のゲーム作成結果決定", func(t *testing.T) {
		tests := []struct {
			name       string
			statusCode int
			body       string
			verify     func(t *testing.T, result *GameCreatedResult, err error)
		}{
			{
				name:       "応答が成功を表し、内容を期待する形式として解釈できるとき、その内容が結果として返る",
				statusCode: http.StatusOK,
				body:       `{"game_id":"game-123"}`,
				verify: func(t *testing.T, result *GameCreatedResult, err error) {
					require.NoError(t, err)
					assert.Equal(t, "game-123", result.GameID)
				},
			},
			{
				name:       "応答が成功を表すが、内容を期待する形式として解釈できないとき、エラーになる",
				statusCode: http.StatusOK,
				body:       `not a json body`,
				verify: func(t *testing.T, result *GameCreatedResult, err error) {
					require.Error(t, err)
				},
			},
			{
				name:       `応答が失敗を表すとき、構造化されたエラーメッセージを含むエラーになる（「battle サービスからのエラー応答内容の決定」の規定に従う）`,
				statusCode: http.StatusBadRequest,
				body:       `{"error":"npc_model not found"}`,
				verify: func(t *testing.T, result *GameCreatedResult, err error) {
					require.Error(t, err)
					assert.Equal(t, "npc_model not found", err.Error())
				},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				client, _ := newTestBattleClient(t, tt.statusCode, tt.body)

				result, err := client.StartNPCBattle(
					context.Background(),
					dummyDeckCards(),
					dummyDeckInitiatives(),
					"npc-easy",
					dummyPlayerSummary("player-1"),
					dummyPlayerSummary("npc-1"),
				)

				tt.verify(t, result, err)
			})
		}
	})
}

func TestCreatePvPGame(t *testing.T) {
	t.Run("[対戦サービス連携]PvP対戦のゲーム作成結果決定", func(t *testing.T) {
		tests := []struct {
			name       string
			statusCode int
			body       string
			verify     func(t *testing.T, result *GameCreatedResult, err error)
		}{
			{
				name:       "応答が成功を表し、内容を期待する形式として解釈できるとき、その内容が結果として返る",
				statusCode: http.StatusOK,
				body:       `{"game_id":"game-456"}`,
				verify: func(t *testing.T, result *GameCreatedResult, err error) {
					require.NoError(t, err)
					assert.Equal(t, "game-456", result.GameID)
				},
			},
			{
				name:       "応答が成功を表すが、内容を期待する形式として解釈できないとき、エラーになる",
				statusCode: http.StatusOK,
				body:       `not a json body`,
				verify: func(t *testing.T, result *GameCreatedResult, err error) {
					require.Error(t, err)
				},
			},
			{
				name:       `応答が失敗を表すとき、構造化されたエラーメッセージを含むエラーになる（「battle サービスからのエラー応答内容の決定」の規定に従う）`,
				statusCode: http.StatusBadRequest,
				body:       `{"error":"deck invalid"}`,
				verify: func(t *testing.T, result *GameCreatedResult, err error) {
					require.Error(t, err)
					assert.Equal(t, "deck invalid", err.Error())
				},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				client, _ := newTestBattleClient(t, tt.statusCode, tt.body)

				result, err := client.CreatePvPGame(
					context.Background(),
					dummyDeckCards(),
					dummyDeckCards(),
					dummyDeckInitiatives(),
					dummyDeckInitiatives(),
					dummyPlayerSummary("player-1"),
					dummyPlayerSummary("player-2"),
				)

				tt.verify(t, result, err)
			})
		}
	})
}

func TestProcessAction(t *testing.T) {
	t.Run("[対戦サービス連携]アクション実行時の入力データ変換", func(t *testing.T) {
		t.Run("入力データが空のとき、変換後のデータも空になり、エラーにならない", func(t *testing.T) {
			client, rec := newTestBattleClient(t, http.StatusOK, `{}`)

			_, err := client.ProcessAction(context.Background(), "game-1", 1, "play_card", nil)
			require.NoError(t, err)

			assert.JSONEq(t, `{"action_type":"play_card","player_num":1,"data":null}`, string(rec.body))
		})

		t.Run("入力データが有効な構造化データ(JSONオブジェクト)のとき、対応するデータ構造に変換される", func(t *testing.T) {
			client, rec := newTestBattleClient(t, http.StatusOK, `{}`)

			data := json.RawMessage(`{"target":"card-9","count":2}`)
			_, err := client.ProcessAction(context.Background(), "game-1", 1, "play_card", data)
			require.NoError(t, err)

			assert.JSONEq(t, `{"action_type":"play_card","player_num":1,"data":{"target":"card-9","count":2}}`, string(rec.body))
		})

		t.Run("入力データが不正な形式(解釈できない)のとき、エラーになる", func(t *testing.T) {
			client, rec := newTestBattleClient(t, http.StatusOK, `{}`)

			data := json.RawMessage(`{not valid json`)
			_, err := client.ProcessAction(context.Background(), "game-1", 1, "play_card", data)

			require.Error(t, err)
			assert.Zero(t, rec.calls)
		})
	})
}

func TestAdvanceNpcTurn(t *testing.T) {
	t.Run("[対戦サービス連携]NPCターンの進行結果決定", func(t *testing.T) {
		tests := []struct {
			name       string
			statusCode int
			body       string
			verify     func(t *testing.T, result *ActionResult, err error)
		}{
			{
				name:       "応答が成功を表し、内容を期待する形式として解釈できるとき、その内容が結果として返る",
				statusCode: http.StatusOK,
				body:       `{"events":[],"game_over":true,"npc_pending":false,"win_reason":"hp_zero","winning_player_num":1}`,
				verify: func(t *testing.T, result *ActionResult, err error) {
					require.NoError(t, err)
					assert.True(t, result.GameOver)
					assert.Equal(t, "hp_zero", result.WinReason)
					assert.Equal(t, int64(1), result.WinningPlayerNum)
				},
			},
			{
				name:       "応答が成功を表すが、内容を期待する形式として解釈できないとき、エラーになる",
				statusCode: http.StatusOK,
				body:       `not a json body`,
				verify: func(t *testing.T, result *ActionResult, err error) {
					require.Error(t, err)
				},
			},
			{
				name:       `応答が失敗を表すとき、構造化されたエラーメッセージを含むエラーになる（「battle サービスからのエラー応答内容の決定」の規定に従う）`,
				statusCode: http.StatusBadRequest,
				body:       `{"error":"game not found"}`,
				verify: func(t *testing.T, result *ActionResult, err error) {
					require.Error(t, err)
					assert.Equal(t, "game not found", err.Error())
				},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				client, _ := newTestBattleClient(t, tt.statusCode, tt.body)

				result, err := client.AdvanceNpcTurn(context.Background(), "game-1")

				tt.verify(t, result, err)
			})
		}
	})
}

func TestListNpcModels(t *testing.T) {
	t.Run("[対戦サービス連携]NPC モデル一覧取得", func(t *testing.T) {
		tests := []struct {
			name       string
			statusCode int
			body       string
			verify     func(t *testing.T, result []NpcModelEntry, err error)
		}{
			{
				name:       "一覧の内容が0件のとき、要素数0件の一覧が返る",
				statusCode: http.StatusOK,
				body:       `{"models":[]}`,
				verify: func(t *testing.T, result []NpcModelEntry, err error) {
					require.NoError(t, err)
					assert.Empty(t, result)
				},
			},
			{
				name:       "一覧の内容が複数件のとき、その内容が一覧として返る",
				statusCode: http.StatusOK,
				body:       `{"models":[{"model":"m1","display_name":"Model One","difficulty":"easy","faction":"faction-a"},{"model":"m2","display_name":"Model Two","difficulty":"hard","faction":"faction-b"}]}`,
				verify: func(t *testing.T, result []NpcModelEntry, err error) {
					require.NoError(t, err)
					assert.Equal(t, []NpcModelEntry{
						{Model: "m1", DisplayName: "Model One", Difficulty: "easy", Faction: "faction-a"},
						{Model: "m2", DisplayName: "Model Two", Difficulty: "hard", Faction: "faction-b"},
					}, result)
				},
			},
			{
				name:       "一覧の内容が期待する形式として解釈できない(不正な形式である)とき、エラーになる",
				statusCode: http.StatusOK,
				body:       `{"models":"not-an-array"}`,
				verify: func(t *testing.T, result []NpcModelEntry, err error) {
					require.Error(t, err)
				},
			},
			{
				name:       "対象のNPCモデル一覧が存在しないとき、一覧が欠落していることを表すエラーになる",
				statusCode: http.StatusNotFound,
				body:       ``,
				verify: func(t *testing.T, result []NpcModelEntry, err error) {
					require.Error(t, err)
					assert.ErrorIs(t, err, errMissingNpcModels)
				},
			},
			{
				name:       `応答が失敗を表すとき、構造化されたエラーメッセージを含むエラーになる（「battle サービスからのエラー応答内容の決定」の規定に従う）`,
				statusCode: http.StatusInternalServerError,
				body:       `{"error":"npc models unavailable"}`,
				verify: func(t *testing.T, result []NpcModelEntry, err error) {
					require.Error(t, err)
					assert.Equal(t, "npc models unavailable", err.Error())
				},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				client, _ := newTestBattleClient(t, tt.statusCode, tt.body)

				result, err := client.ListNpcModels(context.Background())

				tt.verify(t, result, err)
			})
		}
	})
}

func TestGetGameStateForPlayer(t *testing.T) {
	t.Run("[対戦サービス連携]プレイヤー視点の対戦状態取得", func(t *testing.T) {
		tests := []struct {
			name       string
			statusCode int
			body       string
			verify     func(t *testing.T, result json.RawMessage, err error)
		}{
			{
				name:       "対戦状態の内容が含まれているとき、その内容がそのまま返る",
				statusCode: http.StatusOK,
				body:       `{"gameID":"game-1","currentTurn":3}`,
				verify: func(t *testing.T, result json.RawMessage, err error) {
					require.NoError(t, err)
					assert.JSONEq(t, `{"gameID":"game-1","currentTurn":3}`, string(result))
				},
			},
			{
				name:       "対象のゲームまたはプレイヤーが存在しないとき、対戦状態が欠落していることを表すエラーになる",
				statusCode: http.StatusNotFound,
				body:       ``,
				verify: func(t *testing.T, result json.RawMessage, err error) {
					require.Error(t, err)
					assert.ErrorIs(t, err, errMissingGameState)
				},
			},
			{
				name:       `応答が失敗を表すとき、構造化されたエラーメッセージを含むエラーになる（「battle サービスからのエラー応答内容の決定」の規定に従う）`,
				statusCode: http.StatusBadRequest,
				body:       `{"error":"invalid player_num"}`,
				verify: func(t *testing.T, result json.RawMessage, err error) {
					require.Error(t, err)
					assert.Equal(t, "invalid player_num", err.Error())
				},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				client, _ := newTestBattleClient(t, tt.statusCode, tt.body)

				result, err := client.GetGameStateForPlayer(context.Background(), "game-1", 1)

				tt.verify(t, result, err)
			})
		}
	})
}

func TestGetTurnControlsForPlayer(t *testing.T) {
	t.Run("[対戦サービス連携]プレイヤーの手番操作情報取得", func(t *testing.T) {
		t.Run("操作情報の内容が含まれているとき、その内容がそのまま返る", func(t *testing.T) {
			client, _ := newTestBattleClient(t, http.StatusOK, `{"canEndPhase":true,"discardRequired":0}`)

			result, err := client.GetTurnControlsForPlayer(context.Background(), "game-1", 1)

			require.NoError(t, err)
			assert.JSONEq(t, `{"canEndPhase":true,"discardRequired":0}`, string(result))
		})

		t.Run("操作情報の値がnullを表しているとき、エラーにならず操作情報が存在しないことを表す結果になる", func(t *testing.T) {
			client, _ := newTestBattleClient(t, http.StatusOK, `null`)

			result, err := client.GetTurnControlsForPlayer(context.Background(), "game-1", 1)

			require.NoError(t, err)
			assert.Nil(t, result)
		})

		t.Run("対象のゲームまたはプレイヤーの操作情報が存在しない(404)とき、エラーにならず操作情報が存在しないことを表す結果になる", func(t *testing.T) {
			client, _ := newTestBattleClient(t, http.StatusNotFound, ``)

			result, err := client.GetTurnControlsForPlayer(context.Background(), "game-1", 1)

			require.NoError(t, err)
			assert.Nil(t, result)
		})

		t.Run(`応答が失敗を表すとき、構造化されたエラーメッセージを含むエラーになる（「battle サービスからのエラー応答内容の決定」の規定に従う）`, func(t *testing.T) {
			client, _ := newTestBattleClient(t, http.StatusBadRequest, `{"error":"turn controls unavailable"}`)

			_, err := client.GetTurnControlsForPlayer(context.Background(), "game-1", 1)

			require.Error(t, err)
			assert.Equal(t, "turn controls unavailable", err.Error())
		})
	})
}
