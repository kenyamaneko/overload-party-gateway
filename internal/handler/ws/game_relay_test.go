package ws

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apibattle "github.com/kenyamaneko/overload-party-battle/packages/api-battle-rpc-go"
	"github.com/kenyamaneko/overload-party-gateway/internal/port"
	"github.com/kenyamaneko/overload-party-gateway/internal/service"
	wsconst "github.com/kenyamaneko/overload-party-gateway/packages/ws-constants"
)

func gameStateWithTurn(isMyTurn bool, timeBank int64) apibattle.ClientGameState {
	s := minimalClientGameState()
	s.IsMyTurn = isMyTurn
	s.MyView.TimeBank = timeBank
	return s
}

func int64Ptr(v int64) *int64 { return &v }

func stateMarkerFor(playerNum int) json.RawMessage {
	return json.RawMessage(`{"marker":"state-for-player-` + string(rune('0'+playerNum)) + `"}`)
}

func TestGameIDForPlayer(t *testing.T) {
	t.Run("ゲーム所属の問い合わせ", func(t *testing.T) {
		t.Run("そのプレイヤーが現在いずれかの対戦に参加登録されているとき、その対戦の識別子と「見つかった」ことが返る", func(t *testing.T) {
			relay := newTestGameRelay(t, relayDeps{})
			relay.JoinGame("player-1", "game-1", 1)

			gid, ok := relay.GameIDForPlayer("player-1")

			assert.True(t, ok)
			assert.Equal(t, "game-1", gid)
		})

		t.Run("そのプレイヤーが現在どの対戦にも参加登録されていないとき、識別子は空で「見つからなかった」ことが返る", func(t *testing.T) {
			relay := newTestGameRelay(t, relayDeps{})

			gid, ok := relay.GameIDForPlayer("player-unknown")

			assert.False(t, ok)
			assert.Empty(t, gid)
		})
	})
}

func TestJoinGame(t *testing.T) {
	t.Run("ゲームへの参加登録", func(t *testing.T) {
		t.Run("対象のプレイヤーがどの対戦にも参加登録されていない状態で参加登録すると、その対戦の参加登録者としてスロット番号とともに記録される", func(t *testing.T) {
			relay := newTestGameRelay(t, relayDeps{})

			relay.JoinGame("player-1", "game-1", 2)

			num, err := relay.resolvePlayerNum(context.Background(), "game-1", "player-1")
			require.NoError(t, err)
			assert.Equal(t, 2, num)
		})

		t.Run("対象のプレイヤーが既に別の対戦(元の対戦)に参加登録されている状態で、新しい対戦に参加登録すると、以後は新しい対戦の参加登録者一覧にだけ含まれるようになり、元の対戦の参加登録者一覧からは自動的に外れて配信が届かなくなる(元の対戦の記録自体は変更されない)", func(t *testing.T) {
			hub := newTestHub(t, hubDeps{})
			relay := newTestGameRelay(t, relayDeps{hub: hub})
			factory := newTestSocketFactory(t, hub)
			client, _ := factory.connect(t, "player-1")
			relay.JoinGame("player-1", "game-old", 1)

			relay.JoinGame("player-1", "game-new", 1)

			relay.BroadcastToGame("game-old", &WSMessage{Type: "old_game_msg"})
			client.expectNoMessage(t)
			relay.BroadcastToGame("game-new", &WSMessage{Type: "new_game_msg"})
			assert.Equal(t, "new_game_msg", client.readMessage(t).Type)
		})

		t.Run("対象のプレイヤーが同じ対戦に重ねて参加登録しても、その対戦の参加登録者一覧の中で二重にはカウントされない", func(t *testing.T) {
			hub := newTestHub(t, hubDeps{})
			relay := newTestGameRelay(t, relayDeps{hub: hub})
			factory := newTestSocketFactory(t, hub)
			client, _ := factory.connect(t, "player-1")
			relay.JoinGame("player-1", "game-1", 1)

			relay.JoinGame("player-1", "game-1", 1)

			relay.BroadcastToGame("game-1", &WSMessage{Type: "once"})
			assert.Equal(t, "once", client.readMessage(t).Type)
			client.expectNoMessage(t)
		})

		t.Run("対象のプレイヤーが同じ対戦に別のスロット番号で改めて参加登録すると、以後はその新しいスロット番号で扱われる", func(t *testing.T) {
			relay := newTestGameRelay(t, relayDeps{})
			relay.JoinGame("player-1", "game-1", 1)

			relay.JoinGame("player-1", "game-1", 2)

			num, err := relay.resolvePlayerNum(context.Background(), "game-1", "player-1")
			require.NoError(t, err)
			assert.Equal(t, 2, num)
		})
	})
}

func TestLeaveGame(t *testing.T) {
	t.Run("ゲームからの離脱登録", func(t *testing.T) {
		t.Run("対象のプレイヤーがいずれかの対戦に参加登録されているとき、離脱させるとその対戦の参加登録者一覧から外れ、以後その対戦への配信は届かなくなる", func(t *testing.T) {
			hub := newTestHub(t, hubDeps{})
			relay := newTestGameRelay(t, relayDeps{hub: hub})
			factory := newTestSocketFactory(t, hub)
			client, _ := factory.connect(t, "player-1")
			relay.JoinGame("player-1", "game-1", 1)

			relay.LeaveGame("player-1")

			relay.BroadcastToGame("game-1", &WSMessage{Type: "should_not_arrive"})
			client.expectNoMessage(t)
		})

		t.Run("対象のプレイヤーがその対戦の唯一の参加登録者だったとき、離脱後はその対戦の参加登録者は誰もいなくなる", func(t *testing.T) {
			hub := newTestHub(t, hubDeps{})
			relay := newTestGameRelay(t, relayDeps{hub: hub})
			factory := newTestSocketFactory(t, hub)
			client, _ := factory.connect(t, "player-1")
			relay.JoinGame("player-1", "game-1", 1)

			relay.LeaveGame("player-1")

			relay.BroadcastToGame("game-1", &WSMessage{Type: "should_not_arrive"})
			client.expectNoMessage(t)
			_, ok := relay.GameIDForPlayer("player-1")
			assert.False(t, ok)
		})

		t.Run("対象のプレイヤーがどの対戦にも参加登録されていないとき、離脱させても何も変わらない", func(t *testing.T) {
			relay := newTestGameRelay(t, relayDeps{})

			assert.NotPanics(t, func() { relay.LeaveGame("player-unregistered") })
		})
	})
}

func TestResolvePlayerNum(t *testing.T) {
	t.Run("プレイヤーのスロット番号解決", func(t *testing.T) {
		t.Run("対象のプレイヤーが、まさに問い合わせているその対戦に現在参加登録されているとき、参加登録時に記録されたスロット番号がそのまま使われて返る", func(t *testing.T) {
			relay := newTestGameRelay(t, relayDeps{})
			relay.JoinGame("player-1", "game-1", 2)

			num, err := relay.resolvePlayerNum(context.Background(), "game-1", "player-1")

			require.NoError(t, err)
			assert.Equal(t, 2, num)
		})

		t.Run("対象のプレイヤーが、問い合わせているその対戦の参加登録を持たない(別の対戦の参加登録だけを持つ、またはどの対戦の参加登録も持たない)とき、記録済みの参加者一覧を参照してスロット番号が求められる", func(t *testing.T) {
			gamePlayers := &stubGamePlayerRepo{lookupGamePlayersFunc: func(ctx context.Context, gameID string) ([]port.GamePlayerEntry, error) {
				return []port.GamePlayerEntry{{PlayerNum: 1, PlayerID: "player-not-joined"}}, nil
			}}
			relay := newTestGameRelay(t, relayDeps{gamePlayerRepo: gamePlayers})

			num, err := relay.resolvePlayerNum(context.Background(), "game-1", "player-not-joined")

			require.NoError(t, err)
			assert.Equal(t, 1, num)
		})

		t.Run("その参照先の記録自体が利用できない構成のとき、スロット番号は得られず、参照先が無いことを示す失敗が返る", func(t *testing.T) {
			relay := newTestGameRelay(t, relayDeps{gamePlayerRepoUnconfigured: true})

			_, err := relay.resolvePlayerNum(context.Background(), "game-1", "player-1")

			assert.ErrorIs(t, err, errGamePlayerRepoUnavailable)
		})

		t.Run("記録の参照そのものが失敗したとき、その失敗を伝える結果が返る", func(t *testing.T) {
			wantErr := errors.New("lookup game players failed")
			gamePlayers := &stubGamePlayerRepo{lookupGamePlayersFunc: func(ctx context.Context, gameID string) ([]port.GamePlayerEntry, error) {
				return nil, wantErr
			}}
			relay := newTestGameRelay(t, relayDeps{gamePlayerRepo: gamePlayers})

			_, err := relay.resolvePlayerNum(context.Background(), "game-1", "player-1")

			assert.ErrorIs(t, err, wantErr)
		})

		t.Run("記録は参照できたが、対象のプレイヤーがその対戦の参加者一覧に含まれていないとき、参加登録が無いことを示す失敗が返る", func(t *testing.T) {
			gamePlayers := &stubGamePlayerRepo{lookupGamePlayersFunc: func(ctx context.Context, gameID string) ([]port.GamePlayerEntry, error) {
				return []port.GamePlayerEntry{{PlayerNum: 1, PlayerID: "someone-else"}}, nil
			}}
			relay := newTestGameRelay(t, relayDeps{gamePlayerRepo: gamePlayers})

			_, err := relay.resolvePlayerNum(context.Background(), "game-1", "player-1")

			assert.ErrorIs(t, err, errPlayerNotInGame)
		})

		t.Run("記録が参照でき、対象のプレイヤーがその対戦の参加者一覧に含まれているとき、そのスロット番号が返る", func(t *testing.T) {
			gamePlayers := &stubGamePlayerRepo{lookupGamePlayersFunc: func(ctx context.Context, gameID string) ([]port.GamePlayerEntry, error) {
				return []port.GamePlayerEntry{{PlayerNum: 2, PlayerID: "player-1"}}, nil
			}}
			relay := newTestGameRelay(t, relayDeps{gamePlayerRepo: gamePlayers})

			num, err := relay.resolvePlayerNum(context.Background(), "game-1", "player-1")

			require.NoError(t, err)
			assert.Equal(t, 2, num)
		})
	})
}

func TestLookupMatchType(t *testing.T) {
	t.Run("対戦のマッチ種別判定", func(t *testing.T) {
		t.Run("参加者記録の参照先が利用できない構成のとき、種別は「不明」として返り、呼び出し元はNPC対戦向けの処理を行わない", func(t *testing.T) {
			relay := newTestGameRelay(t, relayDeps{gamePlayerRepoUnconfigured: true})

			matchType, err := relay.lookupMatchType(context.Background(), "game-1")

			require.NoError(t, err)
			assert.Empty(t, matchType)
		})

		t.Run("参加者記録の参照に失敗したとき、同様に種別は「不明」として返る", func(t *testing.T) {
			wantErr := errors.New("lookup game players failed")
			gamePlayers := &stubGamePlayerRepo{lookupGamePlayersFunc: func(ctx context.Context, gameID string) ([]port.GamePlayerEntry, error) {
				return nil, wantErr
			}}
			relay := newTestGameRelay(t, relayDeps{gamePlayerRepo: gamePlayers})

			_, err := relay.lookupMatchType(context.Background(), "game-1")

			assert.ErrorIs(t, err, wantErr)
		})

		t.Run("記録上の人間参加者が1人だけのとき、NPC対戦として扱われる", func(t *testing.T) {
			gamePlayers := &stubGamePlayerRepo{lookupGamePlayersFunc: func(ctx context.Context, gameID string) ([]port.GamePlayerEntry, error) {
				return []port.GamePlayerEntry{{PlayerNum: 1, PlayerID: "player-1"}}, nil
			}}
			relay := newTestGameRelay(t, relayDeps{gamePlayerRepo: gamePlayers})

			matchType, err := relay.lookupMatchType(context.Background(), "game-1")

			require.NoError(t, err)
			assert.Equal(t, "npc", matchType)
		})

		t.Run("記録上の人間参加者が2人のとき、PvP対戦として扱われる", func(t *testing.T) {
			gamePlayers := &stubGamePlayerRepo{lookupGamePlayersFunc: func(ctx context.Context, gameID string) ([]port.GamePlayerEntry, error) {
				return []port.GamePlayerEntry{{PlayerNum: 1, PlayerID: "player-1"}, {PlayerNum: 2, PlayerID: "player-2"}}, nil
			}}
			relay := newTestGameRelay(t, relayDeps{gamePlayerRepo: gamePlayers})

			matchType, err := relay.lookupMatchType(context.Background(), "game-1")

			require.NoError(t, err)
			assert.Equal(t, "pvp", matchType)
		})

		t.Run("記録上の人間参加者が1人でも2人でもないとき(0人・3人以上)、想定しない状態としてエラーになる", func(t *testing.T) {
			cases := []struct {
				name            string
				entries         []port.GamePlayerEntry
				wantErrContains string
			}{
				{"0人のとき", []port.GamePlayerEntry{}, "unexpected human player count: 0"},
				{"3人以上のとき", []port.GamePlayerEntry{{PlayerNum: 1, PlayerID: "p1"}, {PlayerNum: 2, PlayerID: "p2"}, {PlayerNum: 3, PlayerID: "p3"}}, "unexpected human player count: 3"},
			}
			for _, tt := range cases {
				t.Run(tt.name, func(t *testing.T) {
					gamePlayers := &stubGamePlayerRepo{lookupGamePlayersFunc: func(ctx context.Context, gameID string) ([]port.GamePlayerEntry, error) {
						return tt.entries, nil
					}}
					relay := newTestGameRelay(t, relayDeps{gamePlayerRepo: gamePlayers})

					_, err := relay.lookupMatchType(context.Background(), "game-1")

					assert.ErrorContains(t, err, tt.wantErrContains)
				})
			}
		})
	})
}

func TestAreBothPlayersDisconnected(t *testing.T) {
	t.Run("双方切断状態の判定", func(t *testing.T) {
		t.Run("参加者記録の参照先が利用できない構成のとき、「双方切断ではない」という結果が返る(エラーにはならない)", func(t *testing.T) {
			relay := newTestGameRelay(t, relayDeps{gamePlayerRepoUnconfigured: true})

			both, err := relay.areBothPlayersDisconnected(context.Background(), "player-1", "game-1")

			require.NoError(t, err)
			assert.False(t, both)
		})

		t.Run("参加者記録の参照に失敗したとき、その失敗を伝える結果が返る", func(t *testing.T) {
			gamePlayers := &stubGamePlayerRepo{lookupGamePlayersFunc: func(ctx context.Context, gameID string) ([]port.GamePlayerEntry, error) {
				return nil, errors.New("lookup game players failed")
			}}
			relay := newTestGameRelay(t, relayDeps{gamePlayerRepo: gamePlayers})

			_, err := relay.areBothPlayersDisconnected(context.Background(), "player-1", "game-1")

			assert.Error(t, err)
		})

		t.Run("その対戦に記録されている人間参加者が1人だけ(NPC対戦)のとき、「双方切断ではない」という結果が返る", func(t *testing.T) {
			gamePlayers := &stubGamePlayerRepo{lookupGamePlayersFunc: func(ctx context.Context, gameID string) ([]port.GamePlayerEntry, error) {
				return []port.GamePlayerEntry{{PlayerNum: 1, PlayerID: "player-1"}}, nil
			}}
			relay := newTestGameRelay(t, relayDeps{gamePlayerRepo: gamePlayers})

			both, err := relay.areBothPlayersDisconnected(context.Background(), "player-1", "game-1")

			require.NoError(t, err)
			assert.False(t, both)
		})

		t.Run("記録されている人間参加者がちょうど2人で、指定したプレイヤーと対戦相手のどちらも現在接続していないとき、「双方切断である」という結果が返る", func(t *testing.T) {
			gamePlayers := &stubGamePlayerRepo{lookupGamePlayersFunc: func(ctx context.Context, gameID string) ([]port.GamePlayerEntry, error) {
				return []port.GamePlayerEntry{{PlayerNum: 1, PlayerID: "player-1"}, {PlayerNum: 2, PlayerID: "player-2"}}, nil
			}}
			relay := newTestGameRelay(t, relayDeps{gamePlayerRepo: gamePlayers})

			both, err := relay.areBothPlayersDisconnected(context.Background(), "player-1", "game-1")

			require.NoError(t, err)
			assert.True(t, both)
		})

		t.Run("記録されている人間参加者がちょうど2人で、指定したプレイヤーと対戦相手の少なくとも一方が現在接続しているとき、「双方切断ではない」という結果が返る", func(t *testing.T) {
			gamePlayers := &stubGamePlayerRepo{lookupGamePlayersFunc: func(ctx context.Context, gameID string) ([]port.GamePlayerEntry, error) {
				return []port.GamePlayerEntry{{PlayerNum: 1, PlayerID: "player-1"}, {PlayerNum: 2, PlayerID: "player-2"}}, nil
			}}
			hub := newTestHub(t, hubDeps{})
			relay := newTestGameRelay(t, relayDeps{hub: hub, gamePlayerRepo: gamePlayers})
			factory := newTestSocketFactory(t, hub)
			factory.connect(t, "player-2")

			both, err := relay.areBothPlayersDisconnected(context.Background(), "player-1", "game-1")

			require.NoError(t, err)
			assert.False(t, both)
		})
	})
}

func TestOpponentPlayerID(t *testing.T) {
	t.Run("対戦相手の識別(記録済みの参加者一覧から)", func(t *testing.T) {
		t.Run("その対戦に記録されている人間参加者がちょうど2人のとき、自分以外のもう1人のプレイヤーIDが対戦相手として返る", func(t *testing.T) {
			entries := []port.GamePlayerEntry{{PlayerNum: 1, PlayerID: "player-1"}, {PlayerNum: 2, PlayerID: "player-2"}}

			got := opponentPlayerID(entries, "player-1")

			assert.Equal(t, "player-2", got)
		})

		t.Run("記録されている人間参加者が1人だけ(NPC対戦)のとき、対戦相手を表すプレイヤーIDは無く「不在」を表す結果が返る", func(t *testing.T) {
			entries := []port.GamePlayerEntry{{PlayerNum: 1, PlayerID: "player-1"}}

			got := opponentPlayerID(entries, "player-1")

			assert.Empty(t, got)
		})
	})
}

func TestPlayerIDsBySlot(t *testing.T) {
	t.Run("スロットごとの参加者特定", func(t *testing.T) {
		t.Run("記録上、スロット1・スロット2の両方に人間参加者が割り当てられているとき、両方のスロットのプレイヤーIDが返る", func(t *testing.T) {
			entries := []port.GamePlayerEntry{{PlayerNum: 1, PlayerID: "player-1"}, {PlayerNum: 2, PlayerID: "player-2"}}

			p1, p2 := playerIDsBySlot(entries)

			assert.Equal(t, "player-1", p1)
			assert.Equal(t, "player-2", p2)
		})

		t.Run("記録上、一方のスロットにしか人間参加者がいない(NPC対戦)とき、参加者がいないスロットは「不在」を表す結果が返る", func(t *testing.T) {
			entries := []port.GamePlayerEntry{{PlayerNum: 1, PlayerID: "player-1"}}

			p1, p2 := playerIDsBySlot(entries)

			assert.Equal(t, "player-1", p1)
			assert.Empty(t, p2)
		})
	})
}

