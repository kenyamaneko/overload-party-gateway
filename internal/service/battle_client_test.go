package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	apibattle "github.com/kenyamaneko/overload-party-battle/packages/api-battle-rpc-go"
)

// StartNPCBattle はプレイヤーのデッキ施策 (ルーチン/スペシャル) を NPC ゲーム作成リクエストに載せる。
func TestStartNPCBattle_ForwardsDeckInitiatives(t *testing.T) {
	const (
		wantRoutine = "RTN-0007"
		wantSpecial = "SPC-0042"
	)
	var got apibattle.NpcBattleRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := NewBattleClient(srv.URL)
	initiatives := DeckInitiatives{RoutineID: wantRoutine, SpecialID: wantSpecial}
	if _, err := c.StartNPCBattle(context.Background(), nil, initiatives, "npc-1", PlayerSummaryRequest{}, PlayerSummaryRequest{}); err != nil {
		t.Fatalf("StartNPCBattle: %v", err)
	}
	if got.RoutineId != wantRoutine {
		t.Errorf("RoutineId = %q, want %q", got.RoutineId, wantRoutine)
	}
	if got.SpecialId != wantSpecial {
		t.Errorf("SpecialId = %q, want %q", got.SpecialId, wantSpecial)
	}
}

// CreatePvPGame は両プレイヤーのデッキ施策をそれぞれのスロットに対応付けてリクエストに載せる。
func TestCreatePvPGame_ForwardsBothDeckInitiatives(t *testing.T) {
	const (
		deck1Routine = "RTN-0001"
		deck1Special = "SPC-0001"
		deck2Routine = "RTN-0002"
		deck2Special = "SPC-0002"
	)
	var got apibattle.PvpBattleRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := NewBattleClient(srv.URL)
	deck1 := DeckInitiatives{RoutineID: deck1Routine, SpecialID: deck1Special}
	deck2 := DeckInitiatives{RoutineID: deck2Routine, SpecialID: deck2Special}
	if _, err := c.CreatePvPGame(context.Background(), nil, nil, deck1, deck2, PlayerSummaryRequest{}, PlayerSummaryRequest{}); err != nil {
		t.Fatalf("CreatePvPGame: %v", err)
	}
	if got.Deck1RoutineId != deck1Routine {
		t.Errorf("Deck1RoutineId = %q, want %q", got.Deck1RoutineId, deck1Routine)
	}
	if got.Deck1SpecialId != deck1Special {
		t.Errorf("Deck1SpecialId = %q, want %q", got.Deck1SpecialId, deck1Special)
	}
	if got.Deck2RoutineId != deck2Routine {
		t.Errorf("Deck2RoutineId = %q, want %q", got.Deck2RoutineId, deck2Routine)
	}
	if got.Deck2SpecialId != deck2Special {
		t.Errorf("Deck2SpecialId = %q, want %q", got.Deck2SpecialId, deck2Special)
	}
}
