package presenter

import (
	apiaccount "github.com/kenyamaneko/overload-party-account/packages/api-account"
	apigateway "github.com/kenyamaneko/overload-party-gateway/packages/api-gateway"
)

// PlayerToWire は account の PlayerResponse を gateway 公開 wire 型に変換する。
// 主な差分:
//   - InitialFaction → SelectedFaction にリネーム (gateway 公開仕様の歴史的経緯)。
//   - OnboardingStatus は gateway 公開 API では露出しない (client は別 endpoint で取得する)。
func PlayerToWire(p *apiaccount.PlayerResponse) *apigateway.PlayerResponse {
	if p == nil {
		return nil
	}
	return &apigateway.PlayerResponse{
		PlayerID:         p.PlayerID,
		FirebaseUID:      p.FirebaseUID,
		Name:             p.Name,
		Level:            p.Level,
		Exp:              p.Exp,
		IsPremium:        p.IsPremium,
		EquippedIconNo:   p.EquippedIconNo,
		SelectedFaction:  p.InitialFaction,
		PremiumExpiresAt: p.PremiumExpiresAt,
		CreatedAt:        p.CreatedAt,
		UpdatedAt:        p.UpdatedAt,
		LevelExpCurrent:  p.LevelExpCurrent,
		LevelExpRequired: p.LevelExpRequired,
	}
}

// PlayerSettingsToWire は account の PlayerSettingsResponse を gateway 公開 wire 型に変換する。
// 形状は一致するため identity copy だが、境界での型固定のために presenter を経由させる。
func PlayerSettingsToWire(s *apiaccount.PlayerSettingsResponse) *apigateway.PlayerSettings {
	if s == nil {
		return nil
	}
	return &apigateway.PlayerSettings{
		PlayerID:    s.PlayerID,
		Language:    s.Language,
		BgmVolume:   s.BgmVolume,
		SeVolume:    s.SeVolume,
		PushEnabled: s.PushEnabled,
		UpdatedAt:   s.UpdatedAt,
	}
}

// BattleLimitToWire は account の BattleLimitResponse を gateway 公開 wire 型に変換する。
func BattleLimitToWire(b *apiaccount.BattleLimitResponse) *apigateway.BattleLimitResponse {
	if b == nil {
		return nil
	}
	return &apigateway.BattleLimitResponse{
		DailyBattleCount: b.DailyBattleCount,
		DailyBattleLimit: b.DailyBattleLimit,
		CanBattle:        b.CanBattle,
	}
}