func TestNotifyOpponentDisconnected(t *testing.T) {
	t.Run("対戦相手への切断通知", func(t *testing.T) {
		t.Run("そのプレイヤーが参加登録されている対戦に自分以外の参加登録者が1人いるとき、その1人に「対戦相手が切断した」ことを知らせるメッセージが届く", func(t *testing.T) {
			hub := newTestHub(t, hubDeps{})
			relay := newTestGameRelay(t, relayDeps{hub: hub})
			factory := newTestSocketFactory(t, hub)
			opponentClient, _ := factory.connect(t, "player-opponent")
			relay.JoinGame("player-1", "game-1", 1)
			relay.JoinGame("player-opponent", "game-1", 2)

			relay.NotifyOpponentDisconnected("player-1", "game-1")

			assert.Equal(t, wsconst.WSServerMsgOpponentDisconnected, opponentClient.readMessage(t).Type)
		})

		t.Run("そのプレイヤーが参加登録されている対戦に自分以外の参加登録者が誰もいないとき、誰にも届かない", func(t *testing.T) {
			hub := newTestHub(t, hubDeps{})
			relay := newTestGameRelay(t, relayDeps{hub: hub})
			factory := newTestSocketFactory(t, hub)
			client, _ := factory.connect(t, "player-1")
			relay.JoinGame("player-1", "game-1", 1)

			relay.NotifyOpponentDisconnected("player-1", "game-1")

			client.expectNoMessage(t)
		})
	})
}

func TestNotifyOpponentReconnected(t *testing.T) {
	t.Run("対戦相手への復帰通知", func(t *testing.T) {
		t.Run("そのプレイヤーが参加登録されている対戦に自分以外の参加登録者が1人いるとき、その1人に「対戦相手が復帰した」ことを知らせるメッセージが届く", func(t *testing.T) {
			hub := newTestHub(t, hubDeps{})
			relay := newTestGameRelay(t, relayDeps{hub: hub})
			factory := newTestSocketFactory(t, hub)
			opponentClient, _ := factory.connect(t, "player-opponent")
			relay.JoinGame("player-1", "game-1", 1)
			relay.JoinGame("player-opponent", "game-1", 2)

			relay.NotifyOpponentReconnected("player-1", "game-1")

			assert.Equal(t, wsconst.WSServerMsgOpponentReconnected, opponentClient.readMessage(t).Type)
		})

		t.Run("そのプレイヤーが参加登録されている対戦に自分以外の参加登録者が誰もいないとき、誰にも届かない", func(t *testing.T) {
			hub := newTestHub(t, hubDeps{})
			relay := newTestGameRelay(t, relayDeps{hub: hub})
			factory := newTestSocketFactory(t, hub)
			client, _ := factory.connect(t, "player-1")
			relay.JoinGame("player-1", "game-1", 1)

			relay.NotifyOpponentReconnected("player-1", "game-1")

			client.expectNoMessage(t)
		})
	})
}

func TestBroadcastToGame(t *testing.T) {
	t.Run("ゲーム内メンバーへのメッセージ配信(内容は呼び出し元が指定)", func(t *testing.T) {
		t.Run("対戦に現在参加登録されている人が誰もいないとき、誰にも届かない", func(t *testing.T) {
			relay := newTestGameRelay(t, relayDeps{})

			assert.NotPanics(t, func() { relay.BroadcastToGame("game-empty", &WSMessage{Type: "x"}) })
		})

		t.Run("対戦に現在参加登録されている人が1人のとき、その人が接続中であればそのメッセージが届く", func(t *testing.T) {
			hub := newTestHub(t, hubDeps{})
			relay := newTestGameRelay(t, relayDeps{hub: hub})
			factory := newTestSocketFactory(t, hub)
			client, _ := factory.connect(t, "player-1")
			relay.JoinGame("player-1", "game-1", 1)

			relay.BroadcastToGame("game-1", &WSMessage{Type: "broadcast_test"})

			assert.Equal(t, "broadcast_test", client.readMessage(t).Type)
		})

		t.Run("対戦に現在参加登録されている人が複数のとき、接続中の全員に同一内容のメッセージが届く", func(t *testing.T) {
			hub := newTestHub(t, hubDeps{})
			relay := newTestGameRelay(t, relayDeps{hub: hub})
			factory := newTestSocketFactory(t, hub)
			client1, _ := factory.connect(t, "player-1")
			client2, _ := factory.connect(t, "player-2")
			relay.JoinGame("player-1", "game-1", 1)
			relay.JoinGame("player-2", "game-1", 2)

			relay.BroadcastToGame("game-1", &WSMessage{Type: "broadcast_multi"})

			assert.Equal(t, "broadcast_multi", client1.readMessage(t).Type)
			assert.Equal(t, "broadcast_multi", client2.readMessage(t).Type)
		})

		t.Run("参加登録者の中に現在接続していない人がいるとき、その人には届かない(エラーにもならない)", func(t *testing.T) {
			hub := newTestHub(t, hubDeps{})
			relay := newTestGameRelay(t, relayDeps{hub: hub})
			factory := newTestSocketFactory(t, hub)
			client1, _ := factory.connect(t, "player-1")
			relay.JoinGame("player-1", "game-1", 1)
			relay.JoinGame("player-ghost", "game-1", 2)

			assert.NotPanics(t, func() { relay.BroadcastToGame("game-1", &WSMessage{Type: "broadcast_ghost"}) })
			assert.Equal(t, "broadcast_ghost", client1.readMessage(t).Type)
		})
	})
}

func TestNotifyMatchFoundTo(t *testing.T) {
	t.Run("マッチ成立通知", func(t *testing.T) {
		t.Run("通知対象のプレイヤーが現在接続中のとき、そのプレイヤーに対戦識別子を含む「マッチが成立した」ことを示すメッセージが届き、呼び出し元には配信できたことが返る", func(t *testing.T) {
			hub := newTestHub(t, hubDeps{})
			relay := newTestGameRelay(t, relayDeps{hub: hub})
			factory := newTestSocketFactory(t, hub)
			client, _ := factory.connect(t, "player-1")

			delivered := relay.NotifyMatchFoundTo("game-1", "player-1")

			assert.True(t, delivered)
			msg := client.readMessage(t)
			assert.Equal(t, wsconst.WSServerMsgMatchFound, msg.Type)
			var payload MatchFoundMessage
			require.NoError(t, json.Unmarshal(msg.Data, &payload))
			assert.Equal(t, "game-1", payload.GameID)
		})

		t.Run("通知対象のプレイヤーが現在接続していないとき、何も届かず、呼び出し元には配信できなかったことが返る", func(t *testing.T) {
			relay := newTestGameRelay(t, relayDeps{})

			delivered := relay.NotifyMatchFoundTo("game-1", "player-offline")

			assert.False(t, delivered)
		})
	})
}

func TestNotifyMatchmakingFailed(t *testing.T) {
	t.Run("マッチ不成立通知", func(t *testing.T) {
		t.Run("通知対象のプレイヤーが現在接続中のとき、そのプレイヤーに「対戦相手が接続していなかった」ことを示すエラーが届く", func(t *testing.T) {
			hub := newTestHub(t, hubDeps{})
			relay := newTestGameRelay(t, relayDeps{hub: hub})
			factory := newTestSocketFactory(t, hub)
			client, _ := factory.connect(t, "player-1")

			relay.NotifyMatchmakingFailed("player-1")

			msg := client.readMessage(t)
			assert.Equal(t, wsconst.WSServerMsgError, msg.Type)
		})

		t.Run("通知対象のプレイヤーが現在接続していないとき、何も届かない", func(t *testing.T) {
			relay := newTestGameRelay(t, relayDeps{})

			assert.NotPanics(t, func() { relay.NotifyMatchmakingFailed("player-offline") })
		})
	})
}

func TestHandleUseStamp(t *testing.T) {
	t.Run("スタンプ演出の中継", func(t *testing.T) {
		t.Run("要求のデータ形式が不正なとき、誰にも届かない(要求した本人にもエラーは届かない)", func(t *testing.T) {
			hub := newTestHub(t, hubDeps{})
			relay := newTestGameRelay(t, relayDeps{hub: hub})
			factory := newTestSocketFactory(t, hub)
			client, conn := factory.connect(t, "player-1")
			relay.JoinGame("player-1", "game-1", 1)

			relay.HandleUseStamp(context.Background(), conn, json.RawMessage(`{"stamp_no": "not-a-number"}`))

			client.expectNoMessage(t)
		})

		t.Run("要求した本人のその対戦でのスロット番号が特定できないとき、他の参加登録者には何も届かず、要求した本人にだけエラーが届く", func(t *testing.T) {
			hub := newTestHub(t, hubDeps{})
			gamePlayers := &stubGamePlayerRepo{lookupGamePlayersFunc: func(ctx context.Context, gameID string) ([]port.GamePlayerEntry, error) {
				return nil, errors.New("lookup failed")
			}}
			relay := newTestGameRelay(t, relayDeps{hub: hub, gamePlayerRepo: gamePlayers})
			factory := newTestSocketFactory(t, hub)
			client, conn := factory.connect(t, "player-1")
			opponentClient, _ := factory.connect(t, "player-2")
			relay.JoinGame("player-2", "game-1", 2)

			data, err := json.Marshal(UseStampMessage{GameID: "game-1", StampNo: 3})
			require.NoError(t, err)
			relay.HandleUseStamp(context.Background(), conn, data)

			assert.Equal(t, wsconst.WSServerMsgError, client.readMessage(t).Type)
			opponentClient.expectNoMessage(t)
		})

		t.Run("要求のデータ形式が正しく、かつ要求した本人のその対戦でのスロット番号が特定できるとき、その対戦の現在の参加登録者全員(要求した本人を含む)に、要求した本人のスロット番号と要求されたスタンプ番号を含む「スタンプが使われた」ことを示すメッセージが届く", func(t *testing.T) {
			hub := newTestHub(t, hubDeps{})
			relay := newTestGameRelay(t, relayDeps{hub: hub})
			factory := newTestSocketFactory(t, hub)
			client, conn := factory.connect(t, "player-1")
			opponentClient, _ := factory.connect(t, "player-2")
			relay.JoinGame("player-1", "game-1", 1)
			relay.JoinGame("player-2", "game-1", 2)

			data, err := json.Marshal(UseStampMessage{GameID: "game-1", StampNo: 5})
			require.NoError(t, err)
			relay.HandleUseStamp(context.Background(), conn, data)

			for _, c := range []*testClientConn{client, opponentClient} {
				msg := c.readMessage(t)
				assert.Equal(t, wsconst.WSServerMsgStampUsed, msg.Type)
				var payload StampUsedMessage
				require.NoError(t, json.Unmarshal(msg.Data, &payload))
				assert.Equal(t, int64(1), payload.PlayerNum)
				assert.Equal(t, int64(5), payload.StampNo)
			}
		})
	})
}

func TestLeaveAllPlayers(t *testing.T) {
	t.Run("対戦終了後の全員退出処理", func(t *testing.T) {
		t.Run("対象の対戦に参加登録者が2人いるとき、両方が対戦の参加登録者でなくなり、両方の切断猶予の記録も消える", func(t *testing.T) {
			timerStore := &stubTimerStore{}
			hub := newTestHub(t, hubDeps{timerStore: timerStore})
			relay := newTestGameRelay(t, relayDeps{hub: hub})
			relay.JoinGame("player-1", "game-1", 1)
			relay.JoinGame("player-2", "game-1", 2)

			relay.leaveAllPlayers("game-1")

			_, ok1 := relay.GameIDForPlayer("player-1")
			_, ok2 := relay.GameIDForPlayer("player-2")
			assert.False(t, ok1)
			assert.False(t, ok2)
			cleared := timerStore.clearDisconnectDeadlineCallsSnapshot()
			assert.Contains(t, cleared, "player-1")
			assert.Contains(t, cleared, "player-2")
		})

		t.Run("対象の対戦に参加登録者が1人だけのとき、その1人が対戦の参加登録者でなくなり、切断猶予の記録も消える", func(t *testing.T) {
			timerStore := &stubTimerStore{}
			hub := newTestHub(t, hubDeps{timerStore: timerStore})
			relay := newTestGameRelay(t, relayDeps{hub: hub})
			relay.JoinGame("player-1", "game-1", 1)

			relay.leaveAllPlayers("game-1")

			_, ok := relay.GameIDForPlayer("player-1")
			assert.False(t, ok)
			assert.Contains(t, timerStore.clearDisconnectDeadlineCallsSnapshot(), "player-1")
		})

		t.Run("対象の対戦に参加登録者が誰もいないとき、何も変わらない", func(t *testing.T) {
			relay := newTestGameRelay(t, relayDeps{})

			assert.NotPanics(t, func() { relay.leaveAllPlayers("game-empty") })
		})
	})
}

func TestHandleDisconnectTimeout(t *testing.T) {
	t.Run("切断タイムアウトによる強制敗北", func(t *testing.T) {
		t.Run("双方が現在切断中かどうかの判定自体に失敗したとき、何もしない(未解決のまま残る)", func(t *testing.T) {
			battle := &stubBattleClient{}
			gamePlayers := &stubGamePlayerRepo{lookupGamePlayersFunc: func(ctx context.Context, gameID string) ([]port.GamePlayerEntry, error) {
				return nil, errors.New("lookup failed")
			}}
			relay := newTestGameRelay(t, relayDeps{battleClient: battle, gamePlayerRepo: gamePlayers})
			relay.JoinGame("player-1", "game-1", 1)

			relay.HandleDisconnectTimeout("player-1", "game-1")

			assert.Empty(t, battle.processActionCallsSnapshot())
			_, ok := relay.GameIDForPlayer("player-1")
			assert.True(t, ok)
		})

		t.Run("対戦相手も含めて双方とも現在切断中と判定されたとき、この時点では対戦を終了させない(対戦終了通知は届かず、参加登録も維持される)。対戦のターン制限時間の計測もそのまま", func(t *testing.T) {
			battle := &stubBattleClient{}
			gamePlayers := &stubGamePlayerRepo{lookupGamePlayersFunc: func(ctx context.Context, gameID string) ([]port.GamePlayerEntry, error) {
				return []port.GamePlayerEntry{{PlayerNum: 1, PlayerID: "player-1"}, {PlayerNum: 2, PlayerID: "player-2"}}, nil
			}}
			relay := newTestGameRelay(t, relayDeps{battleClient: battle, gamePlayerRepo: gamePlayers})
			relay.JoinGame("player-1", "game-1", 1)
			relay.JoinGame("player-2", "game-1", 2)

			relay.HandleDisconnectTimeout("player-1", "game-1")

			assert.Empty(t, battle.processActionCallsSnapshot())
			_, ok1 := relay.GameIDForPlayer("player-1")
			_, ok2 := relay.GameIDForPlayer("player-2")
			assert.True(t, ok1)
			assert.True(t, ok2)
		})

		t.Run("双方切断ではない(対戦相手が接続中、または対戦相手を特定できない)とき、その対戦のターン制限時間の計測が止まる", func(t *testing.T) {
			clock := newFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
			battle := &stubBattleClient{processActionFunc: func(ctx context.Context, gameID string, playerNum int, actionType string, data json.RawMessage) (*service.ActionResult, error) {
				return nil, errors.New("forfeit failed")
			}}
			gamePlayers := &stubGamePlayerRepo{lookupGamePlayersFunc: func(ctx context.Context, gameID string) ([]port.GamePlayerEntry, error) {
				return []port.GamePlayerEntry{{PlayerNum: 1, PlayerID: "player-1"}, {PlayerNum: 2, PlayerID: "player-2"}}, nil
			}}
			hub := newTestHub(t, hubDeps{clock: clock})
			relay := newTestGameRelay(t, relayDeps{hub: hub, battleClient: battle, gamePlayerRepo: gamePlayers, clock: clock})
			factory := newTestSocketFactory(t, hub)
			client1, _ := factory.connect(t, "player-1")
			factory.connect(t, "player-2")
			relay.JoinGame("player-1", "game-1", 1)
			relay.JoinGame("player-2", "game-1", 2)
			relay.resetTurnTimer("game-1", "player-1", 1)

			relay.HandleDisconnectTimeout("player-1", "game-1")
			clock.Advance(10 * time.Second)

			client1.expectNoMessage(t)
		})

		t.Run("切断したプレイヤー本人のスロット番号が特定できないとき、何もしない(未解決のまま残る)", func(t *testing.T) {
			battle := &stubBattleClient{}
			gamePlayers := &stubGamePlayerRepo{lookupGamePlayersFunc: func(ctx context.Context, gameID string) ([]port.GamePlayerEntry, error) {
				return []port.GamePlayerEntry{{PlayerNum: 2, PlayerID: "player-2"}}, nil
			}}
			hub := newTestHub(t, hubDeps{})
			relay := newTestGameRelay(t, relayDeps{hub: hub, battleClient: battle, gamePlayerRepo: gamePlayers})
			factory := newTestSocketFactory(t, hub)
			factory.connect(t, "player-2")

			relay.HandleDisconnectTimeout("player-1", "game-1")

			assert.Empty(t, battle.processActionCallsSnapshot())
		})

		t.Run("不戦敗の記録要求が失敗したとき、何もしない(未解決のまま残る)。対戦相手への通知手段が無いため、この状態は監視に委ねられる", func(t *testing.T) {
			battle := &stubBattleClient{processActionFunc: func(ctx context.Context, gameID string, playerNum int, actionType string, data json.RawMessage) (*service.ActionResult, error) {
				return nil, errors.New("forfeit failed")
			}}
			gamePlayers := &stubGamePlayerRepo{lookupGamePlayersFunc: func(ctx context.Context, gameID string) ([]port.GamePlayerEntry, error) {
				return []port.GamePlayerEntry{{PlayerNum: 1, PlayerID: "player-1"}, {PlayerNum: 2, PlayerID: "player-2"}}, nil
			}}
			hub := newTestHub(t, hubDeps{})
			relay := newTestGameRelay(t, relayDeps{hub: hub, battleClient: battle, gamePlayerRepo: gamePlayers})
			factory := newTestSocketFactory(t, hub)
			client1, _ := factory.connect(t, "player-1")
			opponentClient, _ := factory.connect(t, "player-2")
			relay.JoinGame("player-1", "game-1", 1)
			relay.JoinGame("player-2", "game-1", 2)

			relay.HandleDisconnectTimeout("player-1", "game-1")

			client1.expectNoMessage(t)
			opponentClient.expectNoMessage(t)
			_, ok := relay.GameIDForPlayer("player-1")
			assert.True(t, ok)
		})

		t.Run("不戦敗の記録が成立し対戦が終了したとき、その時点の参加登録者全員に、終了理由を「切断」に固定した対戦終了通知が届き、参加者に経験値が付与され、以後全員がその対戦の参加登録者でなくなる", func(t *testing.T) {
			battle := &stubBattleClient{processActionFunc: func(ctx context.Context, gameID string, playerNum int, actionType string, data json.RawMessage) (*service.ActionResult, error) {
				return &service.ActionResult{GameOver: true, WinningPlayerNum: 2, WinReason: "budget_zero"}, nil
			}}
			account := &stubAccountClient{}
			gamePlayers := &stubGamePlayerRepo{
				lookupGamePlayersFunc: func(ctx context.Context, gameID string) ([]port.GamePlayerEntry, error) {
					return []port.GamePlayerEntry{{PlayerNum: 1, PlayerID: "player-1"}, {PlayerNum: 2, PlayerID: "player-2"}}, nil
				},
				markExpAwardedFunc: func(ctx context.Context, gameID string) (bool, error) { return true, nil },
			}
			hub := newTestHub(t, hubDeps{})
			relay := newTestGameRelay(t, relayDeps{hub: hub, battleClient: battle, accountClient: account, gamePlayerRepo: gamePlayers})
			factory := newTestSocketFactory(t, hub)
			client1, _ := factory.connect(t, "player-1")
			opponentClient, _ := factory.connect(t, "player-2")
			relay.JoinGame("player-1", "game-1", 1)
			relay.JoinGame("player-2", "game-1", 2)

			relay.HandleDisconnectTimeout("player-1", "game-1")

			for _, c := range []*testClientConn{client1, opponentClient} {
				msg := c.readMessage(t)
				assert.Equal(t, wsconst.WSServerMsgGameOver, msg.Type)
				var payload GameOverMessage
				require.NoError(t, json.Unmarshal(msg.Data, &payload))
				assert.Equal(t, "disconnect", payload.WinReason)
			}
			assert.Len(t, account.awardGameExpCallsSnapshot(), 1)
			_, ok1 := relay.GameIDForPlayer("player-1")
			_, ok2 := relay.GameIDForPlayer("player-2")
			assert.False(t, ok1)
			assert.False(t, ok2)
		})

		t.Run("記録要求は成功したが対戦が終了と判定されなかったとき、対戦終了通知は届かない", func(t *testing.T) {
			battle := &stubBattleClient{processActionFunc: func(ctx context.Context, gameID string, playerNum int, actionType string, data json.RawMessage) (*service.ActionResult, error) {
				return &service.ActionResult{GameOver: false}, nil
			}}
			gamePlayers := &stubGamePlayerRepo{lookupGamePlayersFunc: func(ctx context.Context, gameID string) ([]port.GamePlayerEntry, error) {
				return []port.GamePlayerEntry{{PlayerNum: 1, PlayerID: "player-1"}, {PlayerNum: 2, PlayerID: "player-2"}}, nil
			}}
			hub := newTestHub(t, hubDeps{})
			relay := newTestGameRelay(t, relayDeps{hub: hub, battleClient: battle, gamePlayerRepo: gamePlayers})
			factory := newTestSocketFactory(t, hub)
			client1, _ := factory.connect(t, "player-1")
			opponentClient, _ := factory.connect(t, "player-2")
			relay.JoinGame("player-1", "game-1", 1)
			relay.JoinGame("player-2", "game-1", 2)

			relay.HandleDisconnectTimeout("player-1", "game-1")

			client1.expectNoMessage(t)
			opponentClient.expectNoMessage(t)
		})
	})
}

