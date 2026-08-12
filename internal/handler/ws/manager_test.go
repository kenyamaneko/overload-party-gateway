package ws

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apiaccount "github.com/kenyamaneko/overload-party-account/packages/api-account"
	apicard "github.com/kenyamaneko/overload-party-card/packages/api-card"
	"github.com/kenyamaneko/overload-party-gateway/internal/port"
	"github.com/kenyamaneko/overload-party-gateway/internal/service"
	wsconst "github.com/kenyamaneko/overload-party-gateway/packages/ws-constants"
	apimatchmaking "github.com/kenyamaneko/overload-party-matchmaking/packages/api-matchmaking"
)

func stringPtr(s string) *string { return &s }

func completedOnboardingPlayer(name string, level int64) *apiaccount.PlayerResponse {
	return &apiaccount.PlayerResponse{
		OnboardingStatus: apiaccount.OnboardingStatusCompleted,
		Name:             stringPtr(name),
		Level:            level,
	}
}

func notStartedOnboardingPlayer() *apiaccount.PlayerResponse {
	return &apiaccount.PlayerResponse{OnboardingStatus: apiaccount.OnboardingStatusNotStarted}
}

func matchmakingStartData(t *testing.T, deckID int64) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(MatchmakingStartMessage{DeckID: deckID})
	require.NoError(t, err)
	return data
}

func npcBattleStartData(t *testing.T, deckID int64, npcModel string) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(NPCBattleStartMessage{DeckID: deckID, NpcModel: npcModel})
	require.NoError(t, err)
	return data
}

func canBattleLimit() *apiaccount.BattleLimitResponse {
	return &apiaccount.BattleLimitResponse{CanBattle: true, DailyBattleCount: 1, DailyBattleLimit: 5}
}

func exceededBattleLimit() *apiaccount.BattleLimitResponse {
	return &apiaccount.BattleLimitResponse{CanBattle: false, DailyBattleCount: 5, DailyBattleLimit: 5}
}

func TestHandleMessage(t *testing.T) {
	t.Run("[接続管理]受信メッセージ種別ごとの振り分け", func(t *testing.T) {
		t.Run("受信したメッセージの種別が ping のとき、その接続にだけ即座に pong が返る", func(t *testing.T) {
			manager := newTestManager(t, managerDeps{})
			factory := newTestSocketFactory(t, manager.Hub)
			client, conn := factory.connect(t, "player-1")

			manager.HandleMessage(conn, &WSMessage{Type: wsconst.WSClientMsgPing})

			msg := client.readMessage(t)
			assert.Equal(t, wsconst.WSServerMsgPong, msg.Type)
		})

		t.Run("受信したメッセージの種別が matchmaking_cancel のとき、待機中のマッチメイキングタイムアウトが解除された上でマッチメイキングサービスへキャンセルが要求される。このタイムアウト解除は要求の成否に関わらず行われるため、以後待機時間が経過してもタイムアウトによるエラーは届かない", func(t *testing.T) {
			matchmaking := &stubMatchmakingClient{}
			manager := newTestManager(t, managerDeps{matchmaking: matchmaking, matchmakingTimeout: 80 * time.Millisecond})
			factory := newTestSocketFactory(t, manager.Hub)
			client, conn := factory.connect(t, "player-1")
			manager.startMatchWait("player-1")

			manager.HandleMessage(conn, &WSMessage{Type: wsconst.WSClientMsgMatchmakingCancel})

			msg := client.readMessage(t)
			assert.Equal(t, wsconst.WSServerMsgMatchmakingCancelled, msg.Type)
			assert.Equal(t, 1, matchmaking.cancelCallCount())
			client.expectNoMessage(t)
		})

		t.Run(`マッチメイキングサービスへのキャンセル要求が失敗したとき、その接続に error_code が matchmaking_error のエラーが返る(エラー内容 "failed to cancel: cancel failed")`, func(t *testing.T) {
			matchmaking := &stubMatchmakingClient{cancelFunc: func(ctx context.Context) error { return errors.New("cancel failed") }}
			manager := newTestManager(t, managerDeps{matchmaking: matchmaking})
			factory := newTestSocketFactory(t, manager.Hub)
			client, conn := factory.connect(t, "player-1")

			manager.HandleMessage(conn, &WSMessage{Type: wsconst.WSClientMsgMatchmakingCancel})

			msg := client.readMessage(t)
			assert.Equal(t, wsconst.WSServerMsgError, msg.Type)
			var payload ErrorMessage
			require.NoError(t, json.Unmarshal(msg.Data, &payload))
			assert.Equal(t, "matchmaking_error", payload.ErrorCode)
			assert.Equal(t, "failed to cancel: cancel failed", payload.Message)
		})

		t.Run("マッチメイキングサービスへのキャンセル要求が成功したとき、その接続に type が matchmaking_cancelled の応答が返る", func(t *testing.T) {
			matchmaking := &stubMatchmakingClient{cancelFunc: func(ctx context.Context) error { return nil }}
			manager := newTestManager(t, managerDeps{matchmaking: matchmaking})
			factory := newTestSocketFactory(t, manager.Hub)
			client, conn := factory.connect(t, "player-1")

			manager.HandleMessage(conn, &WSMessage{Type: wsconst.WSClientMsgMatchmakingCancel})

			msg := client.readMessage(t)
			assert.Equal(t, wsconst.WSServerMsgMatchmakingCancelled, msg.Type)
		})

		t.Run("受信したメッセージの種別が matchmaking_start のとき、デッキ検証・バトル可能回数確認等を経て応答が返る", func(t *testing.T) {
			account := &stubAccountClient{
				getMeFunc: func(ctx context.Context) (*apiaccount.PlayerResponse, error) {
					return completedOnboardingPlayer("Player One", 5), nil
				},
				getBattleLimitFunc: func(ctx context.Context) (*apiaccount.BattleLimitResponse, error) { return canBattleLimit(), nil },
			}
			card := &stubCardClient{validateDeckForBattleFunc: func(ctx context.Context, deckID int64) error { return nil }}
			matchmaking := &stubMatchmakingClient{enqueueFunc: func(ctx context.Context, deckID int64, name string, level int64) error { return nil }}
			manager := newTestManager(t, managerDeps{account: account, card: card, matchmaking: matchmaking})
			factory := newTestSocketFactory(t, manager.Hub)
			client, conn := factory.connect(t, "player-1")

			manager.HandleMessage(conn, &WSMessage{Type: wsconst.WSClientMsgMatchmakingStart, Data: matchmakingStartData(t, 1)})

			msg := client.readMessage(t)
			assert.Equal(t, wsconst.WSServerMsgMatchmakingStarted, msg.Type)
		})

		t.Run("受信したメッセージの種別が npc_battle_start のとき、デッキ検証・NPCモデル解決・バトル可能回数確認等を経て応答が返る", func(t *testing.T) {
			account := &stubAccountClient{
				getMeFunc: func(ctx context.Context) (*apiaccount.PlayerResponse, error) {
					return completedOnboardingPlayer("Player One", 5), nil
				},
				getBattleLimitFunc: func(ctx context.Context) (*apiaccount.BattleLimitResponse, error) { return canBattleLimit(), nil },
			}
			card := &stubCardClient{
				validateDeckForBattleFunc: func(ctx context.Context, deckID int64) error { return nil },
				getDeckCardsFunc: func(ctx context.Context, deckID int64) ([]apicard.DeckCard, port.DeckInitiatives, error) {
					return []apicard.DeckCard{{CardID: "card-1", ArtNo: 1, Count: 1}}, port.DeckInitiatives{}, nil
				},
			}
			battle := &stubBattleClient{
				listNpcModelsFunc: func(ctx context.Context) ([]service.NpcModelEntry, error) {
					return []service.NpcModelEntry{{Model: "npc-1", DisplayName: "NPC One"}}, nil
				},
				startNPCBattleFunc: func(ctx context.Context, deckCards []service.BattleDeckCard, deckInitiatives service.DeckInitiatives, npcModel string, player1Summary, player2Summary service.PlayerSummaryRequest) (*service.GameCreatedResult, error) {
					return &service.GameCreatedResult{GameID: "game-1"}, nil
				},
			}
			gamePlayers := &stubGamePlayerRepo{insertGamePlayerFunc: func(ctx context.Context, gameID string, playerNum int, playerID string) error { return nil }}
			manager := newTestManager(t, managerDeps{account: account, card: card, battle: battle, gamePlayers: gamePlayers})
			factory := newTestSocketFactory(t, manager.Hub)
			client, conn := factory.connect(t, "player-1")

			manager.HandleMessage(conn, &WSMessage{Type: wsconst.WSClientMsgNpcBattleStart, Data: npcBattleStartData(t, 1, "npc-1")})

			msg := client.readMessage(t)
			assert.Equal(t, wsconst.WSServerMsgNpcBattleCreated, msg.Type)
		})

		t.Run("受信したメッセージの種別がどれにも一致しないとき、その接続には何の応答も返らない(0件)、接続も維持される", func(t *testing.T) {
			manager := newTestManager(t, managerDeps{})
			factory := newTestSocketFactory(t, manager.Hub)
			client, conn := factory.connect(t, "player-1")

			manager.HandleMessage(conn, &WSMessage{Type: "unknown_type"})

			client.expectNoMessage(t)
			manager.HandleMessage(conn, &WSMessage{Type: wsconst.WSClientMsgPing})
			assert.Equal(t, wsconst.WSServerMsgPong, client.readMessage(t).Type)
		})
	})
}

