package rest

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"
	apiaccount "github.com/kenyamaneko/overload-party-account/packages/api-account"
)

func init() {
	gin.SetMode(gin.TestMode)
}

type stubAccountClient struct {
	registerFunc func(ctx context.Context, firebaseUID string) (*apiaccount.PlayerResponse, error)
	loginFunc    func(ctx context.Context, firebaseUID string) (*apiaccount.PlayerResponse, error)
}

func (s *stubAccountClient) Register(ctx context.Context, firebaseUID string) (*apiaccount.PlayerResponse, error) {
	return s.registerFunc(ctx, firebaseUID)
}

func (s *stubAccountClient) Login(ctx context.Context, firebaseUID string) (*apiaccount.PlayerResponse, error) {
	return s.loginFunc(ctx, firebaseUID)
}

func (s *stubAccountClient) FindByFirebaseUID(ctx context.Context, firebaseUID string) (*apiaccount.PlayerResponse, error) {
	panic("stubAccountClient: FindByFirebaseUID is not used by this test")
}

func (s *stubAccountClient) GetMe(ctx context.Context) (*apiaccount.PlayerResponse, error) {
	panic("stubAccountClient: GetMe is not used by this test")
}

func (s *stubAccountClient) GetBattleLimit(ctx context.Context) (*apiaccount.BattleLimitResponse, error) {
	panic("stubAccountClient: GetBattleLimit is not used by this test")
}

func (s *stubAccountClient) IncrementBattleCount(ctx context.Context) error {
	panic("stubAccountClient: IncrementBattleCount is not used by this test")
}

func (s *stubAccountClient) AwardGameExp(ctx context.Context, p1ID, p2ID string, winnerNum int64, reason, matchType string) error {
	panic("stubAccountClient: AwardGameExp is not used by this test")
}

func (s *stubAccountClient) RevertBattleCount(ctx context.Context, gameID, p1ID, p2ID string, consumedAt time.Time) error {
	panic("stubAccountClient: RevertBattleCount is not used by this test")
}

type stubPushMessageProcessor struct {
	err     error
	calls   int
	gotData []byte
}

func (s *stubPushMessageProcessor) ProcessMessage(ctx context.Context, data []byte) error {
	s.calls++
	s.gotData = data
	return s.err
}
