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
//
// token が無いときは何もしない。これは middleware を通らない経路 (Firebase 認証前の auth API
// など) からの呼び出しを許容するため。token を必須化する責務は下流サービスの検証 middleware
// に委ねる。
func InjectHeader(ctx context.Context, h http.Header) {
	if token, ok := TokenFrom(ctx); ok {
		h.Set(HeaderName, token)
	}
}
