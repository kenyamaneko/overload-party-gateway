package internalauth

// HeaderName は内部認証 JWT を運ぶ HTTP ヘッダ名。
const HeaderName = "X-Internal-Auth"

// PlayerIDContextKey は middleware が検証済み player_id をコンテキストに保存するキー名。
const PlayerIDContextKey = "player_id"

// ExpectedIssuer は受理する iss クレーム値。
const ExpectedIssuer = "overload-party-gateway"

// KeyID は HMAC 鍵を識別する文字列。
type KeyID string

// DefaultKeyID は HMAC 鍵を識別する初期 key ID。
const DefaultKeyID KeyID = "v1"