func TestSendGameStateToPlayers(t *testing.T) {
	t.Run("ゲーム状態の配信とターン計時の更新", func(t *testing.T) {
		t.Run("対戦に現在参加登録されている人が誰もいないとき、誰にも配信されず、ターンの制限時間の計測にも影響しない", func(t *testing.T) {
			battle := &stubBattleClient{}
			relay := newTestGameRelay(t, relayDeps{battleClient: battle})

			assert.NotPanics(t, func() { relay.SendGameStateToPlayers("game-empty") })
		})

		t.Run("各参加登録者について、対戦状態の取得に失敗したとき、原因が何であれ(本人の接続が失われたことによる中断であっても)、その人にはエラーが届く", func(t *testing.T) {
			hub := newTestHub(t, hubDeps{})
			battle := &stubBattleClient{getGameStateForPlayerFunc: func(ctx context.Context, gameID string, playerNum int) (json.RawMessage, error) {
				return nil, errors.New("get game state failed")
			}}
			relay := newTestGameRelay(t, relayDeps{hub: hub, battleClient: battle})
			factory := newTestSocketFactory(t, hub)
			client, _ := factory.connect(t, "player-1")
			relay.JoinGame("player-1", "game-1", 1)

			relay.SendGameStateToPlayers("game-1")

			assert.Equal(t, wsconst.WSServerMsgError, client.readMessage(t).Type)
		})

		t.Run("対戦状態の取得に成功したとき、その人には最新の対戦状態が届く", func(t *testing.T) {
			hub := newTestHub(t, hubDeps{})
			state := newRawGameState(t, gameStateWithTurn(false, 0))
			battle := &stubBattleClient{getGameStateForPlayerFunc: func(ctx context.Context, gameID string, playerNum int) (json.RawMessage, error) {
				return state, nil
			}}
			relay := newTestGameRelay(t, relayDeps{hub: hub, battleClient: battle})
			factory := newTestSocketFactory(t, hub)
			client, _ := factory.connect(t, "player-1")
			relay.JoinGame("player-1", "game-1", 1)

			relay.SendGameStateToPlayers("game-1")

			assert.Equal(t, wsconst.WSServerMsgGameState, client.readMessage(t).Type)
		})

		t.Run("配信した対戦状態の中に「今が自分の手番である」ことを示す人がいて、その人の残り時間が直前にターン制限時間の計測を開始したときの(手番のプレイヤー, タイムバンク)の組と異なる(手番の交代を含む)とき、その対戦のターン制限時間の計測が、その人の残り時間をもとに新しく始まる", func(t *testing.T) {
			clock := newFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
			battle := &stubBattleClient{processActionFunc: alwaysFailProcessAction}
			var state apibattle.ClientGameState
			battle.getGameStateForPlayerFunc = func(ctx context.Context, gameID string, playerNum int) (json.RawMessage, error) {
				return newRawGameState(t, state), nil
			}
			hub := newTestHub(t, hubDeps{clock: clock})
			gamePlayers := &stubGamePlayerRepo{lookupGamePlayersFunc: func(ctx context.Context, gameID string) ([]port.GamePlayerEntry, error) {
				return []port.GamePlayerEntry{{PlayerNum: 1, PlayerID: "player-1"}}, nil
			}}
			relay := newTestGameRelay(t, relayDeps{hub: hub, battleClient: battle, gamePlayerRepo: gamePlayers, clock: clock})
			factory := newTestSocketFactory(t, hub)
			client, _ := factory.connect(t, "player-1")
			relay.JoinGame("player-1", "game-1", 1)

			state = gameStateWithTurn(true, 1)
			relay.SendGameStateToPlayers("game-1")
			assert.Equal(t, wsconst.WSServerMsgGameState, client.readMessage(t).Type)

			clock.Advance(2 * time.Second)
			state = gameStateWithTurn(true, 5)
			relay.SendGameStateToPlayers("game-1")
			assert.Equal(t, wsconst.WSServerMsgGameState, client.readMessage(t).Type)

			clock.Advance(1 * time.Second)
			client.expectNoMessage(t)

			clock.Advance(6 * time.Second)
			assert.Equal(t, wsconst.WSServerMsgError, client.readMessage(t).Type)
		})

		t.Run("配信した対戦状態の中に「今が自分の手番である」ことを示す人がいるが、その(手番のプレイヤー, タイムバンク)の組が直前の計測開始時と同じとき、既存のターン制限時間の計測はそのまま継続し、新しく始まらない", func(t *testing.T) {
			clock := newFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
			battle := &stubBattleClient{processActionFunc: alwaysFailProcessAction}
			state := gameStateWithTurn(true, 1)
			battle.getGameStateForPlayerFunc = func(ctx context.Context, gameID string, playerNum int) (json.RawMessage, error) {
				return newRawGameState(t, state), nil
			}
			hub := newTestHub(t, hubDeps{clock: clock})
			gamePlayers := &stubGamePlayerRepo{lookupGamePlayersFunc: func(ctx context.Context, gameID string) ([]port.GamePlayerEntry, error) {
				return []port.GamePlayerEntry{{PlayerNum: 1, PlayerID: "player-1"}}, nil
			}}
			relay := newTestGameRelay(t, relayDeps{hub: hub, battleClient: battle, gamePlayerRepo: gamePlayers, clock: clock})
			factory := newTestSocketFactory(t, hub)
			client, _ := factory.connect(t, "player-1")
			relay.JoinGame("player-1", "game-1", 1)

			relay.SendGameStateToPlayers("game-1")
			assert.Equal(t, wsconst.WSServerMsgGameState, client.readMessage(t).Type)

			clock.Advance(2 * time.Second)
			relay.SendGameStateToPlayers("game-1")
			assert.Equal(t, wsconst.WSServerMsgGameState, client.readMessage(t).Type)

			clock.Advance(1 * time.Second)
			assert.Equal(t, wsconst.WSServerMsgError, client.readMessage(t).Type)
		})

		t.Run("配信した対戦状態のどれも「今が自分の手番である」ことを示していないとき、既存のターン制限時間の計測に変化はない", func(t *testing.T) {
			clock := newFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
			battle := &stubBattleClient{processActionFunc: alwaysFailProcessAction}
			var state apibattle.ClientGameState
			battle.getGameStateForPlayerFunc = func(ctx context.Context, gameID string, playerNum int) (json.RawMessage, error) {
				return newRawGameState(t, state), nil
			}
			hub := newTestHub(t, hubDeps{clock: clock})
			gamePlayers := &stubGamePlayerRepo{lookupGamePlayersFunc: func(ctx context.Context, gameID string) ([]port.GamePlayerEntry, error) {
				return []port.GamePlayerEntry{{PlayerNum: 1, PlayerID: "player-1"}}, nil
			}}
			relay := newTestGameRelay(t, relayDeps{hub: hub, battleClient: battle, gamePlayerRepo: gamePlayers, clock: clock})
			factory := newTestSocketFactory(t, hub)
			client, _ := factory.connect(t, "player-1")
			relay.JoinGame("player-1", "game-1", 1)

			state = gameStateWithTurn(true, 1)
			relay.SendGameStateToPlayers("game-1")
			assert.Equal(t, wsconst.WSServerMsgGameState, client.readMessage(t).Type)

			clock.Advance(2 * time.Second)
			state = gameStateWithTurn(false, 0)
			relay.SendGameStateToPlayers("game-1")
			assert.Equal(t, wsconst.WSServerMsgGameState, client.readMessage(t).Type)

			clock.Advance(1 * time.Second)
			assert.Equal(t, wsconst.WSServerMsgError, client.readMessage(t).Type)
		})
	})
}

func TestSendTurnControlsToPlayers(t *testing.T) {
	t.Run("ターンコントロール情報の配信", func(t *testing.T) {
		t.Run("対戦に現在参加登録されている人が誰もいないとき、誰にも配信されない", func(t *testing.T) {
			relay := newTestGameRelay(t, relayDeps{})

			assert.NotPanics(t, func() { relay.SendTurnControlsToPlayers("game-empty") })
		})

		t.Run("各参加登録者について、ターンコントロール情報の取得に失敗したとき、原因が何であれ(本人の接続が失われたことによる中断であっても)、その人にはエラーが届く", func(t *testing.T) {
			hub := newTestHub(t, hubDeps{})
			battle := &stubBattleClient{getTurnControlsForPlayerFunc: func(ctx context.Context, gameID string, playerNum int) (json.RawMessage, error) {
				return nil, errors.New("get turn controls failed")
			}}
			relay := newTestGameRelay(t, relayDeps{hub: hub, battleClient: battle})
			factory := newTestSocketFactory(t, hub)
			client, _ := factory.connect(t, "player-1")
			relay.JoinGame("player-1", "game-1", 1)

			relay.SendTurnControlsToPlayers("game-1")

			assert.Equal(t, wsconst.WSServerMsgError, client.readMessage(t).Type)
		})

		t.Run("取得はできたが、その人に今表示すべき操作情報が無い(たとえば行動できる手が無い)とき、その人には新しい情報が届かない(エラーにもならない)", func(t *testing.T) {
			hub := newTestHub(t, hubDeps{})
			battle := &stubBattleClient{getTurnControlsForPlayerFunc: func(ctx context.Context, gameID string, playerNum int) (json.RawMessage, error) {
				return nil, nil
			}}
			relay := newTestGameRelay(t, relayDeps{hub: hub, battleClient: battle})
			factory := newTestSocketFactory(t, hub)
			client, _ := factory.connect(t, "player-1")
			relay.JoinGame("player-1", "game-1", 1)

			relay.SendTurnControlsToPlayers("game-1")

			client.expectNoMessage(t)
		})

		t.Run("表示すべき操作情報があるとき、その人にターンコントロール情報が届く", func(t *testing.T) {
			hub := newTestHub(t, hubDeps{})
			controls := newRawTurnControls(t, apibattle.TurnControlsMessage{CanEndPhase: true})
			battle := &stubBattleClient{getTurnControlsForPlayerFunc: func(ctx context.Context, gameID string, playerNum int) (json.RawMessage, error) {
				return controls, nil
			}}
			relay := newTestGameRelay(t, relayDeps{hub: hub, battleClient: battle})
			factory := newTestSocketFactory(t, hub)
			client, _ := factory.connect(t, "player-1")
			relay.JoinGame("player-1", "game-1", 1)

			relay.SendTurnControlsToPlayers("game-1")

			assert.Equal(t, wsconst.WSServerMsgTurnControls, client.readMessage(t).Type)
		})
	})
}

func TestSendActionPerformed(t *testing.T) {
	t.Run("行動結果イベントの配信先振り分け", func(t *testing.T) {
		t.Run("直前の処理結果自体が得られていない、またはイベントが1件もないとき、誰にも配信されない", func(t *testing.T) {
			cases := []struct {
				name   string
				result *service.ActionResult
			}{
				{"処理結果が得られていない(nil)とき", nil},
				{"イベントが1件もないとき", &service.ActionResult{Events: []service.ActionEvent{}}},
			}
			for _, tt := range cases {
				t.Run(tt.name, func(t *testing.T) {
					hub := newTestHub(t, hubDeps{})
					relay := newTestGameRelay(t, relayDeps{hub: hub})
					factory := newTestSocketFactory(t, hub)
					client, _ := factory.connect(t, "player-1")
					relay.JoinGame("player-1", "game-1", 1)

					relay.sendActionPerformed(context.Background(), "game-1", "player-1", tt.result)

					client.expectNoMessage(t)
				})
			}
		})

		t.Run("行動したプレイヤーのスロット番号が特定できないとき、そのイベント群は誰にも配信されず、行動したプレイヤーにエラーが届く", func(t *testing.T) {
			hub := newTestHub(t, hubDeps{})
			gamePlayers := &stubGamePlayerRepo{lookupGamePlayersFunc: func(ctx context.Context, gameID string) ([]port.GamePlayerEntry, error) {
				return []port.GamePlayerEntry{{PlayerNum: 2, PlayerID: "player-2"}}, nil
			}}
			relay := newTestGameRelay(t, relayDeps{hub: hub, gamePlayerRepo: gamePlayers})
			factory := newTestSocketFactory(t, hub)
			actorClient, _ := factory.connect(t, "player-1")
			otherClient, _ := factory.connect(t, "player-2")
			relay.JoinGame("player-2", "game-1", 2)
			result := &service.ActionResult{Events: []service.ActionEvent{{Sequence: 1, EventType: "play_card", PlayerNum: nil}}}

			relay.sendActionPerformed(context.Background(), "game-1", "player-1", result)

			assert.Equal(t, wsconst.WSServerMsgError, actorClient.readMessage(t).Type)
			otherClient.expectNoMessage(t)
		})

		t.Run("イベントがシステム由来(ターン開始など、特定の実行者に紐づかない)のとき、その時点の参加登録者全員(行動したプレイヤーを含む)に、各自の視点の対戦状態とともに届く", func(t *testing.T) {
			hub := newTestHub(t, hubDeps{})
			battle := &stubBattleClient{getGameStateForPlayerFunc: func(ctx context.Context, gameID string, playerNum int) (json.RawMessage, error) {
				return stateMarkerFor(playerNum), nil
			}}
			relay := newTestGameRelay(t, relayDeps{hub: hub, battleClient: battle})
			factory := newTestSocketFactory(t, hub)
			actorClient, _ := factory.connect(t, "player-1")
			otherClient, _ := factory.connect(t, "player-2")
			relay.JoinGame("player-1", "game-1", 1)
			relay.JoinGame("player-2", "game-1", 2)
			result := &service.ActionResult{Events: []service.ActionEvent{{Sequence: 1, EventType: "turn_start", PlayerNum: nil}}}

			relay.sendActionPerformed(context.Background(), "game-1", "player-1", result)

			actorMsg := actorClient.readMessage(t)
			assert.Equal(t, wsconst.WSServerMsgActionPerformed, actorMsg.Type)
			var actorPayload ActionPerformedMessage
			require.NoError(t, json.Unmarshal(actorMsg.Data, &actorPayload))
			assert.JSONEq(t, string(stateMarkerFor(1)), string(actorPayload.State))

			otherMsg := otherClient.readMessage(t)
			assert.Equal(t, wsconst.WSServerMsgActionPerformed, otherMsg.Type)
			var otherPayload ActionPerformedMessage
			require.NoError(t, json.Unmarshal(otherMsg.Data, &otherPayload))
			assert.JSONEq(t, string(stateMarkerFor(2)), string(otherPayload.State))
		})

		t.Run("イベントが行動したプレイヤー自身の行動によるものとき、行動したプレイヤー以外の現在の参加登録者に、各自の視点の対戦状態とともに届く(行動したプレイヤー自身には届かない)", func(t *testing.T) {
			hub := newTestHub(t, hubDeps{})
			battle := &stubBattleClient{getGameStateForPlayerFunc: func(ctx context.Context, gameID string, playerNum int) (json.RawMessage, error) {
				return stateMarkerFor(playerNum), nil
			}}
			relay := newTestGameRelay(t, relayDeps{hub: hub, battleClient: battle})
			factory := newTestSocketFactory(t, hub)
			actorClient, _ := factory.connect(t, "player-1")
			otherClient, _ := factory.connect(t, "player-2")
			relay.JoinGame("player-1", "game-1", 1)
			relay.JoinGame("player-2", "game-1", 2)
			result := &service.ActionResult{Events: []service.ActionEvent{{Sequence: 1, EventType: "play_card", PlayerNum: int64Ptr(1)}}}

			relay.sendActionPerformed(context.Background(), "game-1", "player-1", result)

			otherMsg := otherClient.readMessage(t)
			assert.Equal(t, wsconst.WSServerMsgActionPerformed, otherMsg.Type)
			actorClient.expectNoMessage(t)
		})

		t.Run("行動したプレイヤー以外の現在の参加登録者が誰もいないとき、そのイベントは誰にも届かない", func(t *testing.T) {
			hub := newTestHub(t, hubDeps{})
			relay := newTestGameRelay(t, relayDeps{hub: hub})
			factory := newTestSocketFactory(t, hub)
			actorClient, _ := factory.connect(t, "player-1")
			relay.JoinGame("player-1", "game-1", 1)
			result := &service.ActionResult{Events: []service.ActionEvent{{Sequence: 1, EventType: "play_card", PlayerNum: int64Ptr(1)}}}

			relay.sendActionPerformed(context.Background(), "game-1", "player-1", result)

			actorClient.expectNoMessage(t)
		})

		t.Run("イベントが行動したプレイヤーでも他の参加登録者でもないプレイヤー(NPCなど)による行動で、かつそのイベントに対戦状態のスナップショットが付いているとき、行動したプレイヤー自身に、そのスナップショットのまま届く", func(t *testing.T) {
			hub := newTestHub(t, hubDeps{})
			relay := newTestGameRelay(t, relayDeps{hub: hub})
			factory := newTestSocketFactory(t, hub)
			actorClient, _ := factory.connect(t, "player-1")
			relay.JoinGame("player-1", "game-1", 1)
			npcState := map[string]interface{}{"marker": "npc-snapshot"}
			result := &service.ActionResult{Events: []service.ActionEvent{{Sequence: 1, EventType: "play_card", PlayerNum: int64Ptr(99), State: npcState}}}

			relay.sendActionPerformed(context.Background(), "game-1", "player-1", result)

			msg := actorClient.readMessage(t)
			assert.Equal(t, wsconst.WSServerMsgActionPerformed, msg.Type)
			var payload ActionPerformedMessage
			require.NoError(t, json.Unmarshal(msg.Data, &payload))
			var gotState map[string]interface{}
			require.NoError(t, json.Unmarshal(payload.State, &gotState))
			assert.Equal(t, npcState, gotState)
		})

		t.Run("イベントが行動したプレイヤーでも他の参加登録者でもないプレイヤーによる行動だが、対戦状態のスナップショットが付いていないとき、そのイベントは誰にも届かない", func(t *testing.T) {
			hub := newTestHub(t, hubDeps{})
			relay := newTestGameRelay(t, relayDeps{hub: hub})
			factory := newTestSocketFactory(t, hub)
			actorClient, _ := factory.connect(t, "player-1")
			relay.JoinGame("player-1", "game-1", 1)
			result := &service.ActionResult{Events: []service.ActionEvent{{Sequence: 1, EventType: "play_card", PlayerNum: int64Ptr(99)}}}

			relay.sendActionPerformed(context.Background(), "game-1", "player-1", result)

			actorClient.expectNoMessage(t)
		})

	})
}

