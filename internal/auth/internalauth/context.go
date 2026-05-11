package internalauth

import (
	"context"
	"net/http"
)

type contextKey int

const tokenKey contextKey = iota

// WithToken は ctx に内部認証 token を付加した派生 context を返す。
func WithToken(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, tokenKey, token)
}

// TokenFrom は ctx から内部認証 token を取り出す。
func TokenFrom(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(tokenKey).(string)
	if !ok || v == "" {
		return "", false
	}
	return v, true
}

// InjectHeader は ctx に内部認証 token があれば h に X-Internal-Auth を設定する。
func InjectHeader(ctx context.Context, h http.Header) {
	if token, ok := TokenFrom(ctx); ok {
		h.Set(HeaderName, token)
	}
}
