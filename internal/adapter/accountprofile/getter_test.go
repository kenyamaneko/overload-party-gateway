package accountprofile_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	apiaccount "github.com/kenyamaneko/overload-party-account/packages/api-account"
	"github.com/kenyamaneko/overload-party-account/packages/api-account/apiaccountserverfake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kenyamaneko/overload-party-gateway/internal/adapter/accountprofile"
	"github.com/kenyamaneko/overload-party-gateway/internal/client/accountclient"
	"github.com/kenyamaneko/overload-party-gateway/internal/port"
)

// strPtr は string literal を *string にするテストヘルパ。
func strPtr(s string) *string { return &s }

// TestGetter_GetPlayerProfile_ReturnsProfileWithName は account が name 設定済みの player
// を返したとき PlayerProfile が同値を持つことを検証する (正常系の round trip)。
func TestGetter_GetPlayerProfile_ReturnsProfileWithName(t *testing.T) {
	srv := apiaccountserverfake.NewServer()
	defer srv.Close()
	srv.GetPlayerByIDFn = func(_ string) (int, any) {
		return http.StatusOK, apiaccount.PlayerResponse{PlayerID: "p-1", Name: strPtr("alice"), Level: 7}
	}
	g := accountprofile.New(accountclient.New(srv.URL()))

	got, err := g.GetPlayerProfile(context.Background(), "p-1")
	require.NoError(t, err)
	assert.Equal(t, port.PlayerProfile{Name: "alice", Level: 7}, got)
}

// TestGetter_GetPlayerProfile_ReturnsEmptyNameWhenAccountNameNil は onboarding 未完了
// (Name nil) の player を取得したとき Name="" の PlayerProfile を返すことを検証する
// (silent な空文字 fallback ではなく明示的に空である状態として consumer に渡す)。
func TestGetter_GetPlayerProfile_ReturnsEmptyNameWhenAccountNameNil(t *testing.T) {
	srv := apiaccountserverfake.NewServer()
	defer srv.Close()
	srv.GetPlayerByIDFn = func(_ string) (int, any) {
		return http.StatusOK, apiaccount.PlayerResponse{PlayerID: "p-1", Name: nil, Level: 7}
	}
	g := accountprofile.New(accountclient.New(srv.URL()))

	got, err := g.GetPlayerProfile(context.Background(), "p-1")
	require.NoError(t, err)
	assert.Equal(t, port.PlayerProfile{Name: "", Level: 7}, got)
}

// TestGetter_GetPlayerProfile_ReturnsErrNotFoundOn404 は account が 404 を返したとき
// accountclient.ErrNotFound が port.ErrNotFound に変換されることを検証する
// (consumer は port のエラー語彙だけを扱えればよい契約)。
func TestGetter_GetPlayerProfile_ReturnsErrNotFoundOn404(t *testing.T) {
	srv := apiaccountserverfake.NewServer()
	defer srv.Close()
	srv.GetPlayerByIDFn = func(_ string) (int, any) {
		return http.StatusNotFound, nil
	}
	g := accountprofile.New(accountclient.New(srv.URL()))

	_, err := g.GetPlayerProfile(context.Background(), "missing")
	require.Error(t, err)
	assert.ErrorIs(t, err, port.ErrNotFound)
}

// TestGetter_GetPlayerProfile_PropagatesOtherErrors は 404 以外のエラーが port.ErrNotFound
// に化けず原 error として伝播することを検証する (consumer の retry / 監視判断が
// transient と persistent で分けられるようにするため)。
func TestGetter_GetPlayerProfile_PropagatesOtherErrors(t *testing.T) {
	srv := apiaccountserverfake.NewServer()
	defer srv.Close()
	srv.GetPlayerByIDFn = func(_ string) (int, any) {
		return http.StatusInternalServerError, nil
	}
	g := accountprofile.New(accountclient.New(srv.URL()))

	_, err := g.GetPlayerProfile(context.Background(), "p-1")
	require.Error(t, err)
	assert.False(t, errors.Is(err, port.ErrNotFound))
}