func TestHandleMatchmakingStart(t *testing.T) {
	t.Run("[マッチメイキング]マッチメイキング開始リクエストの検証", func(t *testing.T) {
		t.Run(`受信したデータが matchmaking_start として解釈できない(不正な形式)とき、error_code が invalid_data のエラーが返る(エラー内容 "invalid matchmaking_start data")`, func(t *testing.T) {
			manager := newTestManager(t, managerDeps{})
			factory := newTestSocketFactory(t, manager.Hub)
			client, conn := factory.connect(t, "player-1")

			manager.HandleMessage(conn, &WSMessage{Type: wsconst.WSClientMsgMatchmakingStart, Data: json.RawMessage(`not-json`)})

			msg := client.readMessage(t)
			assert.Equal(t, wsconst.WSServerMsgError, msg.Type)
			var payload ErrorMessage
			require.NoError(t, json.Unmarshal(msg.Data, &payload))
			assert.Equal(t, "invalid_data", payload.ErrorCode)
			assert.Equal(t, "invalid matchmaking_start data", payload.Message)
		})

		t.Run(`プレイヤー情報の取得自体が失敗するとき、error_code が matchmaking_error のエラーが返る(エラー内容 "failed to fetch player profile: lookup failed")`, func(t *testing.T) {
			account := &stubAccountClient{getMeFunc: func(ctx context.Context) (*apiaccount.PlayerResponse, error) {
				return nil, errors.New("lookup failed")
			}}
			manager := newTestManager(t, managerDeps{account: account})
			factory := newTestSocketFactory(t, manager.Hub)
			client, conn := factory.connect(t, "player-1")

			manager.HandleMessage(conn, &WSMessage{Type: wsconst.WSClientMsgMatchmakingStart, Data: matchmakingStartData(t, 1)})

			msg := client.readMessage(t)
			assert.Equal(t, wsconst.WSServerMsgError, msg.Type)
			var payload ErrorMessage
			require.NoError(t, json.Unmarshal(msg.Data, &payload))
			assert.Equal(t, "matchmaking_error", payload.ErrorCode)
			assert.Equal(t, "failed to fetch player profile: lookup failed", payload.Message)
		})

		t.Run(`プレイヤーのオンボーディングが完了していないとき、error_code が matchmaking_error のエラーが返る(エラー内容 "onboarding not completed")`, func(t *testing.T) {
			account := &stubAccountClient{getMeFunc: func(ctx context.Context) (*apiaccount.PlayerResponse, error) {
				return notStartedOnboardingPlayer(), nil
			}}
			manager := newTestManager(t, managerDeps{account: account})
			factory := newTestSocketFactory(t, manager.Hub)
			client, conn := factory.connect(t, "player-1")

			manager.HandleMessage(conn, &WSMessage{Type: wsconst.WSClientMsgMatchmakingStart, Data: matchmakingStartData(t, 1)})

			msg := client.readMessage(t)
			assert.Equal(t, wsconst.WSServerMsgError, msg.Type)
			var payload ErrorMessage
			require.NoError(t, json.Unmarshal(msg.Data, &payload))
			assert.Equal(t, "matchmaking_error", payload.ErrorCode)
			assert.Equal(t, "onboarding not completed", payload.Message)
		})

		t.Run(`指定したデッキの対戦用としての検証自体が失敗するとき、error_code が matchmaking_error のエラーが返る(エラー内容 "deck validation failed: invalid deck")`, func(t *testing.T) {
			account := &stubAccountClient{getMeFunc: func(ctx context.Context) (*apiaccount.PlayerResponse, error) {
				return completedOnboardingPlayer("Player One", 5), nil
			}}
			card := &stubCardClient{validateDeckForBattleFunc: func(ctx context.Context, deckID int64) error { return errors.New("invalid deck") }}
			manager := newTestManager(t, managerDeps{account: account, card: card})
			factory := newTestSocketFactory(t, manager.Hub)
			client, conn := factory.connect(t, "player-1")

			manager.HandleMessage(conn, &WSMessage{Type: wsconst.WSClientMsgMatchmakingStart, Data: matchmakingStartData(t, 1)})

			msg := client.readMessage(t)
			assert.Equal(t, wsconst.WSServerMsgError, msg.Type)
			var payload ErrorMessage
			require.NoError(t, json.Unmarshal(msg.Data, &payload))
			assert.Equal(t, "matchmaking_error", payload.ErrorCode)
			assert.Equal(t, "deck validation failed: invalid deck", payload.Message)
		})

		t.Run(`本日のバトル可能回数の上限に達しているとき、error_code が matchmaking_error のエラーが返る(エラー内容 "daily battle limit reached")`, func(t *testing.T) {
			account := &stubAccountClient{
				getMeFunc: func(ctx context.Context) (*apiaccount.PlayerResponse, error) {
					return completedOnboardingPlayer("Player One", 5), nil
				},
				getBattleLimitFunc: func(ctx context.Context) (*apiaccount.BattleLimitResponse, error) { return exceededBattleLimit(), nil },
			}
			card := &stubCardClient{validateDeckForBattleFunc: func(ctx context.Context, deckID int64) error { return nil }}
			manager := newTestManager(t, managerDeps{account: account, card: card})
			factory := newTestSocketFactory(t, manager.Hub)
			client, conn := factory.connect(t, "player-1")

			manager.HandleMessage(conn, &WSMessage{Type: wsconst.WSClientMsgMatchmakingStart, Data: matchmakingStartData(t, 1)})

			msg := client.readMessage(t)
			assert.Equal(t, wsconst.WSServerMsgError, msg.Type)
			var payload ErrorMessage
			require.NoError(t, json.Unmarshal(msg.Data, &payload))
			assert.Equal(t, "matchmaking_error", payload.ErrorCode)
			assert.Equal(t, "daily battle limit reached", payload.Message)
		})

		t.Run(`バトル可能回数の確認自体が失敗するとき、error_code が matchmaking_error のエラーが返る(エラー内容 "check failed")`, func(t *testing.T) {
			account := &stubAccountClient{
				getMeFunc: func(ctx context.Context) (*apiaccount.PlayerResponse, error) {
					return completedOnboardingPlayer("Player One", 5), nil
				},
				getBattleLimitFunc: func(ctx context.Context) (*apiaccount.BattleLimitResponse, error) {
					return nil, errors.New("check failed")
				},
			}
			card := &stubCardClient{validateDeckForBattleFunc: func(ctx context.Context, deckID int64) error { return nil }}
			manager := newTestManager(t, managerDeps{account: account, card: card})
			factory := newTestSocketFactory(t, manager.Hub)
			client, conn := factory.connect(t, "player-1")

			manager.HandleMessage(conn, &WSMessage{Type: wsconst.WSClientMsgMatchmakingStart, Data: matchmakingStartData(t, 1)})

			msg := client.readMessage(t)
			assert.Equal(t, wsconst.WSServerMsgError, msg.Type)
			var payload ErrorMessage
			require.NoError(t, json.Unmarshal(msg.Data, &payload))
			assert.Equal(t, "matchmaking_error", payload.ErrorCode)
			assert.Equal(t, "check failed", payload.Message)
		})

		t.Run(`本日のバトル可能回数の上限に達しておらず、可能回数の消費自体が失敗するとき、error_code が matchmaking_error のエラーが返る(エラー内容 "increment failed")`, func(t *testing.T) {
			account := &stubAccountClient{
				getMeFunc: func(ctx context.Context) (*apiaccount.PlayerResponse, error) {
					return completedOnboardingPlayer("Player One", 5), nil
				},
				getBattleLimitFunc:       func(ctx context.Context) (*apiaccount.BattleLimitResponse, error) { return canBattleLimit(), nil },
				incrementBattleCountFunc: func(ctx context.Context) error { return errors.New("increment failed") },
			}
			card := &stubCardClient{validateDeckForBattleFunc: func(ctx context.Context, deckID int64) error { return nil }}
			manager := newTestManager(t, managerDeps{account: account, card: card})
			factory := newTestSocketFactory(t, manager.Hub)
			client, conn := factory.connect(t, "player-1")

			manager.HandleMessage(conn, &WSMessage{Type: wsconst.WSClientMsgMatchmakingStart, Data: matchmakingStartData(t, 1)})

			msg := client.readMessage(t)
			assert.Equal(t, wsconst.WSServerMsgError, msg.Type)
			var payload ErrorMessage
			require.NoError(t, json.Unmarshal(msg.Data, &payload))
			assert.Equal(t, "matchmaking_error", payload.ErrorCode)
			assert.Equal(t, "increment failed", payload.Message)
		})

		t.Run(`マッチメイキングへの登録が失敗するとき、error_code が matchmaking_error のエラーが返る(エラー内容 "failed to enqueue: enqueue failed")`, func(t *testing.T) {
			account := &stubAccountClient{
				getMeFunc: func(ctx context.Context) (*apiaccount.PlayerResponse, error) {
					return completedOnboardingPlayer("Player One", 5), nil
				},
				getBattleLimitFunc: func(ctx context.Context) (*apiaccount.BattleLimitResponse, error) { return canBattleLimit(), nil },
			}
			card := &stubCardClient{validateDeckForBattleFunc: func(ctx context.Context, deckID int64) error { return nil }}
			matchmaking := &stubMatchmakingClient{enqueueFunc: func(ctx context.Context, deckID int64, name string, level int64) error {
				return errors.New("enqueue failed")
			}}
			manager := newTestManager(t, managerDeps{account: account, card: card, matchmaking: matchmaking})
			factory := newTestSocketFactory(t, manager.Hub)
			client, conn := factory.connect(t, "player-1")

			manager.HandleMessage(conn, &WSMessage{Type: wsconst.WSClientMsgMatchmakingStart, Data: matchmakingStartData(t, 1)})

			msg := client.readMessage(t)
			assert.Equal(t, wsconst.WSServerMsgError, msg.Type)
			var payload ErrorMessage
			require.NoError(t, json.Unmarshal(msg.Data, &payload))
			assert.Equal(t, "matchmaking_error", payload.ErrorCode)
			assert.Equal(t, "failed to enqueue: enqueue failed", payload.Message)
		})

		t.Run("全ての確認を通過したとき、その接続に type が matchmaking_started の応答が返り、マッチメイキングの待機タイムアウトが開始される。指定したデッキが検証され、本人のプレイヤー名・レベルでマッチメイキングに登録され、バトル可能回数が消費される", func(t *testing.T) {
			account := &stubAccountClient{
				getMeFunc: func(ctx context.Context) (*apiaccount.PlayerResponse, error) {
					return completedOnboardingPlayer("Player One", 5), nil
				},
				getBattleLimitFunc: func(ctx context.Context) (*apiaccount.BattleLimitResponse, error) { return canBattleLimit(), nil },
			}
			card := &stubCardClient{validateDeckForBattleFunc: func(ctx context.Context, deckID int64) error { return nil }}
			matchmaking := &stubMatchmakingClient{enqueueFunc: func(ctx context.Context, deckID int64, name string, level int64) error { return nil }}
			manager := newTestManager(t, managerDeps{account: account, card: card, matchmaking: matchmaking, matchmakingTimeout: 80 * time.Millisecond})
			factory := newTestSocketFactory(t, manager.Hub)
			client, conn := factory.connect(t, "player-1")

			manager.HandleMessage(conn, &WSMessage{Type: wsconst.WSClientMsgMatchmakingStart, Data: matchmakingStartData(t, 1)})

			msg := client.readMessage(t)
			assert.Equal(t, wsconst.WSServerMsgMatchmakingStarted, msg.Type)
			timeoutMsg := client.readMessage(t)
			assert.Equal(t, wsconst.WSServerMsgError, timeoutMsg.Type)
			assert.Equal(t, []int64{1}, card.validateDeckForBattleCallsSnapshot())
			require.Len(t, matchmaking.enqueueCallsSnapshot(), 1)
			assert.Equal(t, enqueueCall{deckID: 1, name: "Player One", level: 5}, matchmaking.enqueueCallsSnapshot()[0])
			assert.Equal(t, 1, account.incrementBattleCountCallCount())
		})

		t.Run("プレイヤー名が設定されていない(nil)とき、空文字の名前でマッチメイキングに登録される", func(t *testing.T) {
			account := &stubAccountClient{
				getMeFunc: func(ctx context.Context) (*apiaccount.PlayerResponse, error) {
					return &apiaccount.PlayerResponse{OnboardingStatus: apiaccount.OnboardingStatusCompleted, Name: nil, Level: 5}, nil
				},
				getBattleLimitFunc: func(ctx context.Context) (*apiaccount.BattleLimitResponse, error) { return canBattleLimit(), nil },
			}
			card := &stubCardClient{validateDeckForBattleFunc: func(ctx context.Context, deckID int64) error { return nil }}
			matchmaking := &stubMatchmakingClient{enqueueFunc: func(ctx context.Context, deckID int64, name string, level int64) error { return nil }}
			manager := newTestManager(t, managerDeps{account: account, card: card, matchmaking: matchmaking})
			factory := newTestSocketFactory(t, manager.Hub)
			client, conn := factory.connect(t, "player-1")

			manager.HandleMessage(conn, &WSMessage{Type: wsconst.WSClientMsgMatchmakingStart, Data: matchmakingStartData(t, 1)})

			msg := client.readMessage(t)
			assert.Equal(t, wsconst.WSServerMsgMatchmakingStarted, msg.Type)
			require.Len(t, matchmaking.enqueueCallsSnapshot(), 1)
			assert.Equal(t, "", matchmaking.enqueueCallsSnapshot()[0].name)
		})
	})
}

