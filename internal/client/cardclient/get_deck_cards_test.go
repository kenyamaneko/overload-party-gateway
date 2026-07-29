package cardclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	apicard "github.com/kenyamaneko/overload-party-card/packages/api-card"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kenyamaneko/overload-party-gateway/internal/auth/internalauth"
)

func TestClient_GetDeckCards(t *testing.T) {
	t.Run("デッキカードの取得", func(t *testing.T) {
		t.Run("デッキ詳細からルーチン/スペシャル施策 ID を取り出して返す", func(t *testing.T) {
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
			require.NoError(t, err)
			require.Len(t, cards, 1)
			assert.Equal(t, wantRoutine, initiatives.RoutineID)
			assert.Equal(t, wantSpecial, initiatives.SpecialID)
		})
	})
}
