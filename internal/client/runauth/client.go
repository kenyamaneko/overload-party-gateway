// Package runauth は Cloud Run のサービス間呼び出しに使う HTTP クライアントを提供する。
package runauth

import (
	"context"
	"fmt"
	"net/http"

	"google.golang.org/api/idtoken"
)

// NewClient は audience 宛の Google 発行 ID トークンを自動で付与する HTTP クライアントを返す。
// Cloud Run の呼び出し IAM はこのトークンを見るため、下流を呼ぶ経路はこれを通す。
//
// audience には呼び出し先 Cloud Run サービスの URL を渡す。Cloud Run は受け取った
// トークンの aud が自身の URL と一致することを求めるため、呼び出し先ごとに別の
// クライアントが要る。
func NewClient(ctx context.Context, audience string) (*http.Client, error) {
	c, err := idtoken.NewClient(ctx, audience)
	if err != nil {
		return nil, fmt.Errorf("runauth: new client for %q: %w", audience, err)
	}
	return c, nil
}