func TestHandleNpcBattleStart(t *testing.T) {
	t.Run("[NPC]NPC対戦開始リクエストの検証", func(t *testing.T) {
		t.Run(`受信したデータが npc_battle_start として解釈できない(不正な形式)とき、error_code が invalid_data のエラーが返る(エラー内容 "invalid npc_battle_start data")`, func(t *testing.T) {
			manager := newTestManager(t, managerDeps{})
			factory := newTestSocketFactory(t, manager.Hub)
			client, conn := factory.connect(t, "player-1")

			manager.HandleMessage(conn, &WSMessage{Type: wsconst.WSClientMsgNpcBattleStart, Data: json.RawMessage(`not-json`)})

			msg := client.readMessage(t)
			assert.Equal(t, wsconst.WSServerMsgError, msg.Type)
			var payload ErrorMessage
			require.NoError(t, json.Unmarshal(msg.Data, &payload))
			assert.Equal(t, "invalid_data", payload.ErrorCode)
			assert.Equal(t, "invalid npc_battle_start data", payload.Message)
		})

		t.Run(`プレイヤー情報の取得自体が失敗するとき、error_code が npc_battle_error のエラーが返る(エラー内容 "failed to fetch player profile: lookup failed")`, func(t *testing.T) {
			account := &stubAccountClient{getMeFunc: func(ctx context.Context) (*apiaccount.PlayerResponse, error) {
				return nil, errors.New("lookup failed")
			}}
			manager := newTestManager(t, managerDeps{account: account})
			factory := newTestSocketFactory(t, manager.Hub)
			client, conn := factory.connect(t, "player-1")

			manager.HandleMessage(conn, &WSMessage{Type: wsconst.WSClientMsgNpcBattleStart, Data: npcBattleStartData(t, 1, "npc-1")})

			msg := client.readMessage(t)
			assert.Equal(t, wsconst.WSServerMsgError, msg.Type)
			var payload ErrorMessage
			require.NoError(t, json.Unmarshal(msg.Data, &payload))
			assert.Equal(t, "npc_battle_error", payload.ErrorCode)
			assert.Equal(t, "failed to fetch player profile: lookup failed", payload.Message)
		})

		t.Run(`プレイヤーのオンボーディングが完了していないとき、error_code が npc_battle_error のエラーが返る(エラー内容 "onboarding not completed")`, func(t *testing.T) {
			account := &stubAccountClient{getMeFunc: func(ctx context.Context) (*apiaccount.PlayerResponse, error) {
				return notStartedOnboardingPlayer(), nil
			}}
			manager := newTestManager(t, managerDeps{account: account})
			factory := newTestSocketFactory(t, manager.Hub)
			client, conn := factory.connect(t, "player-1")

			manager.HandleMessage(conn, &WSMessage{Type: wsconst.WSClientMsgNpcBattleStart, Data: npcBattleStartData(t, 1, "npc-1")})

			msg := client.readMessage(t)
			assert.Equal(t, wsconst.WSServerMsgError, msg.Type)
			var payload ErrorMessage
			require.NoError(t, json.Unmarshal(msg.Data, &payload))
			assert.Equal(t, "npc_battle_error", payload.ErrorCode)
			assert.Equal(t, "onboarding not completed", payload.Message)
		})

		t.Run(`指定したデッキの対戦用としての検証自体が失敗するとき、error_code が npc_battle_error のエラーが返る(エラー内容 "deck validation failed: invalid deck")`, func(t *testing.T) {
			account := &stubAccountClient{getMeFunc: func(ctx context.Context) (*apiaccount.PlayerResponse, error) {
				return completedOnboardingPlayer("Player One", 5), nil
			}}
			card := &stubCardClient{validateDeckForBattleFunc: func(ctx context.Context, deckID int64) error { return errors.New("invalid deck") }}
			manager := newTestManager(t, managerDeps{account: account, card: card})
			factory := newTestSocketFactory(t, manager.Hub)
			client, conn := factory.connect(t, "player-1")

			manager.HandleMessage(conn, &WSMessage{Type: wsconst.WSClientMsgNpcBattleStart, Data: npcBattleStartData(t, 1, "npc-1")})

			msg := client.readMessage(t)
			assert.Equal(t, wsconst.WSServerMsgError, msg.Type)
			var payload ErrorMessage
			require.NoError(t, json.Unmarshal(msg.Data, &payload))
			assert.Equal(t, "npc_battle_error", payload.ErrorCode)
			assert.Equal(t, "deck validation failed: invalid deck", payload.Message)
		})

		t.Run(`デッキ構成の解決自体が失敗するとき、error_code が npc_battle_error のエラーが返る(エラー内容 "failed to resolve deck")`, func(t *testing.T) {
			account := &stubAccountClient{getMeFunc: func(ctx context.Context) (*apiaccount.PlayerResponse, error) {
				return completedOnboardingPlayer("Player One", 5), nil
			}}
			card := &stubCardClient{
				validateDeckForBattleFunc: func(ctx context.Context, deckID int64) error { return nil },
				getDeckCardsFunc: func(ctx context.Context, deckID int64) ([]apicard.DeckCard, port.DeckInitiatives, error) {
					return nil, port.DeckInitiatives{}, errors.New("resolve failed")
				},
			}
			manager := newTestManager(t, managerDeps{account: account, card: card})
			factory := newTestSocketFactory(t, manager.Hub)
			client, conn := factory.connect(t, "player-1")

			manager.HandleMessage(conn, &WSMessage{Type: wsconst.WSClientMsgNpcBattleStart, Data: npcBattleStartData(t, 1, "npc-1")})

			msg := client.readMessage(t)
			assert.Equal(t, wsconst.WSServerMsgError, msg.Type)
			var payload ErrorMessage
			require.NoError(t, json.Unmarshal(msg.Data, &payload))
			assert.Equal(t, "npc_battle_error", payload.ErrorCode)
			assert.Equal(t, "failed to resolve deck", payload.Message)
		})

		t.Run("指定したNPCモデルの解決自体が失敗するとき", func(t *testing.T) {
			cases := []struct {
				name              string
				listNpcModelsFunc func(ctx context.Context) ([]service.NpcModelEntry, error)
				wantMessage       string
			}{
				{
					"候補一覧の取得に失敗した場合、error_code が npc_battle_error のエラーが返る",
					func(ctx context.Context) ([]service.NpcModelEntry, error) { return nil, errors.New("list failed") },
					"failed to resolve npc model: list failed",
				},
				{
					"指定したモデルが候補に含まれない場合、error_code が npc_battle_error のエラーが返る",
					func(ctx context.Context) ([]service.NpcModelEntry, error) {
						return []service.NpcModelEntry{{Model: "other-model", DisplayName: "Other"}}, nil
					},
					"failed to resolve npc model: npc model not found: npc-1",
				},
			}
			for _, tt := range cases {
				t.Run(tt.name, func(t *testing.T) {
					account := &stubAccountClient{getMeFunc: func(ctx context.Context) (*apiaccount.PlayerResponse, error) {
						return completedOnboardingPlayer("Player One", 5), nil
					}}
					card := &stubCardClient{
						validateDeckForBattleFunc: func(ctx context.Context, deckID int64) error { return nil },
						getDeckCardsFunc: func(ctx context.Context, deckID int64) ([]apicard.DeckCard, port.DeckInitiatives, error) {
							return []apicard.DeckCard{{CardID: "card-1", ArtNo: 1, Count: 1}}, port.DeckInitiatives{}, nil
						},
					}
					battle := &stubBattleClient{listNpcModelsFunc: tt.listNpcModelsFunc}
					manager := newTestManager(t, managerDeps{account: account, card: card, battle: battle})
					factory := newTestSocketFactory(t, manager.Hub)
					client, conn := factory.connect(t, "player-1")

					manager.HandleMessage(conn, &WSMessage{Type: wsconst.WSClientMsgNpcBattleStart, Data: npcBattleStartData(t, 1, "npc-1")})

					msg := client.readMessage(t)
					assert.Equal(t, wsconst.WSServerMsgError, msg.Type)
					var payload ErrorMessage
					require.NoError(t, json.Unmarshal(msg.Data, &payload))
					assert.Equal(t, "npc_battle_error", payload.ErrorCode)
					assert.Equal(t, tt.wantMessage, payload.Message)
				})
			}
		})

		t.Run(`本日のバトル可能回数の上限に達しているとき、error_code が npc_battle_error のエラーが返る(エラー内容 "daily battle limit reached")`, func(t *testing.T) {
			account := &stubAccountClient{
				getMeFunc: func(ctx context.Context) (*apiaccount.PlayerResponse, error) {
					return completedOnboardingPlayer("Player One", 5), nil
				},
				getBattleLimitFunc: func(ctx context.Context) (*apiaccount.BattleLimitResponse, error) { return exceededBattleLimit(), nil },
			}
			card := &stubCardClient{
				validateDeckForBattleFunc: func(ctx context.Context, deckID int64) error { return nil },
				getDeckCardsFunc: func(ctx context.Context, deckID int64) ([]apicard.DeckCard, port.DeckInitiatives, error) {
					return []apicard.DeckCard{{CardID: "card-1", ArtNo: 1, Count: 1}}, port.DeckInitiatives{}, nil
				},
			}
			battle := &stubBattleClient{listNpcModelsFunc: func(ctx context.Context) ([]service.NpcModelEntry, error) {
				return []service.NpcModelEntry{{Model: "npc-1", DisplayName: "NPC One"}}, nil
			}}
			manager := newTestManager(t, managerDeps{account: account, card: card, battle: battle})
			factory := newTestSocketFactory(t, manager.Hub)
			client, conn := factory.connect(t, "player-1")

			manager.HandleMessage(conn, &WSMessage{Type: wsconst.WSClientMsgNpcBattleStart, Data: npcBattleStartData(t, 1, "npc-1")})

			msg := client.readMessage(t)
			assert.Equal(t, wsconst.WSServerMsgError, msg.Type)
			var payload ErrorMessage
			require.NoError(t, json.Unmarshal(msg.Data, &payload))
			assert.Equal(t, "npc_battle_error", payload.ErrorCode)
			assert.Equal(t, "daily battle limit reached", payload.Message)
		})

		t.Run(`バトル可能回数の確認自体が失敗するとき、error_code が npc_battle_error のエラーが返る(エラー内容 "check failed")`, func(t *testing.T) {
			account := &stubAccountClient{
				getMeFunc: func(ctx context.Context) (*apiaccount.PlayerResponse, error) {
					return completedOnboardingPlayer("Player One", 5), nil
				},
				getBattleLimitFunc: func(ctx context.Context) (*apiaccount.BattleLimitResponse, error) {
					return nil, errors.New("check failed")
				},
			}
			card := &stubCardClient{
				validateDeckForBattleFunc: func(ctx context.Context, deckID int64) error { return nil },
				getDeckCardsFunc: func(ctx context.Context, deckID int64) ([]apicard.DeckCard, port.DeckInitiatives, error) {
					return []apicard.DeckCard{{CardID: "card-1", ArtNo: 1, Count: 1}}, port.DeckInitiatives{}, nil
				},
			}
			battle := &stubBattleClient{listNpcModelsFunc: func(ctx context.Context) ([]service.NpcModelEntry, error) {
				return []service.NpcModelEntry{{Model: "npc-1", DisplayName: "NPC One"}}, nil
			}}
			manager := newTestManager(t, managerDeps{account: account, card: card, battle: battle})
			factory := newTestSocketFactory(t, manager.Hub)
			client, conn := factory.connect(t, "player-1")

			manager.HandleMessage(conn, &WSMessage{Type: wsconst.WSClientMsgNpcBattleStart, Data: npcBattleStartData(t, 1, "npc-1")})

			msg := client.readMessage(t)
			assert.Equal(t, wsconst.WSServerMsgError, msg.Type)
			var payload ErrorMessage
			require.NoError(t, json.Unmarshal(msg.Data, &payload))
			assert.Equal(t, "npc_battle_error", payload.ErrorCode)
			assert.Equal(t, "check failed", payload.Message)
		})

		t.Run(`本日のバトル可能回数の上限に達しておらず、可能回数の消費自体が失敗するとき、error_code が npc_battle_error のエラーが返る(エラー内容 "increment failed")`, func(t *testing.T) {
			account := &stubAccountClient{
				getMeFunc: func(ctx context.Context) (*apiaccount.PlayerResponse, error) {
					return completedOnboardingPlayer("Player One", 5), nil
				},
				getBattleLimitFunc:       func(ctx context.Context) (*apiaccount.BattleLimitResponse, error) { return canBattleLimit(), nil },
				incrementBattleCountFunc: func(ctx context.Context) error { return errors.New("increment failed") },
			}
			card := &stubCardClient{
				validateDeckForBattleFunc: func(ctx context.Context, deckID int64) error { return nil },
				getDeckCardsFunc: func(ctx context.Context, deckID int64) ([]apicard.DeckCard, port.DeckInitiatives, error) {
					return []apicard.DeckCard{{CardID: "card-1", ArtNo: 1, Count: 1}}, port.DeckInitiatives{}, nil
				},
			}
			battle := &stubBattleClient{listNpcModelsFunc: func(ctx context.Context) ([]service.NpcModelEntry, error) {
				return []service.NpcModelEntry{{Model: "npc-1", DisplayName: "NPC One"}}, nil
			}}
			manager := newTestManager(t, managerDeps{account: account, card: card, battle: battle})
			factory := newTestSocketFactory(t, manager.Hub)
			client, conn := factory.connect(t, "player-1")

			manager.HandleMessage(conn, &WSMessage{Type: wsconst.WSClientMsgNpcBattleStart, Data: npcBattleStartData(t, 1, "npc-1")})

			msg := client.readMessage(t)
			assert.Equal(t, wsconst.WSServerMsgError, msg.Type)
			var payload ErrorMessage
			require.NoError(t, json.Unmarshal(msg.Data, &payload))
			assert.Equal(t, "npc_battle_error", payload.ErrorCode)
			assert.Equal(t, "increment failed", payload.Message)
		})

		t.Run(`対戦の作成自体が失敗するとき、error_code が npc_battle_error のエラーが返る(エラー内容 "create failed")`, func(t *testing.T) {
			account := &stubAccountClient{
				getMeFunc: func(ctx context.Context) (*apiaccount.PlayerResponse, error) {
					return completedOnboardingPlayer("Player One", 5), nil
				},
				getBattleLimitFunc: func(ctx context.Context) (*apiaccount.BattleLimitResponse, error) { return canBattleLimit(), nil },
			}
			card := &stubCardClient{
				validateDeckForBattleFunc: func(ctx context.Context, deckID int64) error { return nil },
				getDeckCardsFunc: func(ctx context.Context, deckID int64) ([]apicard.DeckCard, port.DeckInitiatives, error) {
					return []apicard.DeckCard{{CardID: "card-1", ArtNo: 1, Count: 1}}, port.DeckInitiatives{}, nil
				},
			}
			battle := &stubBattleClient{
				listNpcModelsFunc: func(ctx context.Context) ([]service.NpcModelEntry, error) {
					return []service.NpcModelEntry{{Model: "npc-1", DisplayName: "NPC One"}}, nil
				},
				startNPCBattleFunc: func(ctx context.Context, deckCards []service.BattleDeckCard, deckInitiatives service.DeckInitiatives, npcModel string, player1Summary, player2Summary service.PlayerSummaryRequest) (*service.GameCreatedResult, error) {
					return nil, errors.New("create failed")
				},
			}
			manager := newTestManager(t, managerDeps{account: account, card: card, battle: battle})
			factory := newTestSocketFactory(t, manager.Hub)
			client, conn := factory.connect(t, "player-1")

			manager.HandleMessage(conn, &WSMessage{Type: wsconst.WSClientMsgNpcBattleStart, Data: npcBattleStartData(t, 1, "npc-1")})

			msg := client.readMessage(t)
			assert.Equal(t, wsconst.WSServerMsgError, msg.Type)
			var payload ErrorMessage
			require.NoError(t, json.Unmarshal(msg.Data, &payload))
			assert.Equal(t, "npc_battle_error", payload.ErrorCode)
			assert.Equal(t, "create failed", payload.Message)
		})

		t.Run("対戦の作成に成功したとき、その接続に type が npc_battle_created、作成された対戦の識別子を含む応答が返る。対戦成立の記録保存が別途失敗した場合でも、この成功応答は変わらず返る。指定したデッキが検証され、本人が1人目のプレイヤーとして記録され、バトル可能回数が消費される", func(t *testing.T) {
			account := &stubAccountClient{
				getMeFunc: func(ctx context.Context) (*apiaccount.PlayerResponse, error) {
					return completedOnboardingPlayer("Player One", 5), nil
				},
				getBattleLimitFunc: func(ctx context.Context) (*apiaccount.BattleLimitResponse, error) { return canBattleLimit(), nil },
			}
			card := &stubCardClient{
				validateDeckForBattleFunc: func(ctx context.Context, deckID int64) error { return nil },
				getDeckCardsFunc: func(ctx context.Context, deckID int64) ([]apicard.DeckCard, port.DeckInitiatives, error) {
					return []apicard.DeckCard{{CardID: "card-1", ArtNo: 1, Count: 1}}, port.DeckInitiatives{}, nil
				},
			}
			battle := &stubBattleClient{
				listNpcModelsFunc: func(ctx context.Context) ([]service.NpcModelEntry, error) {
					return []service.NpcModelEntry{{Model: "npc-1", DisplayName: "NPC One"}}, nil
				},
				startNPCBattleFunc: func(ctx context.Context, deckCards []service.BattleDeckCard, deckInitiatives service.DeckInitiatives, npcModel string, player1Summary, player2Summary service.PlayerSummaryRequest) (*service.GameCreatedResult, error) {
					return &service.GameCreatedResult{GameID: "game-1"}, nil
				},
			}
			gamePlayers := &stubGamePlayerRepo{insertGamePlayerFunc: func(ctx context.Context, gameID string, playerNum int, playerID string) error {
				return errors.New("insert failed")
			}}
			manager := newTestManager(t, managerDeps{account: account, card: card, battle: battle, gamePlayers: gamePlayers})
			factory := newTestSocketFactory(t, manager.Hub)
			client, conn := factory.connect(t, "player-1")

			manager.HandleMessage(conn, &WSMessage{Type: wsconst.WSClientMsgNpcBattleStart, Data: npcBattleStartData(t, 1, "npc-1")})

			msg := client.readMessage(t)
			assert.Equal(t, wsconst.WSServerMsgNpcBattleCreated, msg.Type)
			var payload NPCBattleCreatedMessage
			require.NoError(t, json.Unmarshal(msg.Data, &payload))
			assert.Equal(t, "game-1", payload.GameID)
			assert.Equal(t, []int64{1}, card.validateDeckForBattleCallsSnapshot())
			assert.Equal(t, 1, account.incrementBattleCountCallCount())
			require.Len(t, gamePlayers.insertGamePlayerCallsSnapshot(), 1)
			assert.Equal(t, insertGamePlayerCall{gameID: "game-1", playerNum: 1, playerID: "player-1"}, gamePlayers.insertGamePlayerCallsSnapshot()[0])
		})

		t.Run("プレイヤー名が設定されていない(nil)とき、空文字の名前でNPC対戦が作成される", func(t *testing.T) {
			account := &stubAccountClient{
				getMeFunc: func(ctx context.Context) (*apiaccount.PlayerResponse, error) {
					return &apiaccount.PlayerResponse{OnboardingStatus: apiaccount.OnboardingStatusCompleted, Name: nil, Level: 5}, nil
				},
				getBattleLimitFunc: func(ctx context.Context) (*apiaccount.BattleLimitResponse, error) { return canBattleLimit(), nil },
			}
			card := &stubCardClient{
				validateDeckForBattleFunc: func(ctx context.Context, deckID int64) error { return nil },
				getDeckCardsFunc: func(ctx context.Context, deckID int64) ([]apicard.DeckCard, port.DeckInitiatives, error) {
					return []apicard.DeckCard{{CardID: "card-1", ArtNo: 1, Count: 1}}, port.DeckInitiatives{}, nil
				},
			}
			battle := &stubBattleClient{
				listNpcModelsFunc: func(ctx context.Context) ([]service.NpcModelEntry, error) {
					return []service.NpcModelEntry{{Model: "npc-1", DisplayName: "NPC One"}}, nil
				},
				startNPCBattleFunc: func(ctx context.Context, deckCards []service.BattleDeckCard, deckInitiatives service.DeckInitiatives, npcModel string, player1Summary, player2Summary service.PlayerSummaryRequest) (*service.GameCreatedResult, error) {
					return &service.GameCreatedResult{GameID: "game-1"}, nil
				},
			}
			gamePlayers := &stubGamePlayerRepo{insertGamePlayerFunc: func(ctx context.Context, gameID string, playerNum int, playerID string) error { return nil }}
			manager := newTestManager(t, managerDeps{account: account, card: card, battle: battle, gamePlayers: gamePlayers})
			factory := newTestSocketFactory(t, manager.Hub)
			client, conn := factory.connect(t, "player-1")

			manager.HandleMessage(conn, &WSMessage{Type: wsconst.WSClientMsgNpcBattleStart, Data: npcBattleStartData(t, 1, "npc-1")})

			msg := client.readMessage(t)
			assert.Equal(t, wsconst.WSServerMsgNpcBattleCreated, msg.Type)
			require.Len(t, battle.startNPCBattleCallsSnapshot(), 1)
			assert.Equal(t, "", battle.startNPCBattleCallsSnapshot()[0].player1Summary.Name)
		})
	})
}

