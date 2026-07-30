// Package firebaseauth は Firebase Auth によるトークン検証の adapter を提供する。
package firebaseauth

import (
	"context"

	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/auth"

	"github.com/kenyamaneko/overload-party-gateway/internal/port"
)

// Verifier は Firebase Auth によるトークン検証を提供します。
type Verifier struct {
	client *auth.Client
}

var _ port.TokenVerifier = (*Verifier)(nil)

// NewVerifier は Firebase Auth クライアントを包む Verifier を生成します。
func NewVerifier(client *auth.Client) *Verifier {
	return &Verifier{client: client}
}

// VerifyIDToken はトークンを検証し Firebase UID を返します。
func (v *Verifier) VerifyIDToken(ctx context.Context, idToken string) (string, error) {
	token, err := v.client.VerifyIDToken(ctx, idToken)
	if err != nil {
		return "", err
	}
	return token.UID, nil
}

// NewClient は Firebase Auth クライアントを生成します。
func NewClient(ctx context.Context) (*auth.Client, error) {
	app, err := firebase.NewApp(ctx, nil)
	if err != nil {
		return nil, err
	}
	return app.Auth(ctx)
}
