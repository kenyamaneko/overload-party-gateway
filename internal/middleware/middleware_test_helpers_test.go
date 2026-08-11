package middleware

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"

	apiaccount "github.com/kenyamaneko/overload-party-account/packages/api-account"
)

func init() {
	gin.SetMode(gin.TestMode)
}

type stubTokenVerifier struct {
	verifyIDToken func(ctx context.Context, idToken string) (string, error)
}

func (s *stubTokenVerifier) VerifyIDToken(ctx context.Context, idToken string) (string, error) {
	return s.verifyIDToken(ctx, idToken)
}

type stubAccountClient struct {
	findByFirebaseUID func(ctx context.Context, firebaseUID string) (*apiaccount.PlayerResponse, error)
	register          func(ctx context.Context, firebaseUID string) (*apiaccount.PlayerResponse, error)
}

func (s *stubAccountClient) FindByFirebaseUID(ctx context.Context, firebaseUID string) (*apiaccount.PlayerResponse, error) {
	if s.findByFirebaseUID == nil {
		panic("stubAccountClient: FindByFirebaseUID was called without being stubbed")
	}
	return s.findByFirebaseUID(ctx, firebaseUID)
}

func (s *stubAccountClient) Register(ctx context.Context, firebaseUID string) (*apiaccount.PlayerResponse, error) {
	if s.register == nil {
		panic("stubAccountClient: Register was called without being stubbed")
	}
	return s.register(ctx, firebaseUID)
}

func (s *stubAccountClient) Login(ctx context.Context, firebaseUID string) (*apiaccount.PlayerResponse, error) {
	panic("stubAccountClient: Login is not used by internal/middleware")
}

func (s *stubAccountClient) GetMe(ctx context.Context) (*apiaccount.PlayerResponse, error) {
	panic("stubAccountClient: GetMe is not used by internal/middleware")
}

func (s *stubAccountClient) GetBattleLimit(ctx context.Context) (*apiaccount.BattleLimitResponse, error) {
	panic("stubAccountClient: GetBattleLimit is not used by internal/middleware")
}

func (s *stubAccountClient) IncrementBattleCount(ctx context.Context) error {
	panic("stubAccountClient: IncrementBattleCount is not used by internal/middleware")
}

func (s *stubAccountClient) AwardGameExp(ctx context.Context, p1ID, p2ID string, winnerNum int64, reason, matchType string) error {
	panic("stubAccountClient: AwardGameExp is not used by internal/middleware")
}

func (s *stubAccountClient) RevertBattleCount(ctx context.Context, gameID, p1ID, p2ID string, consumedAt time.Time) error {
	panic("stubAccountClient: RevertBattleCount is not used by internal/middleware")
}

type stubPubSubPushTokenValidator struct {
	validate func(ctx context.Context, idToken, audience string) (string, error)
}

func (s *stubPubSubPushTokenValidator) Validate(ctx context.Context, idToken, audience string) (string, error) {
	if s.validate == nil {
		panic("stubPubSubPushTokenValidator: Validate was called without being stubbed")
	}
	return s.validate(ctx, idToken, audience)
}