func TestDeckCardsToBattleDeckCards(t *testing.T) {
	t.Run("[ゲーム参加]デッキ構成からバトル用カードリストへの変換", func(t *testing.T) {
		t.Run("デッキに含まれるカード種別の枚数が1枚のとき、変換後のリストにそのカードが1件含まれる", func(t *testing.T) {
			card := &stubCardClient{getDeckCardsFunc: func(ctx context.Context, deckID int64) ([]apicard.DeckCard, port.DeckInitiatives, error) {
				return []apicard.DeckCard{{CardID: "card-1", ArtNo: 1, Count: 1}}, port.DeckInitiatives{}, nil
			}}
			manager := newTestManager(t, managerDeps{card: card})

			cards, _, err := manager.resolveDeckCards(context.Background(), "player-1", 1)

			require.NoError(t, err)
			assert.Equal(t, []service.BattleDeckCard{{CardID: "card-1", ArtNo: 1}}, cards)
		})

		t.Run("デッキに含まれるカード種別の枚数が複数枚(例: 3枚)のとき、変換後のリストにそのカードが枚数分だけ繰り返し含まれる", func(t *testing.T) {
			card := &stubCardClient{getDeckCardsFunc: func(ctx context.Context, deckID int64) ([]apicard.DeckCard, port.DeckInitiatives, error) {
				return []apicard.DeckCard{{CardID: "card-1", ArtNo: 1, Count: 3}}, port.DeckInitiatives{}, nil
			}}
			manager := newTestManager(t, managerDeps{card: card})

			cards, _, err := manager.resolveDeckCards(context.Background(), "player-1", 1)

			require.NoError(t, err)
			assert.Equal(t, []service.BattleDeckCard{{CardID: "card-1", ArtNo: 1}, {CardID: "card-1", ArtNo: 1}, {CardID: "card-1", ArtNo: 1}}, cards)
		})

		t.Run("デッキに複数のカード種別が含まれるとき、それぞれの枚数分がすべて反映される", func(t *testing.T) {
			card := &stubCardClient{getDeckCardsFunc: func(ctx context.Context, deckID int64) ([]apicard.DeckCard, port.DeckInitiatives, error) {
				return []apicard.DeckCard{
					{CardID: "card-1", ArtNo: 1, Count: 2},
					{CardID: "card-2", ArtNo: 2, Count: 1},
				}, port.DeckInitiatives{}, nil
			}}
			manager := newTestManager(t, managerDeps{card: card})

			cards, _, err := manager.resolveDeckCards(context.Background(), "player-1", 1)

			require.NoError(t, err)
			assert.Equal(t, []service.BattleDeckCard{
				{CardID: "card-1", ArtNo: 1},
				{CardID: "card-1", ArtNo: 1},
				{CardID: "card-2", ArtNo: 2},
			}, cards)
		})

		t.Run("デッキにカードが1件も含まれない(0件)とき、変換後のリストは空になる", func(t *testing.T) {
			card := &stubCardClient{getDeckCardsFunc: func(ctx context.Context, deckID int64) ([]apicard.DeckCard, port.DeckInitiatives, error) {
				return []apicard.DeckCard{}, port.DeckInitiatives{}, nil
			}}
			manager := newTestManager(t, managerDeps{card: card})

			cards, _, err := manager.resolveDeckCards(context.Background(), "player-1", 1)

			require.NoError(t, err)
			assert.Empty(t, cards)
		})
	})

	t.Run("[ゲーム参加]デッキ解決時の施策IDの転記", func(t *testing.T) {
		t.Run("デッキ内容にルーチン施策IDが設定されているとき、解決された対戦転送用のルーチン施策IDはデッキ内容の値と一致する", func(t *testing.T) {
			card := &stubCardClient{getDeckCardsFunc: func(ctx context.Context, deckID int64) ([]apicard.DeckCard, port.DeckInitiatives, error) {
				return []apicard.DeckCard{}, port.DeckInitiatives{RoutineID: "routine-1", SpecialID: "special-1"}, nil
			}}
			manager := newTestManager(t, managerDeps{card: card})

			_, initiatives, err := manager.resolveDeckCards(context.Background(), "player-1", 1)

			require.NoError(t, err)
			assert.Equal(t, "routine-1", initiatives.RoutineID)
		})

		t.Run("デッキ内容にスペシャル施策IDが設定されているとき、解決された対戦転送用のスペシャル施策IDはデッキ内容の値と一致する", func(t *testing.T) {
			card := &stubCardClient{getDeckCardsFunc: func(ctx context.Context, deckID int64) ([]apicard.DeckCard, port.DeckInitiatives, error) {
				return []apicard.DeckCard{}, port.DeckInitiatives{RoutineID: "routine-1", SpecialID: "special-1"}, nil
			}}
			manager := newTestManager(t, managerDeps{card: card})

			_, initiatives, err := manager.resolveDeckCards(context.Background(), "player-1", 1)

			require.NoError(t, err)
			assert.Equal(t, "special-1", initiatives.SpecialID)
		})
	})
}

