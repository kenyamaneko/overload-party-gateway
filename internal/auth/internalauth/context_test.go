package internalauth

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestInjectHeader(t *testing.T) {
	t.Run("X-Internal-Authヘッダーへのトークン注入", func(t *testing.T) {
		cases := []struct {
			name       string
			ctx        context.Context
			wantHeader string
		}{
			{
				name:       "トークンを格納したコンテキストのとき、X-Internal-Authヘッダーに同じトークンを設定する",
				ctx:        WithToken(context.Background(), "abc.def.ghi"),
				wantHeader: "abc.def.ghi",
			},
			{
				name:       "トークンを格納していないコンテキストのとき、ヘッダーを空のままにする",
				ctx:        context.Background(),
				wantHeader: "",
			},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				h := http.Header{}
				InjectHeader(tc.ctx, h)
				assert.Equal(t, tc.wantHeader, h.Get(HeaderName))
			})
		}
	})
}