func TestPlayerNumOf(t *testing.T) {
	t.Run("プレイヤーのスロット番号照会(記録済みの参加者一覧から)", func(t *testing.T) {
		t.Run("記録上そのプレイヤーが対戦の参加者一覧に含まれているとき、そのプレイヤーが登録されているスロット(1 または 2)の番号が得られる", func(t *testing.T) {
			entries := []port.GamePlayerEntry{{PlayerNum: 1, PlayerID: "player-1"}, {PlayerNum: 2, PlayerID: "player-2"}}

			num := playerNumOf(entries, "player-2")

			assert.Equal(t, 2, num)
		})

		t.Run("記録上そのプレイヤーが対戦の参加者一覧に含まれていないとき、スロット番号は得られない(「不在」を表す結果になる)", func(t *testing.T) {
			entries := []port.GamePlayerEntry{{PlayerNum: 1, PlayerID: "player-1"}}

			num := playerNumOf(entries, "player-unknown")

			assert.Zero(t, num)
		})
	})
}

func TestBroadcastGameOver(t *testing.T) {
	t.Run("対戦終了のブロードキャストと結果反映", func(t *testing.T) {
		t.Run("対戦終了が確定したとき、その時点の参加登録者全員に、勝者のプレイヤー番号と終了理由を含む対戦終了通知が届く", func(t *testing.T) {
			account := &stubAccountClient{}
			gamePlayers := &stubGamePlayerRepo{
				lookupGamePlayersFunc: func(ctx context.Context, gameID string) ([]port.GamePlayerEntry, error) {
					return []port.GamePlayerEntry{{PlayerNum: 1, PlayerID: "player-1"}, {PlayerNum: 2, PlayerID: "player-2"}}, nil
				},
				markExpAwardedFunc: func(ctx context.Context, gameID string) (bool, error) { return true, nil },
			}
			hub := newTestHub(t, hubDeps{})
			relay := newTestGameRelay(t, relayDeps{hub: hub, accountClient: account, gamePlayerRepo: gamePlayers})
			factory := newTestSocketFactory(t, hub)
			client1, _ := factory.connect(t, "player-1")
			client2, _ := factory.connect(t, "player-2")
			relay.JoinGame("player-1", "game-1", 1)
			relay.JoinGame("player-2", "game-1", 2)

			relay.broadcastGameOver("game-1", 2, "budget_zero")

			for _, c := range []*testClientConn{client1, client2} {
				msg := c.readMessage(t)
				assert.Equal(t, wsconst.WSServerMsgGameOver, msg.Type)
				var payload GameOverMessage
				require.NoError(t, json.Unmarshal(msg.Data, &payload))
				assert.Equal(t, int64(2), payload.WinningPlayerNum)
				assert.Equal(t, "budget_zero", payload.WinReason)
			}
		})

		t.Run("対戦終了通知に続けて、その対戦の参加者に経験値が付与される(対戦終了後の経験値付与の規定に従い、記録済みの対戦参加者それぞれについて経験値付与が試みられる)", func(t *testing.T) {
			account := &stubAccountClient{}
			gamePlayers := &stubGamePlayerRepo{
				lookupGamePlayersFunc: func(ctx context.Context, gameID string) ([]port.GamePlayerEntry, error) {
					return []port.GamePlayerEntry{{PlayerNum: 1, PlayerID: "player-1"}, {PlayerNum: 2, PlayerID: "player-2"}}, nil
				},
				markExpAwardedFunc: func(ctx context.Context, gameID string) (bool, error) { return true, nil },
			}
			hub := newTestHub(t, hubDeps{})
			relay := newTestGameRelay(t, relayDeps{hub: hub, accountClient: account, gamePlayerRepo: gamePlayers})
			factory := newTestSocketFactory(t, hub)
			client1, _ := factory.connect(t, "player-1")
			client2, _ := factory.connect(t, "player-2")
			relay.JoinGame("player-1", "game-1", 1)
			relay.JoinGame("player-2", "game-1", 2)

			relay.broadcastGameOver("game-1", 2, "budget_zero")

			for _, c := range []*testClientConn{client1, client2} {
				c.readMessage(t)
			}
			require.Len(t, account.awardGameExpCallsSnapshot(), 1)
			call := account.awardGameExpCallsSnapshot()[0]
			assert.Equal(t, "player-1", call.p1ID)
			assert.Equal(t, "player-2", call.p2ID)
			assert.Equal(t, int64(2), call.winnerNum)
		})
	})
}

func expiredDeadlineTimerStore() *stubTimerStore {
	return &stubTimerStore{getDisconnectDeadlineFunc: func(ctx context.Context, id string) (port.DisconnectDeadline, bool, error) {
		return port.DisconnectDeadline{GameID: "game-1", Deadline: time.Now().Add(-time.Minute)}, true, nil
	}}
}

func futureDeadlineTimerStore() *stubTimerStore {
	return &stubTimerStore{getDisconnectDeadlineFunc: func(ctx context.Context, id string) (port.DisconnectDeadline, bool, error) {
		return port.DisconnectDeadline{GameID: "game-1", Deadline: time.Now().Add(time.Minute)}, true, nil
	}}
}

func TestResolveStaleDisconnect(t *testing.T) {
	t.Run("復帰を起点とした未決着対戦の解消", func(t *testing.T) {
		t.Run("復帰した対戦が停止によって無効と記録済みのとき、対戦を継続させる", func(t *testing.T) {
			battle := &stubBattleClient{}
			invalidated := &stubInvalidatedGameRepo{isInvalidatedFunc: func(ctx context.Context, gameID string) (bool, error) { return true, nil }}
			hub := newTestHub(t, hubDeps{})
			relay := newTestGameRelay(t, relayDeps{hub: hub, battleClient: battle, invalidatedGameRepo: invalidated})
			factory := newTestSocketFactory(t, hub)
			returningClient, _ := factory.connect(t, "player-1")
			relay.JoinGame("player-1", "game-1", 1)

			relay.resolveStaleDisconnect("game-1", "player-1", false)

			returningClient.expectNoMessage(t)
			assert.Empty(t, battle.processActionCallsSnapshot())
			_, ok := relay.GameIDForPlayer("player-1")
			assert.True(t, ok)
		})

		t.Run("対戦が無効かどうかの確認自体に失敗したとき、対戦を継続させる", func(t *testing.T) {
			battle := &stubBattleClient{}
			invalidated := &stubInvalidatedGameRepo{isInvalidatedFunc: func(ctx context.Context, gameID string) (bool, error) { return false, errors.New("check failed") }}
			hub := newTestHub(t, hubDeps{})
			relay := newTestGameRelay(t, relayDeps{hub: hub, battleClient: battle, invalidatedGameRepo: invalidated})
			factory := newTestSocketFactory(t, hub)
			returningClient, _ := factory.connect(t, "player-1")
			relay.JoinGame("player-1", "game-1", 1)

			relay.resolveStaleDisconnect("game-1", "player-1", false)

			returningClient.expectNoMessage(t)
			assert.Empty(t, battle.processActionCallsSnapshot())
			_, ok := relay.GameIDForPlayer("player-1")
			assert.True(t, ok)
		})

		t.Run("参加者記録の参照に失敗したとき、対戦を継続させる", func(t *testing.T) {
			battle := &stubBattleClient{}
			invalidated := &stubInvalidatedGameRepo{isInvalidatedFunc: func(ctx context.Context, gameID string) (bool, error) { return false, nil }}
			gamePlayers := &stubGamePlayerRepo{lookupGamePlayersFunc: func(ctx context.Context, gameID string) ([]port.GamePlayerEntry, error) {
				return nil, errors.New("lookup failed")
			}}
			hub := newTestHub(t, hubDeps{})
			relay := newTestGameRelay(t, relayDeps{hub: hub, battleClient: battle, invalidatedGameRepo: invalidated, gamePlayerRepo: gamePlayers})
			factory := newTestSocketFactory(t, hub)
			returningClient, _ := factory.connect(t, "player-1")
			relay.JoinGame("player-1", "game-1", 1)

			relay.resolveStaleDisconnect("game-1", "player-1", false)

			returningClient.expectNoMessage(t)
			assert.Empty(t, battle.processActionCallsSnapshot())
			_, ok := relay.GameIDForPlayer("player-1")
			assert.True(t, ok)
		})

		t.Run("その対戦に記録されている人間参加者が1人だけ(NPC対戦)のとき、対戦相手を特定できないため対戦を継続させる", func(t *testing.T) {
			battle := &stubBattleClient{}
			invalidated := &stubInvalidatedGameRepo{isInvalidatedFunc: func(ctx context.Context, gameID string) (bool, error) { return false, nil }}
			gamePlayers := &stubGamePlayerRepo{lookupGamePlayersFunc: func(ctx context.Context, gameID string) ([]port.GamePlayerEntry, error) {
				return []port.GamePlayerEntry{{PlayerNum: 1, PlayerID: "player-1"}}, nil
			}}
			hub := newTestHub(t, hubDeps{})
			relay := newTestGameRelay(t, relayDeps{hub: hub, battleClient: battle, invalidatedGameRepo: invalidated, gamePlayerRepo: gamePlayers})
			factory := newTestSocketFactory(t, hub)
			returningClient, _ := factory.connect(t, "player-1")
			relay.JoinGame("player-1", "game-1", 1)

			relay.resolveStaleDisconnect("game-1", "player-1", false)

			returningClient.expectNoMessage(t)
			assert.Empty(t, battle.processActionCallsSnapshot())
			_, ok := relay.GameIDForPlayer("player-1")
			assert.True(t, ok)
		})

		t.Run("対戦相手が現在接続中のとき、対戦を継続させる(通常の復帰として扱う)", func(t *testing.T) {
			battle := &stubBattleClient{}
			invalidated := &stubInvalidatedGameRepo{isInvalidatedFunc: func(ctx context.Context, gameID string) (bool, error) { return false, nil }}
			gamePlayers := &stubGamePlayerRepo{lookupGamePlayersFunc: func(ctx context.Context, gameID string) ([]port.GamePlayerEntry, error) {
				return []port.GamePlayerEntry{{PlayerNum: 1, PlayerID: "player-1"}, {PlayerNum: 2, PlayerID: "player-2"}}, nil
			}}
			hub := newTestHub(t, hubDeps{})
			relay := newTestGameRelay(t, relayDeps{hub: hub, battleClient: battle, invalidatedGameRepo: invalidated, gamePlayerRepo: gamePlayers})
			factory := newTestSocketFactory(t, hub)
			returningClient, _ := factory.connect(t, "player-1")
			factory.connect(t, "player-2")
			relay.JoinGame("player-1", "game-1", 1)
			relay.JoinGame("player-2", "game-1", 2)

			relay.resolveStaleDisconnect("game-1", "player-1", false)

			returningClient.expectNoMessage(t)
			assert.Empty(t, battle.processActionCallsSnapshot())
			_, ok := relay.GameIDForPlayer("player-1")
			assert.True(t, ok)
		})

		t.Run("対戦相手の切断猶予が既に切れているかどうかの確認に失敗したとき、対戦を継続させる", func(t *testing.T) {
			battle := &stubBattleClient{}
			invalidated := &stubInvalidatedGameRepo{isInvalidatedFunc: func(ctx context.Context, gameID string) (bool, error) { return false, nil }}
			gamePlayers := &stubGamePlayerRepo{lookupGamePlayersFunc: func(ctx context.Context, gameID string) ([]port.GamePlayerEntry, error) {
				return []port.GamePlayerEntry{{PlayerNum: 1, PlayerID: "player-1"}, {PlayerNum: 2, PlayerID: "player-2"}}, nil
			}}
			timerStore := &stubTimerStore{getDisconnectDeadlineFunc: func(ctx context.Context, id string) (port.DisconnectDeadline, bool, error) {
				return port.DisconnectDeadline{}, false, errors.New("store unavailable")
			}}
			hub := newTestHub(t, hubDeps{timerStore: timerStore})
			relay := newTestGameRelay(t, relayDeps{hub: hub, battleClient: battle, invalidatedGameRepo: invalidated, gamePlayerRepo: gamePlayers})
			factory := newTestSocketFactory(t, hub)
			returningClient, _ := factory.connect(t, "player-1")
			relay.JoinGame("player-1", "game-1", 1)

			relay.resolveStaleDisconnect("game-1", "player-1", false)

			returningClient.expectNoMessage(t)
			assert.Empty(t, battle.processActionCallsSnapshot())
			_, ok := relay.GameIDForPlayer("player-1")
			assert.True(t, ok)
		})

		t.Run("対戦相手の切断猶予がまだ残っているとき、対戦を継続させる", func(t *testing.T) {
			battle := &stubBattleClient{}
			invalidated := &stubInvalidatedGameRepo{isInvalidatedFunc: func(ctx context.Context, gameID string) (bool, error) { return false, nil }}
			gamePlayers := &stubGamePlayerRepo{lookupGamePlayersFunc: func(ctx context.Context, gameID string) ([]port.GamePlayerEntry, error) {
				return []port.GamePlayerEntry{{PlayerNum: 1, PlayerID: "player-1"}, {PlayerNum: 2, PlayerID: "player-2"}}, nil
			}}
			hub := newTestHub(t, hubDeps{timerStore: futureDeadlineTimerStore()})
			relay := newTestGameRelay(t, relayDeps{hub: hub, battleClient: battle, invalidatedGameRepo: invalidated, gamePlayerRepo: gamePlayers})
			factory := newTestSocketFactory(t, hub)
			returningClient, _ := factory.connect(t, "player-1")
			relay.JoinGame("player-1", "game-1", 1)

			relay.resolveStaleDisconnect("game-1", "player-1", false)

			returningClient.expectNoMessage(t)
			assert.Empty(t, battle.processActionCallsSnapshot())
			_, ok := relay.GameIDForPlayer("player-1")
			assert.True(t, ok)
		})

		t.Run("対戦相手の切断猶予だけが切れており、復帰した本人は自分の猶予内に戻れていたとき、対戦相手を敗者として対戦を終了させる。終了理由は「切断」になる", func(t *testing.T) {
			battle := &stubBattleClient{processActionFunc: func(ctx context.Context, gameID string, playerNum int, actionType string, data json.RawMessage) (*service.ActionResult, error) {
				return &service.ActionResult{GameOver: true, WinningPlayerNum: 1, WinReason: "budget_zero"}, nil
			}}
			account := &stubAccountClient{}
			invalidated := &stubInvalidatedGameRepo{isInvalidatedFunc: func(ctx context.Context, gameID string) (bool, error) { return false, nil }}
			gamePlayers := &stubGamePlayerRepo{
				lookupGamePlayersFunc: func(ctx context.Context, gameID string) ([]port.GamePlayerEntry, error) {
					return []port.GamePlayerEntry{{PlayerNum: 1, PlayerID: "player-1"}, {PlayerNum: 2, PlayerID: "player-2"}}, nil
				},
				markExpAwardedFunc: func(ctx context.Context, gameID string) (bool, error) { return true, nil },
			}
			hub := newTestHub(t, hubDeps{timerStore: expiredDeadlineTimerStore()})
			relay := newTestGameRelay(t, relayDeps{hub: hub, battleClient: battle, accountClient: account, invalidatedGameRepo: invalidated, gamePlayerRepo: gamePlayers})
			factory := newTestSocketFactory(t, hub)
			returningClient, _ := factory.connect(t, "player-1")
			relay.JoinGame("player-1", "game-1", 1)

			relay.resolveStaleDisconnect("game-1", "player-1", false)

			msg := returningClient.readMessage(t)
			assert.Equal(t, wsconst.WSServerMsgGameOver, msg.Type)
			var payload GameOverMessage
			require.NoError(t, json.Unmarshal(msg.Data, &payload))
			assert.Equal(t, "disconnect", payload.WinReason)
			calls := battle.processActionCallsSnapshot()
			require.Len(t, calls, 1)
			assert.Equal(t, 2, calls[0].playerNum)
		})

		t.Run("復帰した本人も自分の猶予を過ぎてから戻ってきており、かつ復帰した本人がその対戦のどのスロットにも記録されていない(データ不整合)とき、対戦を継続させる", func(t *testing.T) {
			battle := &stubBattleClient{}
			invalidated := &stubInvalidatedGameRepo{isInvalidatedFunc: func(ctx context.Context, gameID string) (bool, error) { return false, nil }}
			gamePlayers := &stubGamePlayerRepo{lookupGamePlayersFunc: func(ctx context.Context, gameID string) ([]port.GamePlayerEntry, error) {
				return []port.GamePlayerEntry{{PlayerNum: 2, PlayerID: "player-2"}}, nil
			}}
			hub := newTestHub(t, hubDeps{timerStore: expiredDeadlineTimerStore()})
			relay := newTestGameRelay(t, relayDeps{hub: hub, battleClient: battle, invalidatedGameRepo: invalidated, gamePlayerRepo: gamePlayers})
			factory := newTestSocketFactory(t, hub)
			returningClient, _ := factory.connect(t, "player-1")
			relay.JoinGame("player-1", "game-1", 1)

			relay.resolveStaleDisconnect("game-1", "player-1", true)

			returningClient.expectNoMessage(t)
			assert.Empty(t, battle.processActionCallsSnapshot())
			_, ok := relay.GameIDForPlayer("player-1")
			assert.True(t, ok)
		})

		t.Run("対戦相手の猶予・復帰した本人の猶予の両方が切れているとき、どちらか一方を一方的な敗者とせず対戦を終了させる。終了理由は対戦を管理する側が判定したものになる(切断固定にはならない)", func(t *testing.T) {
			battle := &stubBattleClient{processActionFunc: func(ctx context.Context, gameID string, playerNum int, actionType string, data json.RawMessage) (*service.ActionResult, error) {
				return &service.ActionResult{GameOver: true, WinningPlayerNum: 2, WinReason: "mutual_timeout"}, nil
			}}
			account := &stubAccountClient{}
			invalidated := &stubInvalidatedGameRepo{isInvalidatedFunc: func(ctx context.Context, gameID string) (bool, error) { return false, nil }}
			gamePlayers := &stubGamePlayerRepo{
				lookupGamePlayersFunc: func(ctx context.Context, gameID string) ([]port.GamePlayerEntry, error) {
					return []port.GamePlayerEntry{{PlayerNum: 1, PlayerID: "player-1"}, {PlayerNum: 2, PlayerID: "player-2"}}, nil
				},
				markExpAwardedFunc: func(ctx context.Context, gameID string) (bool, error) { return true, nil },
			}
			hub := newTestHub(t, hubDeps{timerStore: expiredDeadlineTimerStore()})
			relay := newTestGameRelay(t, relayDeps{hub: hub, battleClient: battle, accountClient: account, invalidatedGameRepo: invalidated, gamePlayerRepo: gamePlayers})
			factory := newTestSocketFactory(t, hub)
			returningClient, _ := factory.connect(t, "player-1")
			relay.JoinGame("player-1", "game-1", 1)

			relay.resolveStaleDisconnect("game-1", "player-1", true)

			msg := returningClient.readMessage(t)
			assert.Equal(t, wsconst.WSServerMsgGameOver, msg.Type)
			var payload GameOverMessage
			require.NoError(t, json.Unmarshal(msg.Data, &payload))
			assert.Equal(t, "mutual_timeout", payload.WinReason)
		})

		t.Run("対戦を管理する側への強制決着の要求自体が失敗したとき、対戦を継続させる", func(t *testing.T) {
			battle := &stubBattleClient{processActionFunc: func(ctx context.Context, gameID string, playerNum int, actionType string, data json.RawMessage) (*service.ActionResult, error) {
				return nil, errors.New("forfeit request failed")
			}}
			invalidated := &stubInvalidatedGameRepo{isInvalidatedFunc: func(ctx context.Context, gameID string) (bool, error) { return false, nil }}
			gamePlayers := &stubGamePlayerRepo{lookupGamePlayersFunc: func(ctx context.Context, gameID string) ([]port.GamePlayerEntry, error) {
				return []port.GamePlayerEntry{{PlayerNum: 1, PlayerID: "player-1"}, {PlayerNum: 2, PlayerID: "player-2"}}, nil
			}}
			hub := newTestHub(t, hubDeps{timerStore: expiredDeadlineTimerStore()})
			relay := newTestGameRelay(t, relayDeps{hub: hub, battleClient: battle, invalidatedGameRepo: invalidated, gamePlayerRepo: gamePlayers})
			factory := newTestSocketFactory(t, hub)
			returningClient, _ := factory.connect(t, "player-1")
			relay.JoinGame("player-1", "game-1", 1)

			relay.resolveStaleDisconnect("game-1", "player-1", false)

			returningClient.expectNoMessage(t)
			_, ok := relay.GameIDForPlayer("player-1")
			assert.True(t, ok)
		})

		t.Run("強制決着が成立し対戦が終了したとき、対戦を終了させる", func(t *testing.T) {
			battle := &stubBattleClient{processActionFunc: func(ctx context.Context, gameID string, playerNum int, actionType string, data json.RawMessage) (*service.ActionResult, error) {
				return &service.ActionResult{GameOver: true, WinningPlayerNum: 1, WinReason: "budget_zero"}, nil
			}}
			account := &stubAccountClient{}
			invalidated := &stubInvalidatedGameRepo{isInvalidatedFunc: func(ctx context.Context, gameID string) (bool, error) { return false, nil }}
			gamePlayers := &stubGamePlayerRepo{
				lookupGamePlayersFunc: func(ctx context.Context, gameID string) ([]port.GamePlayerEntry, error) {
					return []port.GamePlayerEntry{{PlayerNum: 1, PlayerID: "player-1"}, {PlayerNum: 2, PlayerID: "player-2"}}, nil
				},
				markExpAwardedFunc: func(ctx context.Context, gameID string) (bool, error) { return true, nil },
			}
			hub := newTestHub(t, hubDeps{timerStore: expiredDeadlineTimerStore()})
			relay := newTestGameRelay(t, relayDeps{hub: hub, battleClient: battle, accountClient: account, invalidatedGameRepo: invalidated, gamePlayerRepo: gamePlayers})
			factory := newTestSocketFactory(t, hub)
			returningClient, _ := factory.connect(t, "player-1")
			relay.JoinGame("player-1", "game-1", 1)

			relay.resolveStaleDisconnect("game-1", "player-1", false)

			returningClient.readMessage(t)
			_, ok := relay.GameIDForPlayer("player-1")
			assert.False(t, ok)
		})
	})
}

