package internalauth

// HeaderName は内部認証 JWT を運ぶ HTTP ヘッダ名。
const HeaderName = "X-Internal-Auth"

// PlayerIDContextKey は middleware が検証済み player_id を gin context に保存するキー名。
const PlayerIDContextKey = "player_id"

// TokenContextKey は middleware が検証済み raw token を gin context に保存するキー名。
// 受領 JWT を下流サービスへそのまま forward する用途で利用する。
const TokenContextKey = "internal_auth_token"

// ExpectedIssuer は受理する iss クレーム値。
const ExpectedIssuer = "overload-party-gateway"

// KeyID は署名鍵を識別する文字列。
type KeyID string

// DefaultKeyID は署名鍵を識別する初期 key ID。
const DefaultKeyID KeyID = "v1"