func TestResolveNpcDisplayName(t *testing.T) {
	t.Run("[NPC]NPCモデル名から表示名を解決する", func(t *testing.T) {
		t.Run("候補となるNPCモデルが1件も無い(0件)とき、指定したモデルは解決できず、モデルが見つからない旨のエラーになる", func(t *testing.T) {
			battle := &stubBattleClient{listNpcModelsFunc: func(ctx context.Context) ([]service.NpcModelEntry, error) { return []service.NpcModelEntry{}, nil }}
			manager := newTestManager(t, managerDeps{battle: battle})

			_, err := manager.resolveNpcDisplayName(context.Background(), "npc-1")

			assert.Error(t, err)
		})

		t.Run("候補の中に指定したモデルが含まれるとき、そのモデルの表示名が解決される", func(t *testing.T) {
			battle := &stubBattleClient{listNpcModelsFunc: func(ctx context.Context) ([]service.NpcModelEntry, error) {
				return []service.NpcModelEntry{{Model: "npc-1", DisplayName: "NPC One"}, {Model: "npc-2", DisplayName: "NPC Two"}}, nil
			}}
			manager := newTestManager(t, managerDeps{battle: battle})

			name, err := manager.resolveNpcDisplayName(context.Background(), "npc-2")

			require.NoError(t, err)
			assert.Equal(t, "NPC Two", name)
		})

		t.Run("候補は複数あるが指定したモデルがその中に含まれないとき、指定したモデルは解決できず、モデルが見つからない旨のエラーになる", func(t *testing.T) {
			battle := &stubBattleClient{listNpcModelsFunc: func(ctx context.Context) ([]service.NpcModelEntry, error) {
				return []service.NpcModelEntry{{Model: "npc-1", DisplayName: "NPC One"}, {Model: "npc-2", DisplayName: "NPC Two"}}, nil
			}}
			manager := newTestManager(t, managerDeps{battle: battle})

			_, err := manager.resolveNpcDisplayName(context.Background(), "npc-unknown")

			assert.Error(t, err)
		})
	})
}