func TestHandleReconnect(t *testing.T) {
	t.Run("プレイヤー復帰時の通知と未決着解消のきっかけ", func(t *testing.T) {
		t.Run("プレイヤーが切断していた対戦に復帰したとき、対戦相手へ「対戦相手が復帰した」ことを知らせるメッセージが届く", func(t *testing.T) {
			invalidated := &stubInvalidatedGameRepo{isInvalidatedFunc: func(ctx context.Context, gameID string) (bool, error) { return true, nil }}
			hub := newTestHub(t, hubDeps{})
			relay := newTestGameRelay(t, relayDeps{hub: hub, invalidatedGameRepo: invalidated})
			factory := newTestSocketFactory(t, hub)
			factory.connect(t, "player-1")
			opponentClient, _ := factory.connect(t, "player-2")
			relay.JoinGame("player-1", "game-1", 1)
			relay.JoinGame("player-2", "game-1", 2)

			relay.HandleReconnect("player-1", "game-1", false)

			assert.Equal(t, wsconst.WSServerMsgOpponentReconnected, opponentClient.readMessage(t).Type)
		})

		t.Run("続けて、その対戦の未決着状態を評価する処理が行われる。この評価結果は、復帰通知そのものより遅れて届くことがある", func(t *testing.T) {
			battle := &stubBattleClient{processActionFunc: func(ctx context.Context, gameID string, playerNum int, actionType string, data json.RawMessage) (*service.ActionResult, error) {
				return &service.ActionResult{GameOver: true, WinningPlayerNum: 1, WinReason: "budget_zero"}, nil
			}}
			account := &stubAccountClient{}
			invalidated := &stubInvalidatedGameRepo{isInvalidatedFunc: func(ctx context.Context, gameID string) (bool, error) { return false, nil }}
			gamePlayers := &stubGamePlayerRepo{
				lookupGamePlayersFunc: func(ctx context.Context, gameID string) ([]port.GamePlayerEntry, error) {
					return []port.GamePlayerEntry{{PlayerNum: 1, PlayerID: "player-1"}, {PlayerNum: 2, PlayerID: "player-2"}}, nil
				},
				markExpAwardedFunc: func(ctx context.Context, gameID string) (bool, error) { return true, nil },
			}
			hub := newTestHub(t, hubDeps{timerStore: expiredDeadlineTimerStore()})
			relay := newTestGameRelay(t, relayDeps{hub: hub, battleClient: battle, accountClient: account, invalidatedGameRepo: invalidated, gamePlayerRepo: gamePlayers})
			factory := newTestSocketFactory(t, hub)
			returningClient, _ := factory.connect(t, "player-1")
			relay.JoinGame("player-1", "game-1", 1)

			relay.HandleReconnect("player-1", "game-1", false)

			msg := returningClient.readMessage(t)
			assert.Equal(t, wsconst.WSServerMsgGameOver, msg.Type)
			var payload GameOverMessage
			require.NoError(t, json.Unmarshal(msg.Data, &payload))
			assert.Equal(t, "disconnect", payload.WinReason)
		})
	})
}

func TestRunNpcTurns(t *testing.T) {
	t.Run("NPCの連続ターン進行", func(t *testing.T) {
		t.Run("直前の処理結果が「NPCの手番が続けて必要」を示していない、または既に対戦が終了しているとき、それ以上NPCの行動を進めず、進行を終える", func(t *testing.T) {
			cases := []struct {
				name    string
				current *service.ActionResult
			}{
				{"NPCの手番が続けて必要ではないとき", &service.ActionResult{NpcPending: false, GameOver: false}},
				{"NPCの手番が続けて必要であっても、既に対戦が終了しているとき", &service.ActionResult{NpcPending: true, GameOver: true}},
			}
			for _, tt := range cases {
				t.Run(tt.name, func(t *testing.T) {
					battle := &stubBattleClient{}
					hub := newTestHub(t, hubDeps{})
					relay := newTestGameRelay(t, relayDeps{hub: hub, battleClient: battle})
					factory := newTestSocketFactory(t, hub)
					client, _ := factory.connect(t, "player-1")
					relay.JoinGame("player-1", "game-1", 1)

					result := relay.runNpcTurns(context.Background(), "game-1", "player-1", tt.current)

					assert.Same(t, tt.current, result)
					assert.Empty(t, battle.advanceNpcTurnCallsSnapshot())
					client.expectNoMessage(t)
				})
			}
		})

		t.Run("NPCの手番進行が必要な間、1手ごとに進行を要求し、その結果のイベントは行動結果イベントの配信先振り分けの規定に従って配信される(システム由来のイベントであれば、その時点の参加登録者全員に各自の視点の対戦状態とともに届く)", func(t *testing.T) {
			battle := &stubBattleClient{
				advanceNpcTurnFunc: func(ctx context.Context, gameID string) (*service.ActionResult, error) {
					return &service.ActionResult{NpcPending: false, Events: []service.ActionEvent{{Sequence: 5, EventType: "turn_start", PlayerNum: nil}}}, nil
				},
				getGameStateForPlayerFunc: func(ctx context.Context, gameID string, playerNum int) (json.RawMessage, error) {
					return stateMarkerFor(playerNum), nil
				},
			}
			hub := newTestHub(t, hubDeps{})
			relay := newTestGameRelay(t, relayDeps{hub: hub, battleClient: battle})
			factory := newTestSocketFactory(t, hub)
			client, _ := factory.connect(t, "player-1")
			relay.JoinGame("player-1", "game-1", 1)
			current := &service.ActionResult{NpcPending: true}

			result := relay.runNpcTurns(context.Background(), "game-1", "player-1", current)

			require.Equal(t, []string{"game-1"}, battle.advanceNpcTurnCallsSnapshot())
			msg := client.readMessage(t)
			assert.Equal(t, wsconst.WSServerMsgActionPerformed, msg.Type)
			var payload ActionPerformedMessage
			require.NoError(t, json.Unmarshal(msg.Data, &payload))
			assert.Equal(t, int64(5), payload.Sequence)
			assert.False(t, result.NpcPending)
		})

		t.Run("NPCの手番進行の要求が、進行を依頼した人間プレイヤーの接続が失われたことによって中断されたとき、それ以上進めずに進行を終え、誰にもエラーは届かない", func(t *testing.T) {
			battle := &stubBattleClient{advanceNpcTurnFunc: func(ctx context.Context, gameID string) (*service.ActionResult, error) {
				return nil, context.Canceled
			}}
			hub := newTestHub(t, hubDeps{})
			relay := newTestGameRelay(t, relayDeps{hub: hub, battleClient: battle})
			factory := newTestSocketFactory(t, hub)
			client, _ := factory.connect(t, "player-1")
			relay.JoinGame("player-1", "game-1", 1)
			current := &service.ActionResult{NpcPending: true}

			relay.runNpcTurns(context.Background(), "game-1", "player-1", current)

			require.Len(t, battle.advanceNpcTurnCallsSnapshot(), 1)
			client.expectNoMessage(t)
		})

		t.Run("NPCの手番進行の要求が接続断以外の理由で失敗したとき、それ以上進めずに進行を終え、進行を依頼した人間プレイヤーにエラーが届く", func(t *testing.T) {
			battle := &stubBattleClient{advanceNpcTurnFunc: func(ctx context.Context, gameID string) (*service.ActionResult, error) {
				return nil, errors.New("advance npc turn failed")
			}}
			hub := newTestHub(t, hubDeps{})
			relay := newTestGameRelay(t, relayDeps{hub: hub, battleClient: battle})
			factory := newTestSocketFactory(t, hub)
			client, _ := factory.connect(t, "player-1")
			relay.JoinGame("player-1", "game-1", 1)
			current := &service.ActionResult{NpcPending: true}

			relay.runNpcTurns(context.Background(), "game-1", "player-1", current)

			require.Len(t, battle.advanceNpcTurnCallsSnapshot(), 1)
			msg := client.readMessage(t)
			assert.Equal(t, wsconst.WSServerMsgError, msg.Type)
		})

		t.Run("NPCの連続ターン進行が単一の行動に続けて200回に達すると、それ以上の進行を試みずに200回目までの結果で進行を終える。この打ち切りでは進行を依頼した人間プレイヤーに追加のエラーは届かない", func(t *testing.T) {
			battle := &stubBattleClient{advanceNpcTurnFunc: func(ctx context.Context, gameID string) (*service.ActionResult, error) {
				return &service.ActionResult{NpcPending: true}, nil
			}}
			hub := newTestHub(t, hubDeps{})
			relay := newTestGameRelay(t, relayDeps{hub: hub, battleClient: battle})
			factory := newTestSocketFactory(t, hub)
			client, _ := factory.connect(t, "player-1")
			relay.JoinGame("player-1", "game-1", 1)
			current := &service.ActionResult{NpcPending: true}

			relay.runNpcTurns(context.Background(), "game-1", "player-1", current)

			assert.Len(t, battle.advanceNpcTurnCallsSnapshot(), 200)
			client.expectNoMessage(t)
		})

		t.Run("NPCの手番が不要になった、または対戦が終了したとき、進行を終える", func(t *testing.T) {
			var calls int
			battle := &stubBattleClient{advanceNpcTurnFunc: func(ctx context.Context, gameID string) (*service.ActionResult, error) {
				calls++
				if calls == 1 {
					return &service.ActionResult{NpcPending: true}, nil
				}
				return &service.ActionResult{NpcPending: false, GameOver: true}, nil
			}}
			hub := newTestHub(t, hubDeps{})
			relay := newTestGameRelay(t, relayDeps{hub: hub, battleClient: battle})
			factory := newTestSocketFactory(t, hub)
			factory.connect(t, "player-1")
			relay.JoinGame("player-1", "game-1", 1)
			current := &service.ActionResult{NpcPending: true}

			result := relay.runNpcTurns(context.Background(), "game-1", "player-1", current)

			assert.Len(t, battle.advanceNpcTurnCallsSnapshot(), 2)
			assert.True(t, result.GameOver)
		})
	})
}

func rawBattleState(t *testing.T, fields map[string]interface{}) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(fields)
	require.NoError(t, err)
	return data
}

func pvpBattleState(t *testing.T, myPlayerNum int, myLevel, oppLevel *int64, currentTurn int64, isMyTurn bool) json.RawMessage {
	t.Helper()
	oppPlayerNum := 1
	if myPlayerNum == 1 {
		oppPlayerNum = 2
	}
	summaries := map[string]interface{}{
		"player" + itoa(myPlayerNum) + "Summary":  map[string]interface{}{"name": "自分", "level": myLevel},
		"player" + itoa(oppPlayerNum) + "Summary": map[string]interface{}{"name": "相手", "level": oppLevel},
		"myView":      map[string]interface{}{"playerNum": myPlayerNum},
		"oppView":     map[string]interface{}{"playerNum": oppPlayerNum},
		"currentTurn": currentTurn,
		"isMyTurn":    isMyTurn,
	}
	return rawBattleState(t, summaries)
}

func itoa(n int) string {
	if n == 1 {
		return "1"
	}
	return "2"
}

