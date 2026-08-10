package internalauth

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTokenFrom(t *testing.T) {
	t.Run("内部認証トークンの取得", func(t *testing.T) {
		t.Run("内部認証トークンを保持していない状態から取得すると、無しになる", func(t *testing.T) {
			ctx := context.Background()

			_, ok := TokenFrom(ctx)

			assert.False(t, ok)
		})

		t.Run("内部認証トークンとして空文字を保持している状態から取得すると、無しになる", func(t *testing.T) {
			ctx := WithToken(context.Background(), "")

			_, ok := TokenFrom(ctx)

			assert.False(t, ok)
		})

		t.Run("内部認証トークンとして空文字でない値を保持している状態から取得すると、有りになり、保持していた値がそのまま得られる", func(t *testing.T) {
			ctx := WithToken(context.Background(), "dummy-token")

			token, ok := TokenFrom(ctx)

			assert.True(t, ok)
			assert.Equal(t, "dummy-token", token)
		})
	})
}

func TestInjectHeader(t *testing.T) {
	t.Run("内部認証ヘッダーへの反映", func(t *testing.T) {
		t.Run("内部認証トークンを保持しているとき、HTTPリクエストヘッダーに\"X-Internal-Auth\"という名前でその値が設定される", func(t *testing.T) {
			ctx := WithToken(context.Background(), "dummy-token")
			h := http.Header{}

			InjectHeader(ctx, h)

			assert.Equal(t, "dummy-token", h.Get(HeaderName))
		})

		t.Run("内部認証トークンを保持しており、リクエストヘッダーに既に同名のヘッダーが設定済みであるとき、その値は新しい値に置き換わる", func(t *testing.T) {
			ctx := WithToken(context.Background(), "new-token")
			h := http.Header{}
			h.Set(HeaderName, "old-token")

			InjectHeader(ctx, h)

			assert.Equal(t, "new-token", h.Get(HeaderName))
		})

		t.Run("内部認証トークンを保持していないとき、リクエストヘッダーは変更されない", func(t *testing.T) {
			ctx := context.Background()
			h := http.Header{"X-Existing": []string{"value"}}
			want := h.Clone()

			InjectHeader(ctx, h)

			assert.Equal(t, want, h)
		})

		t.Run("内部認証トークンとして空文字を保持しているとき、リクエストヘッダーは変更されない(トークンを保持していない場合と同じ扱いになる)", func(t *testing.T) {
			ctx := WithToken(context.Background(), "")
			h := http.Header{"X-Existing": []string{"value"}}
			want := h.Clone()

			InjectHeader(ctx, h)

			assert.Equal(t, want, h)
		})
	})
}
