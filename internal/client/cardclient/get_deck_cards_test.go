package cardclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	apicard "github.com/kenyamaneko/overload-party-card/packages/api-card"

	"github.com/kenyamaneko/overload-party-gateway/internal/auth/internalauth"
)

// GetDeckCards はデッキ詳細からルーチン/スペシャル施策の ID を取り出して battle 転送用に返す。
func TestClient_GetDeckCards_SurfacesDeckInitiatives(t *testing.T) {
	const (
		wantRoutine = "RTN-0007"
		wantSpecial = "SPC-0042"
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := apicard.DeckDetailResponse{
			Deck: apicard.Deck{
				RoutineID: wantRoutine,
				SpecialID: wantSpecial,
			},
			Cards: []apicard.DeckCard{{CardID: "TST-0001", ArtNo: 1, Count: 2}},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := New(srv.URL)
	ctx := internalauth.WithToken(context.Background(), "test.jwt.token")
	cards, initiatives, err := c.GetDeckCards(ctx, 1)
	if err != nil {
		t.Fatalf("GetDeckCards: %v", err)
	}
	if len(cards) != 1 {
		t.Fatalf("cards length = %d, want 1", len(cards))
	}
	if initiatives.RoutineID != wantRoutine {
		t.Errorf("RoutineID = %q, want %q", initiatives.RoutineID, wantRoutine)
	}
	if initiatives.SpecialID != wantSpecial {
		t.Errorf("SpecialID = %q, want %q", initiatives.SpecialID, wantSpecial)
	}
}
