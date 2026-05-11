package internalauth

// Verifier は内部サービス間 JWT を検証して sub クレーム (player_id) を返す。
type Verifier interface {
	Verify(token string) (string, error)
}