func TestSendBattleStartAndTurnStart(t *testing.T) {
	t.Run("参加時の演出イベント(対戦開始・ターン開始)配信", func(t *testing.T) {
		t.Run("参加しようとしている本人のスロット番号が特定できないとき、本人に参加登録が無いことを示すエラーが届き、演出イベントは届かない", func(t *testing.T) {
			gamePlayers := &stubGamePlayerRepo{lookupGamePlayersFunc: func(ctx context.Context, gameID string) ([]port.GamePlayerEntry, error) {
				return []port.GamePlayerEntry{}, nil
			}}
			hub := newTestHub(t, hubDeps{})
			relay := newTestGameRelay(t, relayDeps{hub: hub, gamePlayerRepo: gamePlayers})
			factory := newTestSocketFactory(t, hub)
			client, conn := factory.connect(t, "player-1")

			relay.sendBattleStartAndTurnStart(conn, "game-1")

			msg := client.readMessage(t)
			assert.Equal(t, wsconst.WSServerMsgError, msg.Type)
			var payload ErrorMessage
			require.NoError(t, json.Unmarshal(msg.Data, &payload))
			assert.Equal(t, "game_error", payload.ErrorCode)
			assert.Equal(t, "player not in game", payload.Message)
		})

		t.Run("対戦状態の取得が本人の接続断によって中断されたとき、何も届かない", func(t *testing.T) {
			battle := &stubBattleClient{getGameStateForPlayerFunc: func(ctx context.Context, gameID string, playerNum int) (json.RawMessage, error) {
				return nil, context.Canceled
			}}
			hub := newTestHub(t, hubDeps{})
			relay := newTestGameRelay(t, relayDeps{hub: hub, battleClient: battle})
			factory := newTestSocketFactory(t, hub)
			client, conn := factory.connect(t, "player-1")
			relay.JoinGame("player-1", "game-1", 1)

			relay.sendBattleStartAndTurnStart(conn, "game-1")

			client.expectNoMessage(t)
		})

		t.Run("対戦状態の取得が接続断以外の理由で失敗したとき、本人にエラーが届く", func(t *testing.T) {
			battle := &stubBattleClient{getGameStateForPlayerFunc: func(ctx context.Context, gameID string, playerNum int) (json.RawMessage, error) {
				return nil, errors.New("get game state failed")
			}}
			hub := newTestHub(t, hubDeps{})
			relay := newTestGameRelay(t, relayDeps{hub: hub, battleClient: battle})
			factory := newTestSocketFactory(t, hub)
			client, conn := factory.connect(t, "player-1")
			relay.JoinGame("player-1", "game-1", 1)

			relay.sendBattleStartAndTurnStart(conn, "game-1")

			msg := client.readMessage(t)
			assert.Equal(t, wsconst.WSServerMsgError, msg.Type)
			var payload ErrorMessage
			require.NoError(t, json.Unmarshal(msg.Data, &payload))
			assert.Equal(t, "game_state_error", payload.ErrorCode)
			assert.Equal(t, "failed to retrieve game state", payload.Message)
		})

		t.Run("対戦の参加者記録の参照が本人の接続断によって中断されたとき、何も届かない", func(t *testing.T) {
			state := pvpBattleState(t, 1, int64Ptr(3), int64Ptr(4), 1, true)
			battle := &stubBattleClient{getGameStateForPlayerFunc: func(ctx context.Context, gameID string, playerNum int) (json.RawMessage, error) {
				return state, nil
			}}
			gamePlayers := &stubGamePlayerRepo{lookupGamePlayersFunc: func(ctx context.Context, gameID string) ([]port.GamePlayerEntry, error) {
				return nil, context.Canceled
			}}
			hub := newTestHub(t, hubDeps{})
			relay := newTestGameRelay(t, relayDeps{hub: hub, battleClient: battle, gamePlayerRepo: gamePlayers})
			factory := newTestSocketFactory(t, hub)
			client, conn := factory.connect(t, "player-1")
			relay.JoinGame("player-1", "game-1", 1)

			relay.sendBattleStartAndTurnStart(conn, "game-1")

			client.expectNoMessage(t)
		})

		t.Run("対戦の参加者記録の参照が接続断以外の理由で失敗したとき、本人にエラーが届く", func(t *testing.T) {
			state := pvpBattleState(t, 1, int64Ptr(3), int64Ptr(4), 1, true)
			battle := &stubBattleClient{getGameStateForPlayerFunc: func(ctx context.Context, gameID string, playerNum int) (json.RawMessage, error) {
				return state, nil
			}}
			gamePlayers := &stubGamePlayerRepo{lookupGamePlayersFunc: func(ctx context.Context, gameID string) ([]port.GamePlayerEntry, error) {
				return nil, errors.New("lookup game players failed")
			}}
			hub := newTestHub(t, hubDeps{})
			relay := newTestGameRelay(t, relayDeps{hub: hub, battleClient: battle, gamePlayerRepo: gamePlayers})
			factory := newTestSocketFactory(t, hub)
			client, conn := factory.connect(t, "player-1")
			relay.JoinGame("player-1", "game-1", 1)

			relay.sendBattleStartAndTurnStart(conn, "game-1")

			msg := client.readMessage(t)
			assert.Equal(t, wsconst.WSServerMsgError, msg.Type)
			var payload ErrorMessage
			require.NoError(t, json.Unmarshal(msg.Data, &payload))
			assert.Equal(t, "game_state_error", payload.ErrorCode)
			assert.Equal(t, "failed to retrieve game metadata", payload.Message)
		})

		t.Run("そのゲームに記録されている人間参加者が自分1人だけのとき、NPC対戦として扱われる", func(t *testing.T) {
			state := pvpBattleState(t, 1, int64Ptr(3), nil, 1, true)
			battle := &stubBattleClient{getGameStateForPlayerFunc: func(ctx context.Context, gameID string, playerNum int) (json.RawMessage, error) {
				return state, nil
			}}
			gamePlayers := &stubGamePlayerRepo{lookupGamePlayersFunc: func(ctx context.Context, gameID string) ([]port.GamePlayerEntry, error) {
				return []port.GamePlayerEntry{{PlayerNum: 1, PlayerID: "player-1"}}, nil
			}}
			hub := newTestHub(t, hubDeps{})
			relay := newTestGameRelay(t, relayDeps{hub: hub, battleClient: battle, gamePlayerRepo: gamePlayers})
			factory := newTestSocketFactory(t, hub)
			client, conn := factory.connect(t, "player-1")
			relay.JoinGame("player-1", "game-1", 1)

			relay.sendBattleStartAndTurnStart(conn, "game-1")

			msg := client.readMessage(t)
			require.Equal(t, wsconst.WSServerMsgActionPerformed, msg.Type)
			var payload ActionPerformedMessage
			require.NoError(t, json.Unmarshal(msg.Data, &payload))
			var raw map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(payload.ActionData, &raw))
			assert.JSONEq(t, `"npc"`, string(raw["match_type"]))
		})

		t.Run("そのゲームに記録されている人間参加者が2人のとき、PvP対戦として扱われる", func(t *testing.T) {
			state := pvpBattleState(t, 1, int64Ptr(3), int64Ptr(4), 1, true)
			battle := &stubBattleClient{getGameStateForPlayerFunc: func(ctx context.Context, gameID string, playerNum int) (json.RawMessage, error) {
				return state, nil
			}}
			gamePlayers := &stubGamePlayerRepo{lookupGamePlayersFunc: func(ctx context.Context, gameID string) ([]port.GamePlayerEntry, error) {
				return []port.GamePlayerEntry{{PlayerNum: 1, PlayerID: "player-1"}, {PlayerNum: 2, PlayerID: "player-2"}}, nil
			}}
			hub := newTestHub(t, hubDeps{})
			relay := newTestGameRelay(t, relayDeps{hub: hub, battleClient: battle, gamePlayerRepo: gamePlayers})
			factory := newTestSocketFactory(t, hub)
			client, conn := factory.connect(t, "player-1")
			relay.JoinGame("player-1", "game-1", 1)

			relay.sendBattleStartAndTurnStart(conn, "game-1")

			msg := client.readMessage(t)
			require.Equal(t, wsconst.WSServerMsgActionPerformed, msg.Type)
			var payload ActionPerformedMessage
			require.NoError(t, json.Unmarshal(msg.Data, &payload))
			var raw map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(payload.ActionData, &raw))
			assert.JSONEq(t, `"pvp"`, string(raw["match_type"]))
		})

		t.Run("そのゲームに記録されている人間参加者が1人でも2人でもない(0人・3人以上)とき、想定しない状態として本人にエラーが届く", func(t *testing.T) {
			state := pvpBattleState(t, 1, int64Ptr(3), int64Ptr(4), 1, true)
			battle := &stubBattleClient{getGameStateForPlayerFunc: func(ctx context.Context, gameID string, playerNum int) (json.RawMessage, error) {
				return state, nil
			}}
			gamePlayers := &stubGamePlayerRepo{lookupGamePlayersFunc: func(ctx context.Context, gameID string) ([]port.GamePlayerEntry, error) {
				return []port.GamePlayerEntry{}, nil
			}}
			hub := newTestHub(t, hubDeps{})
			relay := newTestGameRelay(t, relayDeps{hub: hub, battleClient: battle, gamePlayerRepo: gamePlayers})
			factory := newTestSocketFactory(t, hub)
			client, conn := factory.connect(t, "player-1")
			relay.JoinGame("player-1", "game-1", 1)

			relay.sendBattleStartAndTurnStart(conn, "game-1")

			msg := client.readMessage(t)
			assert.Equal(t, wsconst.WSServerMsgError, msg.Type)
			var payload ErrorMessage
			require.NoError(t, json.Unmarshal(msg.Data, &payload))
			assert.Equal(t, "game_state_error", payload.ErrorCode)
			assert.Equal(t, "failed to retrieve game metadata", payload.Message)
		})

		t.Run("取得した対戦状態の解析に失敗したとき、本人にエラーが届く", func(t *testing.T) {
			battle := &stubBattleClient{getGameStateForPlayerFunc: func(ctx context.Context, gameID string, playerNum int) (json.RawMessage, error) {
				return json.RawMessage(`not-json`), nil
			}}
			gamePlayers := &stubGamePlayerRepo{lookupGamePlayersFunc: func(ctx context.Context, gameID string) ([]port.GamePlayerEntry, error) {
				return []port.GamePlayerEntry{{PlayerNum: 1, PlayerID: "player-1"}, {PlayerNum: 2, PlayerID: "player-2"}}, nil
			}}
			hub := newTestHub(t, hubDeps{})
			relay := newTestGameRelay(t, relayDeps{hub: hub, battleClient: battle, gamePlayerRepo: gamePlayers})
			factory := newTestSocketFactory(t, hub)
			client, conn := factory.connect(t, "player-1")
			relay.JoinGame("player-1", "game-1", 1)

			relay.sendBattleStartAndTurnStart(conn, "game-1")

			msg := client.readMessage(t)
			assert.Equal(t, wsconst.WSServerMsgError, msg.Type)
			var payload ErrorMessage
			require.NoError(t, json.Unmarshal(msg.Data, &payload))
			assert.Equal(t, "game_state_error", payload.ErrorCode)
			assert.Equal(t, "failed to parse game state", payload.Message)
		})

		t.Run("参加しようとしている本人のスロット番号が特定でき、対戦状態の取得と対戦の参加者記録の参照がいずれも成功し、取得した対戦状態の解析にも成功したとき、本人に、対戦開始を表す演出イベントが1件届く。内容は自分の名前・レベル、相手の名前・レベル、対戦種別を含む", func(t *testing.T) {
			state := pvpBattleState(t, 2, int64Ptr(7), int64Ptr(9), 1, true)
			battle := &stubBattleClient{getGameStateForPlayerFunc: func(ctx context.Context, gameID string, playerNum int) (json.RawMessage, error) {
				return state, nil
			}}
			gamePlayers := &stubGamePlayerRepo{lookupGamePlayersFunc: func(ctx context.Context, gameID string) ([]port.GamePlayerEntry, error) {
				return []port.GamePlayerEntry{{PlayerNum: 1, PlayerID: "player-1"}, {PlayerNum: 2, PlayerID: "player-2"}}, nil
			}}
			hub := newTestHub(t, hubDeps{})
			relay := newTestGameRelay(t, relayDeps{hub: hub, battleClient: battle, gamePlayerRepo: gamePlayers})
			factory := newTestSocketFactory(t, hub)
			client, conn := factory.connect(t, "player-2")
			relay.JoinGame("player-2", "game-1", 2)

			relay.sendBattleStartAndTurnStart(conn, "game-1")

			msg := client.readMessage(t)
			require.Equal(t, wsconst.WSServerMsgActionPerformed, msg.Type)
			var payload ActionPerformedMessage
			require.NoError(t, json.Unmarshal(msg.Data, &payload))
			var raw map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(payload.ActionData, &raw))
			assert.JSONEq(t, `"自分"`, string(raw["my_name"]))
			assert.JSONEq(t, `7`, string(raw["my_level"]))
			assert.JSONEq(t, `"相手"`, string(raw["opponent_name"]))
			assert.JSONEq(t, `9`, string(raw["opponent_level"]))
			assert.JSONEq(t, `"pvp"`, string(raw["match_type"]))
		})

		t.Run("自分または相手がレベルを持たない(NPC)とき、レベルを持たない側のレベルはnull値として届く", func(t *testing.T) {
			state := pvpBattleState(t, 2, int64Ptr(7), nil, 1, true)
			battle := &stubBattleClient{getGameStateForPlayerFunc: func(ctx context.Context, gameID string, playerNum int) (json.RawMessage, error) {
				return state, nil
			}}
			gamePlayers := &stubGamePlayerRepo{lookupGamePlayersFunc: func(ctx context.Context, gameID string) ([]port.GamePlayerEntry, error) {
				return []port.GamePlayerEntry{{PlayerNum: 2, PlayerID: "player-2"}}, nil
			}}
			hub := newTestHub(t, hubDeps{})
			relay := newTestGameRelay(t, relayDeps{hub: hub, battleClient: battle, gamePlayerRepo: gamePlayers})
			factory := newTestSocketFactory(t, hub)
			client, conn := factory.connect(t, "player-2")
			relay.JoinGame("player-2", "game-1", 2)

			relay.sendBattleStartAndTurnStart(conn, "game-1")

			msg := client.readMessage(t)
			require.Equal(t, wsconst.WSServerMsgActionPerformed, msg.Type)
			var payload ActionPerformedMessage
			require.NoError(t, json.Unmarshal(msg.Data, &payload))
			var raw map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(payload.ActionData, &raw))
			assert.JSONEq(t, `null`, string(raw["opponent_level"]))
		})

		t.Run("この対戦開始イベントは、対戦アクションの通し番号とは別扱いの、常に 0 という通し番号を持つ", func(t *testing.T) {
			state := pvpBattleState(t, 1, int64Ptr(3), int64Ptr(4), 1, true)
			battle := &stubBattleClient{getGameStateForPlayerFunc: func(ctx context.Context, gameID string, playerNum int) (json.RawMessage, error) {
				return state, nil
			}}
			gamePlayers := &stubGamePlayerRepo{lookupGamePlayersFunc: func(ctx context.Context, gameID string) ([]port.GamePlayerEntry, error) {
				return []port.GamePlayerEntry{{PlayerNum: 1, PlayerID: "player-1"}, {PlayerNum: 2, PlayerID: "player-2"}}, nil
			}}
			hub := newTestHub(t, hubDeps{})
			relay := newTestGameRelay(t, relayDeps{hub: hub, battleClient: battle, gamePlayerRepo: gamePlayers})
			factory := newTestSocketFactory(t, hub)
			client, conn := factory.connect(t, "player-1")
			relay.JoinGame("player-1", "game-1", 1)

			relay.sendBattleStartAndTurnStart(conn, "game-1")

			msg := client.readMessage(t)
			var payload ActionPerformedMessage
			require.NoError(t, json.Unmarshal(msg.Data, &payload))
			assert.Zero(t, payload.Sequence)
		})

		t.Run("現在のターンに関するメタ情報が読み取れたとき、本人に、現在のターン数と「今が自分の手番かどうか」を含むターン開始の演出イベントがもう1件届く。これも対戦アクションの通し番号とは別扱いの、常に 0 という通し番号を持つ", func(t *testing.T) {
			state := pvpBattleState(t, 1, int64Ptr(3), int64Ptr(4), 7, true)
			battle := &stubBattleClient{getGameStateForPlayerFunc: func(ctx context.Context, gameID string, playerNum int) (json.RawMessage, error) {
				return state, nil
			}}
			gamePlayers := &stubGamePlayerRepo{lookupGamePlayersFunc: func(ctx context.Context, gameID string) ([]port.GamePlayerEntry, error) {
				return []port.GamePlayerEntry{{PlayerNum: 1, PlayerID: "player-1"}, {PlayerNum: 2, PlayerID: "player-2"}}, nil
			}}
			hub := newTestHub(t, hubDeps{})
			relay := newTestGameRelay(t, relayDeps{hub: hub, battleClient: battle, gamePlayerRepo: gamePlayers})
			factory := newTestSocketFactory(t, hub)
			client, conn := factory.connect(t, "player-1")
			relay.JoinGame("player-1", "game-1", 1)

			relay.sendBattleStartAndTurnStart(conn, "game-1")

			client.readMessage(t)
			msg := client.readMessage(t)
			require.Equal(t, wsconst.WSServerMsgActionPerformed, msg.Type)
			var payload ActionPerformedMessage
			require.NoError(t, json.Unmarshal(msg.Data, &payload))
			assert.Zero(t, payload.Sequence)
			var raw map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(payload.ActionData, &raw))
			assert.JSONEq(t, `7`, string(raw["turn"]))
			assert.JSONEq(t, `true`, string(raw["is_my_turn"]))
		})
	})
}

