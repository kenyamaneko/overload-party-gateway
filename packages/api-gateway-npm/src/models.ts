// Overload Party gateway API types — the npm package for the client.
//
// Contains REST request/response types for gateway-owned endpoints
// (auth / version). Service-specific contracts
// (account / card / shop / scenario / news / support / battle) are
// published by each service in `@kenyamaneko/overload-party-api-<service>`
// (battle 公開型は `@kenyamaneko/overload-party-game-state` を直接消費)
// and consumed by the client directly.

/** HealthResponse is the API response for GET /api/v1/health. */
export interface HealthResponse {
  status: string;
}

/** VersionResponse is the API response for GET /api/v1/version. */
export interface VersionResponse {
  minimumVersion: string;
  latestVersion: string;
  forceUpdate: boolean;
  storeUrl: string;
}

/** PlayerResponse is the API response for POST /api/v1/auth/register and /api/v1/auth/login. */
export interface PlayerResponse {
  player_id: string;
  firebase_uid: string;
  name?: string;
  level: number;
  exp: number;
  is_premium: boolean;
  equipped_icon_no?: number;
  selected_faction?: string;
  premium_expires_at?: string;
  created_at: string;
  updated_at: string;
  level_exp_current: number;
  level_exp_required: number;
}

/** ErrorResponse is the standard error envelope returned by gateway REST endpoints. */
export interface ErrorResponse {
  error_code: string;
  message: string;
}