func TestMatchWaitTimeout(t *testing.T) {
	t.Run("[マッチメイキング]マッチメイキング待機タイムアウトの管理", func(t *testing.T) {
		t.Run("待機タイムアウトの時間が0以下に設定されている(機能が無効化されている)とき、待機を開始してもタイムアウトは発生しない", func(t *testing.T) {
			manager := NewManager(&stubBattleClient{}, &stubAccountClient{}, &stubCardClient{}, &stubMatchmakingClient{}, &stubGamePlayerRepo{}, &stubProcessedMatchRepo{}, &stubInvalidatedGameRepo{}, 0, newTestInternalSigner(t), &stubTimerStore{}, DefaultDisconnectTimeout)
			factory := newTestSocketFactory(t, manager.Hub)
			client, _ := factory.connect(t, "player-1")

			manager.startMatchWait("player-1")

			client.expectNoMessage(t)
		})

		t.Run(`待機タイムアウトの時間が正の値のとき、開始してから当該時間が経過してもマッチが成立せずキャンセルもされない場合、その接続に error_code が matchmaking_error のエラーが返り(エラー内容 "matchmaking timed out")、上流のマッチメイキングもキャンセルされる`, func(t *testing.T) {
			matchmaking := &stubMatchmakingClient{}
			manager := newTestManager(t, managerDeps{matchmaking: matchmaking, matchmakingTimeout: 80 * time.Millisecond})
			factory := newTestSocketFactory(t, manager.Hub)
			client, _ := factory.connect(t, "player-1")

			manager.startMatchWait("player-1")

			msg := client.readMessage(t)
			assert.Equal(t, wsconst.WSServerMsgError, msg.Type)
			var payload ErrorMessage
			require.NoError(t, json.Unmarshal(msg.Data, &payload))
			assert.Equal(t, "matchmaking_error", payload.ErrorCode)
			assert.Equal(t, "matchmaking timed out", payload.Message)
			assert.Equal(t, 1, matchmaking.cancelCallCount())
		})

		t.Run("待機が開始された後、時間経過前にマッチが成立するかキャンセルされるとき、タイムアウトによるエラーは発生しない", func(t *testing.T) {
			manager := newTestManager(t, managerDeps{matchmakingTimeout: 300 * time.Millisecond})
			factory := newTestSocketFactory(t, manager.Hub)
			client, _ := factory.connect(t, "player-1")

			manager.startMatchWait("player-1")
			manager.stopMatchWait("player-1")

			client.expectNoMessage(t)
		})

		t.Run("同一プレイヤーに対して待機を開始し直したとき、以前の待機は無効化され、新しい待機のみが有効になる(タイムアウトは1回だけ発生する)", func(t *testing.T) {
			manager := newTestManager(t, managerDeps{matchmakingTimeout: 150 * time.Millisecond})
			factory := newTestSocketFactory(t, manager.Hub)
			client, _ := factory.connect(t, "player-1")

			manager.startMatchWait("player-1")
			time.Sleep(80 * time.Millisecond)
			manager.startMatchWait("player-1")

			msg := client.readMessage(t)
			assert.Equal(t, wsconst.WSServerMsgError, msg.Type)
			client.expectNoMessage(t)
		})
	})
}

func TestHandleMatchMadeParticipantCount(t *testing.T) {
	t.Run("[マッチメイキング]マッチ成立イベントの参加人数検証", func(t *testing.T) {
		cases := []struct {
			name    string
			players []apimatchmaking.MatchedPlayer
		}{
			{"0人のとき", []apimatchmaking.MatchedPlayer{}},
			{"1人のとき", []apimatchmaking.MatchedPlayer{{PlayerID: "player-1", DeckID: 1, Name: "Player One", Level: 1}}},
			{"3人以上のとき", []apimatchmaking.MatchedPlayer{
				{PlayerID: "player-1", DeckID: 1, Name: "Player One", Level: 1},
				{PlayerID: "player-2", DeckID: 2, Name: "Player Two", Level: 1},
				{PlayerID: "player-3", DeckID: 3, Name: "Player Three", Level: 1},
			}},
		}
		for _, tt := range cases {
			t.Run("成立イベントに含まれるプレイヤーが"+tt.name, func(t *testing.T) {
				manager := newTestManager(t, managerDeps{})

				err := manager.HandleMatchMade(context.Background(), apimatchmaking.MatchMadeEvent{MatchID: "match-1", Players: tt.players})

				assert.EqualError(t, err, "match_made event must contain exactly 2 players")
			})
		}
	})
}

func twoPlayerMatchMadeEvent(matchID string) apimatchmaking.MatchMadeEvent {
	return apimatchmaking.MatchMadeEvent{
		MatchID: matchID,
		Players: []apimatchmaking.MatchedPlayer{
			{PlayerID: "player-1", DeckID: 1, Name: "Player One", Level: 3},
			{PlayerID: "player-2", DeckID: 2, Name: "Player Two", Level: 4},
		},
	}
}

func deckResolvableCardStub() *stubCardClient {
	return &stubCardClient{getDeckCardsFunc: func(ctx context.Context, deckID int64) ([]apicard.DeckCard, port.DeckInitiatives, error) {
		return []apicard.DeckCard{{CardID: "card-1", ArtNo: 1, Count: 1}}, port.DeckInitiatives{}, nil
	}}
}

func perPlayerDeckCardStub() *stubCardClient {
	return &stubCardClient{getDeckCardsFunc: func(ctx context.Context, deckID int64) ([]apicard.DeckCard, port.DeckInitiatives, error) {
		if deckID == 1 {
			return []apicard.DeckCard{{CardID: "card-p1", ArtNo: 11, Count: 1}}, port.DeckInitiatives{}, nil
		}
		return []apicard.DeckCard{{CardID: "card-p2", ArtNo: 22, Count: 1}}, port.DeckInitiatives{}, nil
	}}
}

func TestHandleMatchMadeBattleCreationRequest(t *testing.T) {
	t.Run("[マッチメイキング]対戦作成要求の組み立て", func(t *testing.T) {
		t.Run("プレイヤー1側に渡るデッキ内容は、マッチング成立イベントのプレイヤー1のデッキIDから解決された内容と一致する", func(t *testing.T) {
			processedMatch := &stubProcessedMatchRepo{
				claimFunc:             func(ctx context.Context, matchID string) (bool, error) { return true, nil },
				recordGameCreatedFunc: func(ctx context.Context, matchID, gameID string) error { return nil },
				markNotifiedFunc:      func(ctx context.Context, matchID string) (bool, error) { return true, nil },
			}
			card := perPlayerDeckCardStub()
			battle := &stubBattleClient{createPvPGameFunc: func(ctx context.Context, deck1Cards, deck2Cards []service.BattleDeckCard, deck1Initiatives, deck2Initiatives service.DeckInitiatives, player1Summary, player2Summary service.PlayerSummaryRequest) (*service.GameCreatedResult, error) {
				return &service.GameCreatedResult{GameID: "game-1"}, nil
			}}
			gamePlayers := &stubGamePlayerRepo{insertGamePlayerFunc: func(ctx context.Context, gameID string, playerNum int, playerID string) error { return nil }}
			manager := newTestManager(t, managerDeps{processedMatch: processedMatch, card: card, battle: battle, gamePlayers: gamePlayers})

			err := manager.HandleMatchMade(context.Background(), twoPlayerMatchMadeEvent("match-1"))

			require.NoError(t, err)
			require.Len(t, battle.createPvPGameCallsSnapshot(), 1)
			assert.Equal(t, []service.BattleDeckCard{{CardID: "card-p1", ArtNo: 11}}, battle.createPvPGameCallsSnapshot()[0].deck1Cards)
		})

		t.Run("プレイヤー2側に渡るデッキ内容は、マッチング成立イベントのプレイヤー2のデッキIDから解決された内容と一致する", func(t *testing.T) {
			processedMatch := &stubProcessedMatchRepo{
				claimFunc:             func(ctx context.Context, matchID string) (bool, error) { return true, nil },
				recordGameCreatedFunc: func(ctx context.Context, matchID, gameID string) error { return nil },
				markNotifiedFunc:      func(ctx context.Context, matchID string) (bool, error) { return true, nil },
			}
			card := perPlayerDeckCardStub()
			battle := &stubBattleClient{createPvPGameFunc: func(ctx context.Context, deck1Cards, deck2Cards []service.BattleDeckCard, deck1Initiatives, deck2Initiatives service.DeckInitiatives, player1Summary, player2Summary service.PlayerSummaryRequest) (*service.GameCreatedResult, error) {
				return &service.GameCreatedResult{GameID: "game-1"}, nil
			}}
			gamePlayers := &stubGamePlayerRepo{insertGamePlayerFunc: func(ctx context.Context, gameID string, playerNum int, playerID string) error { return nil }}
			manager := newTestManager(t, managerDeps{processedMatch: processedMatch, card: card, battle: battle, gamePlayers: gamePlayers})

			err := manager.HandleMatchMade(context.Background(), twoPlayerMatchMadeEvent("match-1"))

			require.NoError(t, err)
			require.Len(t, battle.createPvPGameCallsSnapshot(), 1)
			assert.Equal(t, []service.BattleDeckCard{{CardID: "card-p2", ArtNo: 22}}, battle.createPvPGameCallsSnapshot()[0].deck2Cards)
		})
	})
}