func TestAdvanceNpcIfNeeded(t *testing.T) {
	t.Run("参加時のNPC自動進行", func(t *testing.T) {
		t.Run("対戦の記録上の人間参加者が2人(PvP対戦)のとき、NPCの自動進行は行われない", func(t *testing.T) {
			battle := &stubBattleClient{}
			gamePlayers := &stubGamePlayerRepo{lookupGamePlayersFunc: func(ctx context.Context, gameID string) ([]port.GamePlayerEntry, error) {
				return []port.GamePlayerEntry{{PlayerNum: 1, PlayerID: "player-1"}, {PlayerNum: 2, PlayerID: "player-2"}}, nil
			}}
			hub := newTestHub(t, hubDeps{})
			relay := newTestGameRelay(t, relayDeps{hub: hub, battleClient: battle, gamePlayerRepo: gamePlayers})
			factory := newTestSocketFactory(t, hub)
			client, _ := factory.connect(t, "player-1")
			relay.JoinGame("player-1", "game-1", 1)

			relay.advanceNpcIfNeeded(context.Background(), "game-1", "player-1")

			assert.Empty(t, battle.advanceNpcTurnCallsSnapshot())
			client.expectNoMessage(t)
		})

		t.Run("対戦の記録上の人間参加者が1人でも2人でもない(0人・3人以上)とき、想定しない状態としてサーバー側にエラーが記録され、NPCの自動進行は行われない(本人への通知は無い)", func(t *testing.T) {
			battle := &stubBattleClient{}
			gamePlayers := &stubGamePlayerRepo{lookupGamePlayersFunc: func(ctx context.Context, gameID string) ([]port.GamePlayerEntry, error) {
				return []port.GamePlayerEntry{}, nil
			}}
			hub := newTestHub(t, hubDeps{})
			relay := newTestGameRelay(t, relayDeps{hub: hub, battleClient: battle, gamePlayerRepo: gamePlayers})
			factory := newTestSocketFactory(t, hub)
			client, _ := factory.connect(t, "player-1")
			relay.JoinGame("player-1", "game-1", 1)

			relay.advanceNpcIfNeeded(context.Background(), "game-1", "player-1")

			assert.Empty(t, battle.advanceNpcTurnCallsSnapshot())
			client.expectNoMessage(t)
		})

		t.Run("対戦種別の判定自体ができない(参加者記録の参照先が利用できない、または参照に失敗した)とき、NPC対戦とはみなされず、NPCの自動進行は行われない", func(t *testing.T) {
			battle := &stubBattleClient{}
			hub := newTestHub(t, hubDeps{})
			relay := newTestGameRelay(t, relayDeps{hub: hub, battleClient: battle, gamePlayerRepoUnconfigured: true})
			factory := newTestSocketFactory(t, hub)
			client, _ := factory.connect(t, "player-1")
			relay.JoinGame("player-1", "game-1", 1)

			relay.advanceNpcIfNeeded(context.Background(), "game-1", "player-1")

			assert.Empty(t, battle.advanceNpcTurnCallsSnapshot())
			client.expectNoMessage(t)
		})

		t.Run("対戦の記録上の人間参加者が1人だけ(NPC戦)のとき、NPCの最初の手番が自動的に進行する", func(t *testing.T) {
			battle := &stubBattleClient{advanceNpcTurnFunc: func(ctx context.Context, gameID string) (*service.ActionResult, error) {
				return &service.ActionResult{NpcPending: false}, nil
			}}
			gamePlayers := &stubGamePlayerRepo{lookupGamePlayersFunc: func(ctx context.Context, gameID string) ([]port.GamePlayerEntry, error) {
				return []port.GamePlayerEntry{{PlayerNum: 1, PlayerID: "player-1"}}, nil
			}}
			hub := newTestHub(t, hubDeps{})
			relay := newTestGameRelay(t, relayDeps{hub: hub, battleClient: battle, gamePlayerRepo: gamePlayers})
			factory := newTestSocketFactory(t, hub)
			client, _ := factory.connect(t, "player-1")
			relay.JoinGame("player-1", "game-1", 1)

			relay.advanceNpcIfNeeded(context.Background(), "game-1", "player-1")

			require.Equal(t, []string{"game-1"}, battle.advanceNpcTurnCallsSnapshot())
			client.expectNoMessage(t)
		})

		t.Run("NPCの手番進行の要求が、参加しようとしている本人の接続が失われたことによって中断されたとき、何も届かない", func(t *testing.T) {
			battle := &stubBattleClient{advanceNpcTurnFunc: func(ctx context.Context, gameID string) (*service.ActionResult, error) {
				return nil, context.Canceled
			}}
			gamePlayers := &stubGamePlayerRepo{lookupGamePlayersFunc: func(ctx context.Context, gameID string) ([]port.GamePlayerEntry, error) {
				return []port.GamePlayerEntry{{PlayerNum: 1, PlayerID: "player-1"}}, nil
			}}
			hub := newTestHub(t, hubDeps{})
			relay := newTestGameRelay(t, relayDeps{hub: hub, battleClient: battle, gamePlayerRepo: gamePlayers})
			factory := newTestSocketFactory(t, hub)
			client, _ := factory.connect(t, "player-1")
			relay.JoinGame("player-1", "game-1", 1)

			relay.advanceNpcIfNeeded(context.Background(), "game-1", "player-1")

			client.expectNoMessage(t)
		})

		t.Run("NPCの手番進行の要求が接続断以外の理由で失敗したとき、参加しようとしている本人にエラーが届き、NPCの自動進行はこの1手で止まる", func(t *testing.T) {
			battle := &stubBattleClient{advanceNpcTurnFunc: func(ctx context.Context, gameID string) (*service.ActionResult, error) {
				return nil, errors.New("advance npc turn failed")
			}}
			gamePlayers := &stubGamePlayerRepo{lookupGamePlayersFunc: func(ctx context.Context, gameID string) ([]port.GamePlayerEntry, error) {
				return []port.GamePlayerEntry{{PlayerNum: 1, PlayerID: "player-1"}}, nil
			}}
			hub := newTestHub(t, hubDeps{})
			relay := newTestGameRelay(t, relayDeps{hub: hub, battleClient: battle, gamePlayerRepo: gamePlayers})
			factory := newTestSocketFactory(t, hub)
			client, _ := factory.connect(t, "player-1")
			relay.JoinGame("player-1", "game-1", 1)

			relay.advanceNpcIfNeeded(context.Background(), "game-1", "player-1")

			msg := client.readMessage(t)
			assert.Equal(t, wsconst.WSServerMsgError, msg.Type)
			require.Len(t, battle.advanceNpcTurnCallsSnapshot(), 1)
		})

		t.Run("NPCの手番進行に成功したとき、そのイベントが配信され、以後NPCの手番が続く限り自動的に進行し続ける", func(t *testing.T) {
			var calls int
			battle := &stubBattleClient{
				advanceNpcTurnFunc: func(ctx context.Context, gameID string) (*service.ActionResult, error) {
					calls++
					if calls < 3 {
						return &service.ActionResult{NpcPending: true, Events: []service.ActionEvent{{Sequence: int64(calls), EventType: "turn_start", PlayerNum: nil}}}, nil
					}
					return &service.ActionResult{NpcPending: false, Events: []service.ActionEvent{{Sequence: int64(calls), EventType: "turn_start", PlayerNum: nil}}}, nil
				},
				getGameStateForPlayerFunc: func(ctx context.Context, gameID string, playerNum int) (json.RawMessage, error) {
					return stateMarkerFor(playerNum), nil
				},
			}
			gamePlayers := &stubGamePlayerRepo{lookupGamePlayersFunc: func(ctx context.Context, gameID string) ([]port.GamePlayerEntry, error) {
				return []port.GamePlayerEntry{{PlayerNum: 1, PlayerID: "player-1"}}, nil
			}}
			hub := newTestHub(t, hubDeps{})
			relay := newTestGameRelay(t, relayDeps{hub: hub, battleClient: battle, gamePlayerRepo: gamePlayers})
			factory := newTestSocketFactory(t, hub)
			client, _ := factory.connect(t, "player-1")
			relay.JoinGame("player-1", "game-1", 1)

			relay.advanceNpcIfNeeded(context.Background(), "game-1", "player-1")

			require.Len(t, battle.advanceNpcTurnCallsSnapshot(), 3)
			for i := 1; i <= 3; i++ {
				msg := client.readMessage(t)
				var payload ActionPerformedMessage
				require.NoError(t, json.Unmarshal(msg.Data, &payload))
				assert.Equal(t, int64(i), payload.Sequence)
			}
		})

		t.Run("NPCの自動進行の結果、対戦がこの時点で終了したとき、その場で対戦終了処理が行われる。このタイミングでは、それに先立つ最新の対戦状態の追加配信は行われない", func(t *testing.T) {
			battle := &stubBattleClient{advanceNpcTurnFunc: func(ctx context.Context, gameID string) (*service.ActionResult, error) {
				return &service.ActionResult{NpcPending: false, GameOver: true, WinningPlayerNum: 1, WinReason: "budget_zero"}, nil
			}}
			account := &stubAccountClient{}
			gamePlayers := &stubGamePlayerRepo{
				lookupGamePlayersFunc: func(ctx context.Context, gameID string) ([]port.GamePlayerEntry, error) {
					return []port.GamePlayerEntry{{PlayerNum: 1, PlayerID: "player-1"}}, nil
				},
				markExpAwardedFunc: func(ctx context.Context, gameID string) (bool, error) { return true, nil },
			}
			hub := newTestHub(t, hubDeps{})
			relay := newTestGameRelay(t, relayDeps{hub: hub, battleClient: battle, accountClient: account, gamePlayerRepo: gamePlayers})
			factory := newTestSocketFactory(t, hub)
			client, _ := factory.connect(t, "player-1")
			relay.JoinGame("player-1", "game-1", 1)

			relay.advanceNpcIfNeeded(context.Background(), "game-1", "player-1")

			msg := client.readMessage(t)
			assert.Equal(t, wsconst.WSServerMsgGameOver, msg.Type)
			var payload GameOverMessage
			require.NoError(t, json.Unmarshal(msg.Data, &payload))
			assert.Equal(t, "budget_zero", payload.WinReason)
			_, ok := relay.GameIDForPlayer("player-1")
			assert.False(t, ok)
		})
	})
}

func gameEnterData(t *testing.T, gameID string, deckID int64) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(GameEnterMessage{GameID: gameID, DeckID: deckID})
	require.NoError(t, err)
	return data
}

func TestHandleGameEnter(t *testing.T) {
	t.Run("ゲーム参加(game_enter)の受付", func(t *testing.T) {
		t.Run("参加要求のデータ形式が不正なとき、要求した本人にエラーが届く。それ以外の処理(対戦の有効性確認・参加登録・演出イベント配信・NPC自動進行)は行われない", func(t *testing.T) {
			invalidated := &stubInvalidatedGameRepo{}
			gamePlayers := &stubGamePlayerRepo{}
			battle := &stubBattleClient{}
			hub := newTestHub(t, hubDeps{})
			relay := newTestGameRelay(t, relayDeps{hub: hub, battleClient: battle, gamePlayerRepo: gamePlayers, invalidatedGameRepo: invalidated})
			factory := newTestSocketFactory(t, hub)
			client, conn := factory.connect(t, "player-1")

			relay.HandleGameEnter(conn, json.RawMessage(`not-json`))

			msg := client.readMessage(t)
			assert.Equal(t, wsconst.WSServerMsgError, msg.Type)
			var payload ErrorMessage
			require.NoError(t, json.Unmarshal(msg.Data, &payload))
			assert.Equal(t, "invalid_data", payload.ErrorCode)
			_, ok := relay.GameIDForPlayer("player-1")
			assert.False(t, ok)
		})

		t.Run("対象の対戦が停止によって無効と記録済みのとき、要求した本人に、対戦が無効化されたことを示すエラーが届く。参加登録は行われない", func(t *testing.T) {
			invalidated := &stubInvalidatedGameRepo{isInvalidatedFunc: func(ctx context.Context, gameID string) (bool, error) { return true, nil }}
			hub := newTestHub(t, hubDeps{})
			relay := newTestGameRelay(t, relayDeps{hub: hub, invalidatedGameRepo: invalidated})
			factory := newTestSocketFactory(t, hub)
			client, conn := factory.connect(t, "player-1")

			relay.HandleGameEnter(conn, gameEnterData(t, "game-1", 1))

			msg := client.readMessage(t)
			assert.Equal(t, wsconst.WSServerMsgError, msg.Type)
			var payload ErrorMessage
			require.NoError(t, json.Unmarshal(msg.Data, &payload))
			assert.Equal(t, "game_invalidated", payload.ErrorCode)
			_, ok := relay.GameIDForPlayer("player-1")
			assert.False(t, ok)
		})

		t.Run("対戦が無効かどうかの確認自体が、本人の接続が失われたことによって中断されたとき、何も届かない", func(t *testing.T) {
			invalidated := &stubInvalidatedGameRepo{isInvalidatedFunc: func(ctx context.Context, gameID string) (bool, error) { return false, context.Canceled }}
			hub := newTestHub(t, hubDeps{})
			relay := newTestGameRelay(t, relayDeps{hub: hub, invalidatedGameRepo: invalidated})
			factory := newTestSocketFactory(t, hub)
			client, conn := factory.connect(t, "player-1")

			relay.HandleGameEnter(conn, gameEnterData(t, "game-1", 1))

			client.expectNoMessage(t)
		})

		t.Run("対戦が無効かどうかの確認自体が接続断以外の理由で失敗したとき、要求した本人にエラーが届く", func(t *testing.T) {
			invalidated := &stubInvalidatedGameRepo{isInvalidatedFunc: func(ctx context.Context, gameID string) (bool, error) { return false, errors.New("check failed") }}
			hub := newTestHub(t, hubDeps{})
			relay := newTestGameRelay(t, relayDeps{hub: hub, invalidatedGameRepo: invalidated})
			factory := newTestSocketFactory(t, hub)
			client, conn := factory.connect(t, "player-1")

			relay.HandleGameEnter(conn, gameEnterData(t, "game-1", 1))

			msg := client.readMessage(t)
			assert.Equal(t, wsconst.WSServerMsgError, msg.Type)
			var payload ErrorMessage
			require.NoError(t, json.Unmarshal(msg.Data, &payload))
			assert.Equal(t, "game_error", payload.ErrorCode)
			assert.Equal(t, "failed to check whether the game is still valid", payload.Message)
		})

		t.Run("要求した本人がその対戦の参加登録者として記録されていないとき(古い/不正な対戦識別子を送ってきた場合を含む)、原因が何であれ(接続断による中断を除く)、本人には同一のエラー(参加登録が無いことを示すもの)が届く", func(t *testing.T) {
			cases := []struct {
				name string
				err  error
			}{
				{"参加登録の記録が無いとき", port.ErrNotFound},
				{"参加登録の記録確認自体が別の理由で失敗したとき", errors.New("lookup failed")},
			}
			var payloads []ErrorMessage
			for _, tt := range cases {
				t.Run(tt.name, func(t *testing.T) {
					invalidated := &stubInvalidatedGameRepo{isInvalidatedFunc: func(ctx context.Context, gameID string) (bool, error) { return false, nil }}
					gamePlayers := &stubGamePlayerRepo{lookupPlayerNumFunc: func(ctx context.Context, gameID, playerID string) (int, error) {
						return 0, tt.err
					}}
					hub := newTestHub(t, hubDeps{})
					relay := newTestGameRelay(t, relayDeps{hub: hub, gamePlayerRepo: gamePlayers, invalidatedGameRepo: invalidated})
					factory := newTestSocketFactory(t, hub)
					client, conn := factory.connect(t, "player-1")

					relay.HandleGameEnter(conn, gameEnterData(t, "game-1", 1))

					msg := client.readMessage(t)
					assert.Equal(t, wsconst.WSServerMsgError, msg.Type)
					var payload ErrorMessage
					require.NoError(t, json.Unmarshal(msg.Data, &payload))
					payloads = append(payloads, payload)
				})
			}
			require.Len(t, payloads, 2)
			assert.Equal(t, payloads[0], payloads[1])
		})

		t.Run("参加登録者としての記録確認自体が本人の接続断によって中断されたとき、何も届かない", func(t *testing.T) {
			invalidated := &stubInvalidatedGameRepo{isInvalidatedFunc: func(ctx context.Context, gameID string) (bool, error) { return false, nil }}
			gamePlayers := &stubGamePlayerRepo{lookupPlayerNumFunc: func(ctx context.Context, gameID, playerID string) (int, error) {
				return 0, context.Canceled
			}}
			hub := newTestHub(t, hubDeps{})
			relay := newTestGameRelay(t, relayDeps{hub: hub, gamePlayerRepo: gamePlayers, invalidatedGameRepo: invalidated})
			factory := newTestSocketFactory(t, hub)
			client, conn := factory.connect(t, "player-1")

			relay.HandleGameEnter(conn, gameEnterData(t, "game-1", 1))

			client.expectNoMessage(t)
		})

		t.Run("参加要求のデータ形式が正しく、対象の対戦が無効化されておらず、要求した本人がその対戦の参加登録者として記録されているとき、要求した本人はその対戦の参加登録者として記録され、参加登録が完了したことを示す応答(対戦識別子を含む)が本人に届く", func(t *testing.T) {
			state := pvpBattleState(t, 1, int64Ptr(3), int64Ptr(4), 1, true)
			invalidated := &stubInvalidatedGameRepo{isInvalidatedFunc: func(ctx context.Context, gameID string) (bool, error) { return false, nil }}
			gamePlayers := &stubGamePlayerRepo{
				lookupPlayerNumFunc: func(ctx context.Context, gameID, playerID string) (int, error) { return 1, nil },
				lookupGamePlayersFunc: func(ctx context.Context, gameID string) ([]port.GamePlayerEntry, error) {
					return []port.GamePlayerEntry{{PlayerNum: 1, PlayerID: "player-1"}, {PlayerNum: 2, PlayerID: "player-2"}}, nil
				},
			}
			battle := &stubBattleClient{
				getGameStateForPlayerFunc: func(ctx context.Context, gameID string, playerNum int) (json.RawMessage, error) {
					return state, nil
				},
				getTurnControlsForPlayerFunc: func(ctx context.Context, gameID string, playerNum int) (json.RawMessage, error) {
					return newRawTurnControls(t, apibattle.TurnControlsMessage{CanEndPhase: true}), nil
				},
			}
			hub := newTestHub(t, hubDeps{})
			relay := newTestGameRelay(t, relayDeps{hub: hub, battleClient: battle, gamePlayerRepo: gamePlayers, invalidatedGameRepo: invalidated})
			factory := newTestSocketFactory(t, hub)
			client, conn := factory.connect(t, "player-1")

			relay.HandleGameEnter(conn, gameEnterData(t, "game-1", 1))

			msg := client.readMessage(t)
			assert.Equal(t, wsconst.WSServerMsgGameEntered, msg.Type)
			var raw map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(msg.Data, &raw))
			assert.JSONEq(t, `"game-1"`, string(raw["game_id"]))
			gameID, ok := relay.GameIDForPlayer("player-1")
			assert.True(t, ok)
			assert.Equal(t, "game-1", gameID)
		})

		t.Run("続けて本人に、対戦開始を表す演出イベントが届き、続くターン情報が読み取れればターン開始を表す演出イベントも届く", func(t *testing.T) {
			state := pvpBattleState(t, 1, int64Ptr(3), int64Ptr(4), 1, true)
			invalidated := &stubInvalidatedGameRepo{isInvalidatedFunc: func(ctx context.Context, gameID string) (bool, error) { return false, nil }}
			gamePlayers := &stubGamePlayerRepo{
				lookupPlayerNumFunc: func(ctx context.Context, gameID, playerID string) (int, error) { return 1, nil },
				lookupGamePlayersFunc: func(ctx context.Context, gameID string) ([]port.GamePlayerEntry, error) {
					return []port.GamePlayerEntry{{PlayerNum: 1, PlayerID: "player-1"}, {PlayerNum: 2, PlayerID: "player-2"}}, nil
				},
			}
			battle := &stubBattleClient{
				getGameStateForPlayerFunc: func(ctx context.Context, gameID string, playerNum int) (json.RawMessage, error) {
					return state, nil
				},
				getTurnControlsForPlayerFunc: func(ctx context.Context, gameID string, playerNum int) (json.RawMessage, error) {
					return newRawTurnControls(t, apibattle.TurnControlsMessage{CanEndPhase: true}), nil
				},
			}
			hub := newTestHub(t, hubDeps{})
			relay := newTestGameRelay(t, relayDeps{hub: hub, battleClient: battle, gamePlayerRepo: gamePlayers, invalidatedGameRepo: invalidated})
			factory := newTestSocketFactory(t, hub)
			client, conn := factory.connect(t, "player-1")

			relay.HandleGameEnter(conn, gameEnterData(t, "game-1", 1))

			client.readMessagesOfType(t, wsconst.WSServerMsgGameEntered)
			battleStartMsg := client.readMessage(t)
			assert.Equal(t, wsconst.WSServerMsgActionPerformed, battleStartMsg.Type)
			var battleStartPayload ActionPerformedMessage
			require.NoError(t, json.Unmarshal(battleStartMsg.Data, &battleStartPayload))
			var battleStartData map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(battleStartPayload.ActionData, &battleStartData))
			assert.JSONEq(t, `"pvp"`, string(battleStartData["match_type"]))
			turnStartMsg := client.readMessage(t)
			assert.Equal(t, wsconst.WSServerMsgActionPerformed, turnStartMsg.Type)
			var turnStartPayload ActionPerformedMessage
			require.NoError(t, json.Unmarshal(turnStartMsg.Data, &turnStartPayload))
			var turnStartData map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(turnStartPayload.ActionData, &turnStartData))
			assert.JSONEq(t, `1`, string(turnStartData["turn"]))
		})

		t.Run("続けて、対戦の記録上の人間参加者が本人1人だけ(NPC戦)であれば、NPCの最初の手番が自動的に進行しその結果のイベントが配信される", func(t *testing.T) {
			state := pvpBattleState(t, 1, int64Ptr(3), nil, 1, true)
			invalidated := &stubInvalidatedGameRepo{isInvalidatedFunc: func(ctx context.Context, gameID string) (bool, error) { return false, nil }}
			gamePlayers := &stubGamePlayerRepo{
				lookupPlayerNumFunc: func(ctx context.Context, gameID, playerID string) (int, error) { return 1, nil },
				lookupGamePlayersFunc: func(ctx context.Context, gameID string) ([]port.GamePlayerEntry, error) {
					return []port.GamePlayerEntry{{PlayerNum: 1, PlayerID: "player-1"}}, nil
				},
			}
			battle := &stubBattleClient{
				getGameStateForPlayerFunc: func(ctx context.Context, gameID string, playerNum int) (json.RawMessage, error) {
					return state, nil
				},
				getTurnControlsForPlayerFunc: func(ctx context.Context, gameID string, playerNum int) (json.RawMessage, error) {
					return newRawTurnControls(t, apibattle.TurnControlsMessage{CanEndPhase: true}), nil
				},
				advanceNpcTurnFunc: func(ctx context.Context, gameID string) (*service.ActionResult, error) {
					npcState := map[string]interface{}{"marker": "npc-snapshot"}
					return &service.ActionResult{NpcPending: false, Events: []service.ActionEvent{{Sequence: 9, EventType: "play_card", PlayerNum: int64Ptr(2), State: npcState}}}, nil
				},
			}
			hub := newTestHub(t, hubDeps{})
			relay := newTestGameRelay(t, relayDeps{hub: hub, battleClient: battle, gamePlayerRepo: gamePlayers, invalidatedGameRepo: invalidated})
			factory := newTestSocketFactory(t, hub)
			client, conn := factory.connect(t, "player-1")

			relay.HandleGameEnter(conn, gameEnterData(t, "game-1", 1))

			client.readMessagesOfType(t, wsconst.WSServerMsgGameEntered, wsconst.WSServerMsgActionPerformed, wsconst.WSServerMsgActionPerformed)
			npcMsg := client.readMessage(t)
			assert.Equal(t, wsconst.WSServerMsgActionPerformed, npcMsg.Type)
			var npcPayload ActionPerformedMessage
			require.NoError(t, json.Unmarshal(npcMsg.Data, &npcPayload))
			assert.Equal(t, int64(9), npcPayload.Sequence)
			require.Equal(t, []string{"game-1"}, battle.advanceNpcTurnCallsSnapshot())
		})

		t.Run("NPCの自動進行の結果、対戦がこの時点で終了したとき、その場で対戦終了通知が参加登録者全員に届き、経験値が付与され、以後は参加登録者が誰もいなくなる", func(t *testing.T) {
			state := pvpBattleState(t, 1, int64Ptr(3), nil, 1, true)
			invalidated := &stubInvalidatedGameRepo{isInvalidatedFunc: func(ctx context.Context, gameID string) (bool, error) { return false, nil }}
			account := &stubAccountClient{}
			gamePlayers := &stubGamePlayerRepo{
				lookupPlayerNumFunc: func(ctx context.Context, gameID, playerID string) (int, error) { return 1, nil },
				lookupGamePlayersFunc: func(ctx context.Context, gameID string) ([]port.GamePlayerEntry, error) {
					return []port.GamePlayerEntry{{PlayerNum: 1, PlayerID: "player-1"}}, nil
				},
				markExpAwardedFunc: func(ctx context.Context, gameID string) (bool, error) { return true, nil },
			}
			battle := &stubBattleClient{
				getGameStateForPlayerFunc: func(ctx context.Context, gameID string, playerNum int) (json.RawMessage, error) {
					return state, nil
				},
				getTurnControlsForPlayerFunc: func(ctx context.Context, gameID string, playerNum int) (json.RawMessage, error) {
					return newRawTurnControls(t, apibattle.TurnControlsMessage{CanEndPhase: true}), nil
				},
				advanceNpcTurnFunc: func(ctx context.Context, gameID string) (*service.ActionResult, error) {
					return &service.ActionResult{NpcPending: false, GameOver: true, WinningPlayerNum: 2, WinReason: "budget_zero"}, nil
				},
			}
			hub := newTestHub(t, hubDeps{})
			relay := newTestGameRelay(t, relayDeps{hub: hub, battleClient: battle, accountClient: account, gamePlayerRepo: gamePlayers, invalidatedGameRepo: invalidated})
			factory := newTestSocketFactory(t, hub)
			client, conn := factory.connect(t, "player-1")

			relay.HandleGameEnter(conn, gameEnterData(t, "game-1", 1))

			client.readMessagesOfType(t, wsconst.WSServerMsgGameEntered, wsconst.WSServerMsgActionPerformed, wsconst.WSServerMsgActionPerformed)
			gameOverMsg := client.readMessage(t)
			assert.Equal(t, wsconst.WSServerMsgGameOver, gameOverMsg.Type)
			var payload GameOverMessage
			require.NoError(t, json.Unmarshal(gameOverMsg.Data, &payload))
			assert.Equal(t, "budget_zero", payload.WinReason)
			assert.Len(t, account.awardGameExpCallsSnapshot(), 1)
			_, ok := relay.GameIDForPlayer("player-1")
			assert.False(t, ok)
			client.expectNoMessage(t)
		})

		t.Run("そのあと、その対戦の現在の参加登録者全員に最新の対戦状態とターンコントロール情報の配信が試みられる", func(t *testing.T) {
			state := pvpBattleState(t, 1, int64Ptr(3), int64Ptr(4), 1, true)
			invalidated := &stubInvalidatedGameRepo{isInvalidatedFunc: func(ctx context.Context, gameID string) (bool, error) { return false, nil }}
			gamePlayers := &stubGamePlayerRepo{
				lookupPlayerNumFunc: func(ctx context.Context, gameID, playerID string) (int, error) { return 1, nil },
				lookupGamePlayersFunc: func(ctx context.Context, gameID string) ([]port.GamePlayerEntry, error) {
					return []port.GamePlayerEntry{{PlayerNum: 1, PlayerID: "player-1"}, {PlayerNum: 2, PlayerID: "player-2"}}, nil
				},
			}
			battle := &stubBattleClient{
				getGameStateForPlayerFunc: func(ctx context.Context, gameID string, playerNum int) (json.RawMessage, error) {
					return state, nil
				},
				getTurnControlsForPlayerFunc: func(ctx context.Context, gameID string, playerNum int) (json.RawMessage, error) {
					return newRawTurnControls(t, apibattle.TurnControlsMessage{CanEndPhase: true}), nil
				},
			}
			hub := newTestHub(t, hubDeps{})
			relay := newTestGameRelay(t, relayDeps{hub: hub, battleClient: battle, gamePlayerRepo: gamePlayers, invalidatedGameRepo: invalidated})
			factory := newTestSocketFactory(t, hub)
			client, conn := factory.connect(t, "player-1")

			relay.HandleGameEnter(conn, gameEnterData(t, "game-1", 1))

			client.readMessagesOfType(t, wsconst.WSServerMsgGameEntered, wsconst.WSServerMsgActionPerformed, wsconst.WSServerMsgActionPerformed)
			gameStateMsg := client.readMessage(t)
			assert.Equal(t, wsconst.WSServerMsgGameState, gameStateMsg.Type)
			turnControlsMsg := client.readMessage(t)
			assert.Equal(t, wsconst.WSServerMsgTurnControls, turnControlsMsg.Type)
		})
	})
}

