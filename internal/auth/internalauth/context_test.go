package internalauth

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTokenContext(t *testing.T) {
	t.Run("コンテキストへのトークン格納と取り出し", func(t *testing.T) {
		cases := []struct {
			name      string
			token     string
			wantToken string
			wantOK    bool
		}{
			{name: "空でないトークンを格納したとき、同じ値が取り出せる", token: "abc.def.ghi", wantToken: "abc.def.ghi", wantOK: true},
			{name: "空のトークンを格納したとき、欠落として報告される", token: "", wantToken: "", wantOK: false},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				ctx := WithToken(context.Background(), tc.token)
				got, ok := TokenFrom(ctx)
				assert.Equal(t, tc.wantToken, got)
				assert.Equal(t, tc.wantOK, ok)
			})
		}

		t.Run("トークンを格納していないコンテキストから取得すると、欠落として報告される", func(t *testing.T) {
			got, ok := TokenFrom(context.Background())
			assert.Empty(t, got)
			assert.False(t, ok)
		})
	})
}

func TestInjectHeader(t *testing.T) {
	t.Run("X-Internal-Auth header への token 注入", func(t *testing.T) {
		cases := []struct {
			name       string
			ctx        context.Context
			wantHeader string
		}{
			{
				name:       "token がある ctx のとき、X-Internal-Auth に token を設定する",
				ctx:        WithToken(context.Background(), "abc.def.ghi"),
				wantHeader: "abc.def.ghi",
			},
			{
				name:       "token がない ctx のとき、header を空のままにする",
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
