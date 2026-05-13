// Package accountprofile は accountclient.Client を port.PlayerProfileGetter として
// 公開する adapter。consumer は port にのみ依存し、accountclient の concrete 型と
// API 契約型 (apiaccount.PlayerResponse) を直 import しない。
package accountprofile

import (
	"context"
	"errors"

	"github.com/kenyamaneko/overload-party-gateway/internal/client/accountclient"
	"github.com/kenyamaneko/overload-party-gateway/internal/port"
)

// Getter は accountclient.Client から PlayerProfile を返す adapter。
type Getter struct {
	client *accountclient.Client
}

var _ port.PlayerProfileGetter = (*Getter)(nil)

// New は accountclient.Client から Getter を生成する。
func New(client *accountclient.Client) *Getter {
	return &Getter{client: client}
}

// GetPlayerProfile は account から player profile を取得し port.PlayerProfile に変換する。
// 未登録 player は port.ErrNotFound を返す。name 未確定 (onboarding 前) は Name="" で返す。
func (g *Getter) GetPlayerProfile(ctx context.Context, playerID string) (port.PlayerProfile, error) {
	p, err := g.client.GetPlayer(ctx, playerID)
	if err != nil {
		if errors.Is(err, accountclient.ErrNotFound) {
			return port.PlayerProfile{}, port.ErrNotFound
		}
		return port.PlayerProfile{}, err
	}
	if p == nil {
		return port.PlayerProfile{}, port.ErrNotFound
	}
	var name string
	if p.Name != nil {
		name = *p.Name
	}
	return port.PlayerProfile{Name: name, Level: int(p.Level)}, nil
}
