package port

import "context"

// TokenVerifier は Firebase ID トークンを検証し、対応する Firebase UID を返す port。
type TokenVerifier interface {
	VerifyIDToken(ctx context.Context, idToken string) (string, error)
}
