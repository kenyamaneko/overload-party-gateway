package internalauth

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWithTokenTokenFrom(t *testing.T) {
	t.Run("内部認証トークンのコンテキストへの保持・取り出し", func(t *testing.T) {
		t.Run("内部認証トークンを保持させたコンテキストから取り出すと、保持させた値がそのまま得られる", func(t *testing.T) {
			ctx := WithToken(context.Background(), "some-internal-token")

			got, ok := TokenFrom(ctx)

			assert.True(t, ok)
			assert.Equal(t, "some-internal-token", got)
		})

		t.Run("内部認証トークンを一度も保持させていないコンテキストから取り出そうとすると、値は見つからない扱いになる", func(t *testing.T) {
			got, ok := TokenFrom(context.Background())

			assert.False(t, ok)
			assert.Empty(t, got)
		})

		t.Run("空文字列の内部認証トークンを保持させたコンテキストから取り出そうとすると、値は見つからない扱いになる", func(t *testing.T) {
			ctx := WithToken(context.Background(), "")

			got, ok := TokenFrom(ctx)

			assert.False(t, ok)
			assert.Empty(t, got)
		})
	})
}

func TestInjectHeader(t *testing.T) {
	t.Run("内部認証トークンのHTTPヘッダへの反映", func(t *testing.T) {
		t.Run("内部認証トークンを保持したコンテキストを渡すと、渡したHTTPヘッダ集合にX-Internal-Authというヘッダ名でその内部認証トークンの値が設定される", func(t *testing.T) {
			ctx := WithToken(context.Background(), "some-internal-token")
			h := http.Header{}

			InjectHeader(ctx, h)

			assert.Equal(t, "some-internal-token", h.Get("X-Internal-Auth"))
		})

		t.Run("呼び出し前に同じ名前のヘッダへ既に別の値が設定されていた場合、その値は新しい内部認証トークンの値で置き換えられる", func(t *testing.T) {
			ctx := WithToken(context.Background(), "new-internal-token")
			h := http.Header{}
			h.Set("X-Internal-Auth", "old-internal-token")

			InjectHeader(ctx, h)

			assert.Equal(t, "new-internal-token", h.Get("X-Internal-Auth"))
		})

		t.Run("内部認証トークンを保持していないコンテキストを渡すと、X-Internal-Authヘッダは追加されない", func(t *testing.T) {
			h := http.Header{}

			InjectHeader(context.Background(), h)

			_, ok := h["X-Internal-Auth"]
			assert.False(t, ok)
		})

		t.Run("空文字列の内部認証トークンしか保持していないコンテキストを渡すと、内部認証トークンを保持していない場合と同じ扱いになり、ヘッダは追加されない", func(t *testing.T) {
			ctx := WithToken(context.Background(), "")
			h := http.Header{}

			InjectHeader(ctx, h)

			_, ok := h["X-Internal-Auth"]
			assert.False(t, ok)
		})
	})
}
