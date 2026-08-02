package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	apibattle "github.com/kenyamaneko/overload-party-battle/packages/api-battle-rpc-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStartNPCBattle(t *testing.T) {
	t.Run("NPC ゲーム作成", func(t *testing.T) {
		t.Run("プレイヤーのデッキ施策 (ルーチン/スペシャル) をリクエストに載せる", func(t *testing.T) {
			const (
				wantRoutine = "RTN-0007"
				wantSpecial = "SPC-0042"
			)
			var got apibattle.NpcBattleRequest
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.NoError(t, json.NewDecoder(r.Body).Decode(&got))
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{}`))
			}))
			defer srv.Close()

			c := NewBattleClient(srv.URL, &http.Client{})
			initiatives := DeckInitiatives{RoutineID: wantRoutine, SpecialID: wantSpecial}
			_, err := c.StartNPCBattle(context.Background(), nil, initiatives, "npc-1", PlayerSummaryRequest{}, PlayerSummaryRequest{})
			require.NoError(t, err)
			assert.Equal(t, wantRoutine, got.RoutineId)
			assert.Equal(t, wantSpecial, got.SpecialId)
		})
	})
}

func TestCreatePvPGame(t *testing.T) {
	t.Run("PvP ゲーム作成", func(t *testing.T) {
		t.Run("両プレイヤーのデッキ施策をそれぞれのスロットに載せる", func(t *testing.T) {
			const (
				deck1Routine = "RTN-0001"
				deck1Special = "SPC-0001"
				deck2Routine = "RTN-0002"
				deck2Special = "SPC-0002"
			)
			var got apibattle.PvpBattleRequest
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.NoError(t, json.NewDecoder(r.Body).Decode(&got))
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{}`))
			}))
			defer srv.Close()

			c := NewBattleClient(srv.URL, &http.Client{})
			deck1 := DeckInitiatives{RoutineID: deck1Routine, SpecialID: deck1Special}
			deck2 := DeckInitiatives{RoutineID: deck2Routine, SpecialID: deck2Special}
			_, err := c.CreatePvPGame(context.Background(), nil, nil, deck1, deck2, PlayerSummaryRequest{}, PlayerSummaryRequest{})
			require.NoError(t, err)
			assert.Equal(t, deck1Routine, got.Deck1RoutineId)
			assert.Equal(t, deck1Special, got.Deck1SpecialId)
			assert.Equal(t, deck2Routine, got.Deck2RoutineId)
			assert.Equal(t, deck2Special, got.Deck2SpecialId)
		})
	})
}