func TestHandleMatchMadeDeduplication(t *testing.T) {
	t.Run("[マッチメイキング]マッチ成立イベントの重複・競合処理", func(t *testing.T) {
		t.Run("マッチング成立イベントの処理開始の記録が失敗したとき、エラーが返る", func(t *testing.T) {
			processedMatch := &stubProcessedMatchRepo{
				claimFunc: func(ctx context.Context, matchID string) (bool, error) { return false, errors.New("claim failed") },
			}
			manager := newTestManager(t, managerDeps{processedMatch: processedMatch})

			err := manager.HandleMatchMade(context.Background(), twoPlayerMatchMadeEvent("match-1"))

			assert.ErrorContains(t, err, "claim failed")
		})

		t.Run("同一のマッチング成立イベントについて処理開始の記録が既に済んでいるが、対応する対戦IDの記録参照が失敗したとき、エラーが返る", func(t *testing.T) {
			processedMatch := &stubProcessedMatchRepo{
				claimFunc: func(ctx context.Context, matchID string) (bool, error) { return false, nil },
				gameIDForFunc: func(ctx context.Context, matchID string) (string, bool, error) {
					return "", false, errors.New("game id lookup failed")
				},
			}
			manager := newTestManager(t, managerDeps{processedMatch: processedMatch})

			err := manager.HandleMatchMade(context.Background(), twoPlayerMatchMadeEvent("match-1"))

			assert.ErrorContains(t, err, "game id lookup failed")
		})

		t.Run("同一の成立イベントが、既に通知済みの状態で再度届いたとき、通知は再送されず、イベントは処理済みとして扱われる", func(t *testing.T) {
			processedMatch := &stubProcessedMatchRepo{
				claimFunc:        func(ctx context.Context, matchID string) (bool, error) { return false, nil },
				gameIDForFunc:    func(ctx context.Context, matchID string) (string, bool, error) { return "game-1", true, nil },
				markNotifiedFunc: func(ctx context.Context, matchID string) (bool, error) { return false, nil },
			}
			manager := newTestManager(t, managerDeps{processedMatch: processedMatch})
			factory := newTestSocketFactory(t, manager.Hub)
			client1, _ := factory.connect(t, "player-1")
			client2, _ := factory.connect(t, "player-2")

			err := manager.HandleMatchMade(context.Background(), twoPlayerMatchMadeEvent("match-1"))

			require.NoError(t, err)
			client1.expectNoMessage(t)
			client2.expectNoMessage(t)
		})

		t.Run("同一の成立イベントが、対戦の作成はまだ記録されていない競合中の状態で再度届いたとき、通知は行われず、イベントは処理済みとして扱われる", func(t *testing.T) {
			processedMatch := &stubProcessedMatchRepo{
				claimFunc:     func(ctx context.Context, matchID string) (bool, error) { return false, nil },
				gameIDForFunc: func(ctx context.Context, matchID string) (string, bool, error) { return "", false, nil },
			}
			manager := newTestManager(t, managerDeps{processedMatch: processedMatch})
			factory := newTestSocketFactory(t, manager.Hub)
			client1, _ := factory.connect(t, "player-1")
			client2, _ := factory.connect(t, "player-2")

			err := manager.HandleMatchMade(context.Background(), twoPlayerMatchMadeEvent("match-1"))

			require.NoError(t, err)
			client1.expectNoMessage(t)
			client2.expectNoMessage(t)
		})

		t.Run("対戦の作成自体が失敗したとき、今回の処理は失敗として扱われ、後で同じ成立イベントが再度届いた場合に対戦を作成し直せる状態になる", func(t *testing.T) {
			var releasedMatchIDs []string
			processedMatch := &stubProcessedMatchRepo{
				claimFunc: func(ctx context.Context, matchID string) (bool, error) { return true, nil },
				releaseFunc: func(ctx context.Context, matchID string) error {
					releasedMatchIDs = append(releasedMatchIDs, matchID)
					return nil
				},
			}
			card := deckResolvableCardStub()
			battle := &stubBattleClient{createPvPGameFunc: func(ctx context.Context, deck1Cards, deck2Cards []service.BattleDeckCard, deck1Initiatives, deck2Initiatives service.DeckInitiatives, player1Summary, player2Summary service.PlayerSummaryRequest) (*service.GameCreatedResult, error) {
				return nil, errors.New("create failed")
			}}
			manager := newTestManager(t, managerDeps{processedMatch: processedMatch, card: card, battle: battle})

			err := manager.HandleMatchMade(context.Background(), twoPlayerMatchMadeEvent("match-1"))

			assert.EqualError(t, err, "create failed")
			assert.Equal(t, []string{"match-1"}, releasedMatchIDs)
		})

		t.Run("対戦の作成には成功したがその記録が失敗したとき、今回の処理は失敗として扱われ、後で同じ成立イベントが再度届いても対戦は作成し直されない状態になる", func(t *testing.T) {
			var releaseCalls int
			processedMatch := &stubProcessedMatchRepo{
				claimFunc: func(ctx context.Context, matchID string) (bool, error) { return true, nil },
				releaseFunc: func(ctx context.Context, matchID string) error {
					releaseCalls++
					return nil
				},
				recordGameCreatedFunc: func(ctx context.Context, matchID, gameID string) error { return errors.New("record failed") },
			}
			card := deckResolvableCardStub()
			battle := &stubBattleClient{createPvPGameFunc: func(ctx context.Context, deck1Cards, deck2Cards []service.BattleDeckCard, deck1Initiatives, deck2Initiatives service.DeckInitiatives, player1Summary, player2Summary service.PlayerSummaryRequest) (*service.GameCreatedResult, error) {
				return &service.GameCreatedResult{GameID: "game-1"}, nil
			}}
			manager := newTestManager(t, managerDeps{processedMatch: processedMatch, card: card, battle: battle})

			err := manager.HandleMatchMade(context.Background(), twoPlayerMatchMadeEvent("match-1"))

			assert.EqualError(t, err, "match_made: record game game-1 for matchId match-1: record failed")
			assert.Zero(t, releaseCalls)
		})

		t.Run("参加者記録の保存が失敗したとき、処理は失敗として扱われ、両プレイヤーへの成立通知は行われない", func(t *testing.T) {
			processedMatch := &stubProcessedMatchRepo{
				claimFunc:             func(ctx context.Context, matchID string) (bool, error) { return true, nil },
				recordGameCreatedFunc: func(ctx context.Context, matchID, gameID string) error { return nil },
			}
			card := deckResolvableCardStub()
			battle := &stubBattleClient{createPvPGameFunc: func(ctx context.Context, deck1Cards, deck2Cards []service.BattleDeckCard, deck1Initiatives, deck2Initiatives service.DeckInitiatives, player1Summary, player2Summary service.PlayerSummaryRequest) (*service.GameCreatedResult, error) {
				return &service.GameCreatedResult{GameID: "game-1"}, nil
			}}
			gamePlayers := &stubGamePlayerRepo{insertGamePlayerFunc: func(ctx context.Context, gameID string, playerNum int, playerID string) error {
				return errors.New("insert failed")
			}}
			manager := newTestManager(t, managerDeps{processedMatch: processedMatch, card: card, battle: battle, gamePlayers: gamePlayers})
			factory := newTestSocketFactory(t, manager.Hub)
			client1, _ := factory.connect(t, "player-1")
			client2, _ := factory.connect(t, "player-2")

			err := manager.HandleMatchMade(context.Background(), twoPlayerMatchMadeEvent("match-1"))

			assert.EqualError(t, err, "match_made: insert game_player p1: insert failed")
			client1.expectNoMessage(t)
			client2.expectNoMessage(t)
		})

		t.Run("マッチング成立に伴う対戦への参加者記録で、プレイヤー2の記録が失敗したとき、返るエラーの内容はプレイヤー2の記録に関するものだと分かる", func(t *testing.T) {
			processedMatch := &stubProcessedMatchRepo{
				claimFunc:             func(ctx context.Context, matchID string) (bool, error) { return true, nil },
				recordGameCreatedFunc: func(ctx context.Context, matchID, gameID string) error { return nil },
			}
			card := deckResolvableCardStub()
			battle := &stubBattleClient{createPvPGameFunc: func(ctx context.Context, deck1Cards, deck2Cards []service.BattleDeckCard, deck1Initiatives, deck2Initiatives service.DeckInitiatives, player1Summary, player2Summary service.PlayerSummaryRequest) (*service.GameCreatedResult, error) {
				return &service.GameCreatedResult{GameID: "game-1"}, nil
			}}
			gamePlayers := &stubGamePlayerRepo{insertGamePlayerFunc: func(ctx context.Context, gameID string, playerNum int, playerID string) error {
				if playerNum == 2 {
					return errors.New("insert failed")
				}
				return nil
			}}
			manager := newTestManager(t, managerDeps{processedMatch: processedMatch, card: card, battle: battle, gamePlayers: gamePlayers})

			err := manager.HandleMatchMade(context.Background(), twoPlayerMatchMadeEvent("match-1"))

			assert.EqualError(t, err, "match_made: insert game_player p2: insert failed")
		})
	})
}