func gameActionData(t *testing.T, gameID, actionType string, data json.RawMessage) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(GameActionMessage{GameID: gameID, ActionType: actionType, Data: data})
	require.NoError(t, err)
	return raw
}

func TestHandleGameAction(t *testing.T) {
	t.Run("対戦アクション(game_action)の受付", func(t *testing.T) {
		t.Run("行動要求のデータ形式が不正なとき、要求した本人にエラーが届く。対戦への行動処理は行われない(対戦状態の再配信もターンコントロール情報の再配信も発生しない)", func(t *testing.T) {
			battle := &stubBattleClient{}
			hub := newTestHub(t, hubDeps{})
			relay := newTestGameRelay(t, relayDeps{hub: hub, battleClient: battle})
			factory := newTestSocketFactory(t, hub)
			client, conn := factory.connect(t, "player-1")
			relay.JoinGame("player-1", "game-1", 1)

			relay.HandleGameAction(context.Background(), conn, json.RawMessage(`not-json`))

			msg := client.readMessage(t)
			assert.Equal(t, wsconst.WSServerMsgError, msg.Type)
			var payload ErrorMessage
			require.NoError(t, json.Unmarshal(msg.Data, &payload))
			assert.Equal(t, "invalid_data", payload.ErrorCode)
			assert.Empty(t, battle.processActionCallsSnapshot())
			client.expectNoMessage(t)
		})

		t.Run("要求した本人のその対戦でのスロット番号が特定できないとき、原因を問わず、本人に参加登録が無いことを示すエラーが届く", func(t *testing.T) {
			cases := []struct {
				name string
				err  error
			}{
				{"参加登録の記録が無いとき", errPlayerNotInGame},
				{"参加登録の記録確認自体が本人の接続断によって中断されたとき", context.Canceled},
			}
			for _, tt := range cases {
				t.Run(tt.name, func(t *testing.T) {
					battle := &stubBattleClient{}
					gamePlayers := &stubGamePlayerRepo{lookupGamePlayersFunc: func(ctx context.Context, gameID string) ([]port.GamePlayerEntry, error) {
						return nil, tt.err
					}}
					hub := newTestHub(t, hubDeps{})
					relay := newTestGameRelay(t, relayDeps{hub: hub, battleClient: battle, gamePlayerRepo: gamePlayers})
					factory := newTestSocketFactory(t, hub)
					client, conn := factory.connect(t, "player-1")

					relay.HandleGameAction(context.Background(), conn, gameActionData(t, "game-1", "play_card", nil))

					msg := client.readMessage(t)
					assert.Equal(t, wsconst.WSServerMsgError, msg.Type)
					var payload ErrorMessage
					require.NoError(t, json.Unmarshal(msg.Data, &payload))
					assert.Equal(t, "game_error", payload.ErrorCode)
					assert.Equal(t, "player not found in game", payload.Message)
					assert.Empty(t, battle.processActionCallsSnapshot())
				})
			}
		})

		t.Run("対戦への行動処理が、本人の接続断によって中断されたとき、何も届かない", func(t *testing.T) {
			battle := &stubBattleClient{processActionFunc: func(ctx context.Context, gameID string, playerNum int, actionType string, data json.RawMessage) (*service.ActionResult, error) {
				return nil, context.Canceled
			}}
			hub := newTestHub(t, hubDeps{})
			relay := newTestGameRelay(t, relayDeps{hub: hub, battleClient: battle})
			factory := newTestSocketFactory(t, hub)
			client, conn := factory.connect(t, "player-1")
			relay.JoinGame("player-1", "game-1", 1)

			relay.HandleGameAction(context.Background(), conn, gameActionData(t, "game-1", "play_card", nil))

			client.expectNoMessage(t)
		})

		t.Run("対戦への行動処理が接続断以外の理由で拒否・失敗したとき、要求した本人にのみ、対戦識別子・要求した行動種別・拒否された理由を含む「行動が拒否された」ことを示すメッセージが届く。この場合、状態の再配信やNPC進行は行われない", func(t *testing.T) {
			battle := &stubBattleClient{processActionFunc: func(ctx context.Context, gameID string, playerNum int, actionType string, data json.RawMessage) (*service.ActionResult, error) {
				return nil, errors.New("insufficient_budget")
			}}
			hub := newTestHub(t, hubDeps{})
			relay := newTestGameRelay(t, relayDeps{hub: hub, battleClient: battle})
			factory := newTestSocketFactory(t, hub)
			client, conn := factory.connect(t, "player-1")
			opponentClient, _ := factory.connect(t, "player-2")
			relay.JoinGame("player-1", "game-1", 1)
			relay.JoinGame("player-2", "game-1", 2)

			relay.HandleGameAction(context.Background(), conn, gameActionData(t, "game-1", "play_card", nil))

			msg := client.readMessage(t)
			assert.Equal(t, wsconst.WSServerMsgActionRejected, msg.Type)
			var payload ActionRejectedMessage
			require.NoError(t, json.Unmarshal(msg.Data, &payload))
			assert.Equal(t, "game-1", payload.GameID)
			assert.Equal(t, "play_card", payload.ActionType)
			assert.NotEmpty(t, payload.Reason)
			opponentClient.expectNoMessage(t)
			client.expectNoMessage(t)
			assert.Empty(t, battle.advanceNpcTurnCallsSnapshot())
		})

		t.Run("対戦への行動処理が受理されたとき、その結果のイベントが配信され、続けて必要なNPCの手番が自動的に進行する", func(t *testing.T) {
			battle := &stubBattleClient{
				processActionFunc: func(ctx context.Context, gameID string, playerNum int, actionType string, data json.RawMessage) (*service.ActionResult, error) {
					return &service.ActionResult{NpcPending: true, Events: []service.ActionEvent{{Sequence: 1, EventType: "turn_start", PlayerNum: nil}}}, nil
				},
				advanceNpcTurnFunc: func(ctx context.Context, gameID string) (*service.ActionResult, error) {
					return &service.ActionResult{NpcPending: false, Events: []service.ActionEvent{{Sequence: 2, EventType: "turn_start", PlayerNum: nil}}}, nil
				},
				getGameStateForPlayerFunc: func(ctx context.Context, gameID string, playerNum int) (json.RawMessage, error) {
					return stateMarkerFor(playerNum), nil
				},
				getTurnControlsForPlayerFunc: func(ctx context.Context, gameID string, playerNum int) (json.RawMessage, error) {
					return newRawTurnControls(t, apibattle.TurnControlsMessage{CanEndPhase: true}), nil
				},
			}
			hub := newTestHub(t, hubDeps{})
			relay := newTestGameRelay(t, relayDeps{hub: hub, battleClient: battle})
			factory := newTestSocketFactory(t, hub)
			client, conn := factory.connect(t, "player-1")
			relay.JoinGame("player-1", "game-1", 1)

			relay.HandleGameAction(context.Background(), conn, gameActionData(t, "game-1", "play_card", nil))

			firstMsg := client.readMessage(t)
			require.Equal(t, wsconst.WSServerMsgActionPerformed, firstMsg.Type)
			var firstPayload ActionPerformedMessage
			require.NoError(t, json.Unmarshal(firstMsg.Data, &firstPayload))
			assert.Equal(t, int64(1), firstPayload.Sequence)

			secondMsg := client.readMessage(t)
			require.Equal(t, wsconst.WSServerMsgActionPerformed, secondMsg.Type)
			var secondPayload ActionPerformedMessage
			require.NoError(t, json.Unmarshal(secondMsg.Data, &secondPayload))
			assert.Equal(t, int64(2), secondPayload.Sequence)
			require.Equal(t, []string{"game-1"}, battle.advanceNpcTurnCallsSnapshot())
		})

		t.Run("対戦への行動処理が受理されたあと、対戦が終了する行動であっても、終了通知の前に、その対戦の現在の参加登録者全員へ最新の対戦状態とターンコントロール情報が一度配信される", func(t *testing.T) {
			battle := &stubBattleClient{
				processActionFunc: func(ctx context.Context, gameID string, playerNum int, actionType string, data json.RawMessage) (*service.ActionResult, error) {
					return &service.ActionResult{GameOver: true, WinningPlayerNum: 1, WinReason: "budget_zero"}, nil
				},
				getGameStateForPlayerFunc: func(ctx context.Context, gameID string, playerNum int) (json.RawMessage, error) {
					return stateMarkerFor(playerNum), nil
				},
				getTurnControlsForPlayerFunc: func(ctx context.Context, gameID string, playerNum int) (json.RawMessage, error) {
					return newRawTurnControls(t, apibattle.TurnControlsMessage{CanEndPhase: true}), nil
				},
			}
			account := &stubAccountClient{}
			gamePlayers := &stubGamePlayerRepo{
				lookupGamePlayersFunc: func(ctx context.Context, gameID string) ([]port.GamePlayerEntry, error) {
					return []port.GamePlayerEntry{{PlayerNum: 1, PlayerID: "player-1"}}, nil
				},
				markExpAwardedFunc: func(ctx context.Context, gameID string) (bool, error) { return true, nil },
			}
			hub := newTestHub(t, hubDeps{})
			relay := newTestGameRelay(t, relayDeps{hub: hub, battleClient: battle, accountClient: account, gamePlayerRepo: gamePlayers})
			factory := newTestSocketFactory(t, hub)
			client, conn := factory.connect(t, "player-1")
			relay.JoinGame("player-1", "game-1", 1)

			relay.HandleGameAction(context.Background(), conn, gameActionData(t, "game-1", "play_card", nil))

			gameStateMsg := client.readMessage(t)
			assert.Equal(t, wsconst.WSServerMsgGameState, gameStateMsg.Type)
			turnControlsMsg := client.readMessage(t)
			assert.Equal(t, wsconst.WSServerMsgTurnControls, turnControlsMsg.Type)
			gameOverMsg := client.readMessage(t)
			assert.Equal(t, wsconst.WSServerMsgGameOver, gameOverMsg.Type)
		})

		t.Run("受理された行動によって対戦が終了と判定されたとき、その対戦のターン制限時間の計測は止まり、参加登録者全員に対戦終了通知が届き、参加者に経験値が付与され、以後全員がその対戦の参加登録者でなくなる。この終了理由は、対戦を管理する側が判定したものがそのまま使われる", func(t *testing.T) {
			clock := newFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
			battle := &stubBattleClient{
				processActionFunc: func(ctx context.Context, gameID string, playerNum int, actionType string, data json.RawMessage) (*service.ActionResult, error) {
					return &service.ActionResult{GameOver: true, WinningPlayerNum: 1, WinReason: "budget_zero"}, nil
				},
				getGameStateForPlayerFunc: func(ctx context.Context, gameID string, playerNum int) (json.RawMessage, error) {
					return stateMarkerFor(playerNum), nil
				},
				getTurnControlsForPlayerFunc: func(ctx context.Context, gameID string, playerNum int) (json.RawMessage, error) {
					return newRawTurnControls(t, apibattle.TurnControlsMessage{CanEndPhase: true}), nil
				},
			}
			account := &stubAccountClient{}
			gamePlayers := &stubGamePlayerRepo{
				lookupGamePlayersFunc: func(ctx context.Context, gameID string) ([]port.GamePlayerEntry, error) {
					return []port.GamePlayerEntry{{PlayerNum: 1, PlayerID: "player-1"}}, nil
				},
				markExpAwardedFunc: func(ctx context.Context, gameID string) (bool, error) { return true, nil },
			}
			hub := newTestHub(t, hubDeps{clock: clock})
			relay := newTestGameRelay(t, relayDeps{hub: hub, battleClient: battle, accountClient: account, gamePlayerRepo: gamePlayers, clock: clock})
			factory := newTestSocketFactory(t, hub)
			client, conn := factory.connect(t, "player-1")
			relay.JoinGame("player-1", "game-1", 1)
			relay.resetTurnTimer("game-1", "player-1", 1)

			relay.HandleGameAction(context.Background(), conn, gameActionData(t, "game-1", "play_card", nil))

			client.readMessagesOfType(t, wsconst.WSServerMsgGameState, wsconst.WSServerMsgTurnControls)
			gameOverMsg := client.readMessage(t)
			assert.Equal(t, wsconst.WSServerMsgGameOver, gameOverMsg.Type)
			var payload GameOverMessage
			require.NoError(t, json.Unmarshal(gameOverMsg.Data, &payload))
			assert.Equal(t, "budget_zero", payload.WinReason)
			assert.Len(t, account.awardGameExpCallsSnapshot(), 1)
			_, ok := relay.GameIDForPlayer("player-1")
			assert.False(t, ok)

			clock.Advance(10 * time.Second)
			client.expectNoMessage(t)
		})

		t.Run("受理された行動で対戦が終了と判定されなかったとき、対戦終了通知は届かず、参加登録も維持される", func(t *testing.T) {
			battle := &stubBattleClient{
				processActionFunc: func(ctx context.Context, gameID string, playerNum int, actionType string, data json.RawMessage) (*service.ActionResult, error) {
					return &service.ActionResult{GameOver: false}, nil
				},
				getGameStateForPlayerFunc: func(ctx context.Context, gameID string, playerNum int) (json.RawMessage, error) {
					return stateMarkerFor(playerNum), nil
				},
				getTurnControlsForPlayerFunc: func(ctx context.Context, gameID string, playerNum int) (json.RawMessage, error) {
					return newRawTurnControls(t, apibattle.TurnControlsMessage{CanEndPhase: true}), nil
				},
			}
			hub := newTestHub(t, hubDeps{})
			relay := newTestGameRelay(t, relayDeps{hub: hub, battleClient: battle})
			factory := newTestSocketFactory(t, hub)
			client, conn := factory.connect(t, "player-1")
			relay.JoinGame("player-1", "game-1", 1)

			relay.HandleGameAction(context.Background(), conn, gameActionData(t, "game-1", "play_card", nil))

			client.readMessagesOfType(t, wsconst.WSServerMsgGameState, wsconst.WSServerMsgTurnControls)
			client.expectNoMessage(t)
			_, ok := relay.GameIDForPlayer("player-1")
			assert.True(t, ok)
		})
	})
}
