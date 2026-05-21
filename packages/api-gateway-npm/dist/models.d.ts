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
    retryable?: boolean;
}
//# sourceMappingURL=models.d.ts.map