func TestHandleMatchMadeNotificationFollowUp(t *testing.T) {
	t.Run("[マッチメイキング]マッチ成立通知の配信結果に応じた後続処理", func(t *testing.T) {
		t.Run("対戦の作成と記録が完了した後、通知済みであることの記録が失敗したとき、エラーが返る", func(t *testing.T) {
			processedMatch := &stubProcessedMatchRepo{
				claimFunc:             func(ctx context.Context, matchID string) (bool, error) { return true, nil },
				recordGameCreatedFunc: func(ctx context.Context, matchID, gameID string) error { return nil },
				markNotifiedFunc: func(ctx context.Context, matchID string) (bool, error) {
					return false, errors.New("mark notified failed")
				},
			}
			card := deckResolvableCardStub()
			battle := &stubBattleClient{createPvPGameFunc: func(ctx context.Context, deck1Cards, deck2Cards []service.BattleDeckCard, deck1Initiatives, deck2Initiatives service.DeckInitiatives, player1Summary, player2Summary service.PlayerSummaryRequest) (*service.GameCreatedResult, error) {
				return &service.GameCreatedResult{GameID: "game-1"}, nil
			}}
			gamePlayers := &stubGamePlayerRepo{insertGamePlayerFunc: func(ctx context.Context, gameID string, playerNum int, playerID string) error { return nil }}
			manager := newTestManager(t, managerDeps{processedMatch: processedMatch, card: card, battle: battle, gamePlayers: gamePlayers})

			err := manager.HandleMatchMade(context.Background(), twoPlayerMatchMadeEvent("match-1"))

			assert.ErrorContains(t, err, "mark notified failed")
		})

		t.Run("両プレイヤーへの成立通知が届くとき、それ以上の追加処理は行われない。成立したゲームIDで、1人目・2人目それぞれのプレイヤーが正しい対応で対戦相手として記録される", func(t *testing.T) {
			processedMatch := &stubProcessedMatchRepo{
				claimFunc:             func(ctx context.Context, matchID string) (bool, error) { return true, nil },
				recordGameCreatedFunc: func(ctx context.Context, matchID, gameID string) error { return nil },
				markNotifiedFunc:      func(ctx context.Context, matchID string) (bool, error) { return true, nil },
			}
			card := deckResolvableCardStub()
			battle := &stubBattleClient{createPvPGameFunc: func(ctx context.Context, deck1Cards, deck2Cards []service.BattleDeckCard, deck1Initiatives, deck2Initiatives service.DeckInitiatives, player1Summary, player2Summary service.PlayerSummaryRequest) (*service.GameCreatedResult, error) {
				return &service.GameCreatedResult{GameID: "game-1"}, nil
			}}
			gamePlayers := &stubGamePlayerRepo{insertGamePlayerFunc: func(ctx context.Context, gameID string, playerNum int, playerID string) error { return nil }}
			matchmaking := &stubMatchmakingClient{}
			manager := newTestManager(t, managerDeps{processedMatch: processedMatch, card: card, battle: battle, gamePlayers: gamePlayers, matchmaking: matchmaking})
			factory := newTestSocketFactory(t, manager.Hub)
			client1, _ := factory.connect(t, "player-1")
			client2, _ := factory.connect(t, "player-2")

			err := manager.HandleMatchMade(context.Background(), twoPlayerMatchMadeEvent("match-1"))

			require.NoError(t, err)
			assert.Equal(t, wsconst.WSServerMsgMatchFound, client1.readMessage(t).Type)
			assert.Equal(t, wsconst.WSServerMsgMatchFound, client2.readMessage(t).Type)
			client1.expectNoMessage(t)
			client2.expectNoMessage(t)
			assert.Empty(t, matchmaking.reportMatchAbandonedCallsSnapshot())
			require.Len(t, gamePlayers.insertGamePlayerCallsSnapshot(), 2)
			assert.Equal(t, insertGamePlayerCall{gameID: "game-1", playerNum: 1, playerID: "player-1"}, gamePlayers.insertGamePlayerCallsSnapshot()[0])
			assert.Equal(t, insertGamePlayerCall{gameID: "game-1", playerNum: 2, playerID: "player-2"}, gamePlayers.insertGamePlayerCallsSnapshot()[1])
			require.Len(t, processedMatch.recordGameCreatedCallsSnapshot(), 1)
			assert.Equal(t, recordGameCreatedCall{matchID: "match-1", gameID: "game-1"}, processedMatch.recordGameCreatedCallsSnapshot()[0])
			require.Len(t, battle.createPvPGameCallsSnapshot(), 1)
			createCall := battle.createPvPGameCallsSnapshot()[0]
			assert.Equal(t, "Player One", createCall.player1Summary.Name)
			assert.Equal(t, "Player Two", createCall.player2Summary.Name)
		})

		t.Run("片方のプレイヤーにのみ成立通知が届くとき、通知が届いた方のプレイヤーへ追加でマッチメイキング失敗が通知され、上流へ今回のマッチが不成立であると申告される", func(t *testing.T) {
			processedMatch := &stubProcessedMatchRepo{
				claimFunc:             func(ctx context.Context, matchID string) (bool, error) { return true, nil },
				recordGameCreatedFunc: func(ctx context.Context, matchID, gameID string) error { return nil },
				markNotifiedFunc:      func(ctx context.Context, matchID string) (bool, error) { return true, nil },
			}
			card := deckResolvableCardStub()
			battle := &stubBattleClient{createPvPGameFunc: func(ctx context.Context, deck1Cards, deck2Cards []service.BattleDeckCard, deck1Initiatives, deck2Initiatives service.DeckInitiatives, player1Summary, player2Summary service.PlayerSummaryRequest) (*service.GameCreatedResult, error) {
				return &service.GameCreatedResult{GameID: "game-1"}, nil
			}}
			gamePlayers := &stubGamePlayerRepo{insertGamePlayerFunc: func(ctx context.Context, gameID string, playerNum int, playerID string) error { return nil }}
			matchmaking := &stubMatchmakingClient{reportMatchAbandonedFunc: func(ctx context.Context, matchID string, playerIDs []string) error { return nil }}
			manager := newTestManager(t, managerDeps{processedMatch: processedMatch, card: card, battle: battle, gamePlayers: gamePlayers, matchmaking: matchmaking})
			factory := newTestSocketFactory(t, manager.Hub)
			client1, _ := factory.connect(t, "player-1")

			err := manager.HandleMatchMade(context.Background(), twoPlayerMatchMadeEvent("match-1"))

			require.NoError(t, err)
			assert.Equal(t, wsconst.WSServerMsgMatchFound, client1.readMessage(t).Type)
			failedMsg := client1.readMessage(t)
			assert.Equal(t, wsconst.WSServerMsgError, failedMsg.Type)
			var failedPayload ErrorMessage
			require.NoError(t, json.Unmarshal(failedMsg.Data, &failedPayload))
			assert.Equal(t, "matchmaking_error", failedPayload.ErrorCode)
			assert.Equal(t, "opponent was not connected", failedPayload.Message)
			require.Len(t, matchmaking.reportMatchAbandonedCallsSnapshot(), 1)
			assert.Equal(t, "match-1", matchmaking.reportMatchAbandonedCallsSnapshot()[0].matchID)
		})

		t.Run("どちらのプレイヤーにも成立通知が届かないとき、追加のマッチメイキング失敗通知は行われない(0件)が、上流へ今回のマッチが不成立であると申告される", func(t *testing.T) {
			processedMatch := &stubProcessedMatchRepo{
				claimFunc:             func(ctx context.Context, matchID string) (bool, error) { return true, nil },
				recordGameCreatedFunc: func(ctx context.Context, matchID, gameID string) error { return nil },
				markNotifiedFunc:      func(ctx context.Context, matchID string) (bool, error) { return true, nil },
			}
			card := deckResolvableCardStub()
			battle := &stubBattleClient{createPvPGameFunc: func(ctx context.Context, deck1Cards, deck2Cards []service.BattleDeckCard, deck1Initiatives, deck2Initiatives service.DeckInitiatives, player1Summary, player2Summary service.PlayerSummaryRequest) (*service.GameCreatedResult, error) {
				return &service.GameCreatedResult{GameID: "game-1"}, nil
			}}
			gamePlayers := &stubGamePlayerRepo{insertGamePlayerFunc: func(ctx context.Context, gameID string, playerNum int, playerID string) error { return nil }}
			matchmaking := &stubMatchmakingClient{reportMatchAbandonedFunc: func(ctx context.Context, matchID string, playerIDs []string) error { return nil }}
			manager := newTestManager(t, managerDeps{processedMatch: processedMatch, card: card, battle: battle, gamePlayers: gamePlayers, matchmaking: matchmaking})

			err := manager.HandleMatchMade(context.Background(), twoPlayerMatchMadeEvent("match-1"))

			require.NoError(t, err)
			require.Len(t, matchmaking.reportMatchAbandonedCallsSnapshot(), 1)
		})

		t.Run("上流への不成立の申告自体が失敗したとき、呼び出し元にエラーが返る", func(t *testing.T) {
			processedMatch := &stubProcessedMatchRepo{
				claimFunc:             func(ctx context.Context, matchID string) (bool, error) { return true, nil },
				recordGameCreatedFunc: func(ctx context.Context, matchID, gameID string) error { return nil },
				markNotifiedFunc:      func(ctx context.Context, matchID string) (bool, error) { return true, nil },
			}
			card := deckResolvableCardStub()
			battle := &stubBattleClient{createPvPGameFunc: func(ctx context.Context, deck1Cards, deck2Cards []service.BattleDeckCard, deck1Initiatives, deck2Initiatives service.DeckInitiatives, player1Summary, player2Summary service.PlayerSummaryRequest) (*service.GameCreatedResult, error) {
				return &service.GameCreatedResult{GameID: "game-1"}, nil
			}}
			gamePlayers := &stubGamePlayerRepo{insertGamePlayerFunc: func(ctx context.Context, gameID string, playerNum int, playerID string) error { return nil }}
			matchmaking := &stubMatchmakingClient{reportMatchAbandonedFunc: func(ctx context.Context, matchID string, playerIDs []string) error {
				return errors.New("report failed")
			}}
			manager := newTestManager(t, managerDeps{processedMatch: processedMatch, card: card, battle: battle, gamePlayers: gamePlayers, matchmaking: matchmaking})

			err := manager.HandleMatchMade(context.Background(), twoPlayerMatchMadeEvent("match-1"))

			assert.EqualError(t, err, "match_made: report abandoned match-1: report failed")
		})
	})
}

func TestHandleMatchMadeStopsMatchWait(t *testing.T) {
	t.Run("[マッチメイキング]マッチ成立時の待機タイムアウト停止", func(t *testing.T) {
		t.Run("両プレイヤーがマッチメイキングの待機タイムアウト発生前にマッチ成立イベントを受け取るとき、それぞれの待機タイムアウトは以後発生しなくなる", func(t *testing.T) {
			processedMatch := &stubProcessedMatchRepo{
				claimFunc:             func(ctx context.Context, matchID string) (bool, error) { return true, nil },
				recordGameCreatedFunc: func(ctx context.Context, matchID, gameID string) error { return nil },
				markNotifiedFunc:      func(ctx context.Context, matchID string) (bool, error) { return true, nil },
			}
			card := deckResolvableCardStub()
			battle := &stubBattleClient{createPvPGameFunc: func(ctx context.Context, deck1Cards, deck2Cards []service.BattleDeckCard, deck1Initiatives, deck2Initiatives service.DeckInitiatives, player1Summary, player2Summary service.PlayerSummaryRequest) (*service.GameCreatedResult, error) {
				return &service.GameCreatedResult{GameID: "game-1"}, nil
			}}
			gamePlayers := &stubGamePlayerRepo{insertGamePlayerFunc: func(ctx context.Context, gameID string, playerNum int, playerID string) error { return nil }}
			manager := newTestManager(t, managerDeps{processedMatch: processedMatch, card: card, battle: battle, gamePlayers: gamePlayers, matchmakingTimeout: 80 * time.Millisecond})
			factory := newTestSocketFactory(t, manager.Hub)
			client1, _ := factory.connect(t, "player-1")
			client2, _ := factory.connect(t, "player-2")
			manager.startMatchWait("player-1")
			manager.startMatchWait("player-2")

			err := manager.HandleMatchMade(context.Background(), twoPlayerMatchMadeEvent("match-1"))

			require.NoError(t, err)
			assert.Equal(t, wsconst.WSServerMsgMatchFound, client1.readMessage(t).Type)
			assert.Equal(t, wsconst.WSServerMsgMatchFound, client2.readMessage(t).Type)
			client1.expectNoMessage(t)
			client2.expectNoMessage(t)
		})
	})
}
