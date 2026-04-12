import type { CardStats } from '@kenyamaneko/overload-party-card-types';
import type { CardType, FactionId, Restriction } from '@kenyamaneko/overload-party-game-design-constants';
import type { CloudNewsSource } from '@kenyamaneko/overload-party-newsfeed-constants';
import type { ProductType } from '@kenyamaneko/overload-party-shop-constants';
/** PlayerCardWithDef is a PlayerCard enriched with card definition fields for API responses. */
export interface PlayerCardWithDef {
    card_id: string;
    art_no: number;
    count: number;
    card_name: string;
    resource_label: string;
    faction: FactionId | 'Neutral';
    card_type: CardType;
    deploy_turns: number;
    resizable: boolean;
    elastic: boolean;
    stats: CardStats;
    effect_text?: string;
    restriction: Restriction;
}
/** Deck is the API response for a player's deck with optional card list. */
export interface Deck {
    player_id: string;
    deck_id: number;
    deck_name: string;
    is_valid: boolean;
    playmat_no?: number;
    sleeve_no?: number;
    created_at: string;
    updated_at: string;
    deck_cards?: DeckCard[];
}
/** DeckCard is a single card entry within a deck. */
export interface DeckCard {
    player_id: string;
    deck_id: number;
    card_id: string;
    art_no: number;
    count: number;
}
/** VersionResponse is the API response for GET /version. */
export interface VersionResponse {
    minimumVersion: string;
    latestVersion: string;
    forceUpdate: boolean;
    storeUrl: string;
}
/** PlayerResponse is the API response for GET /player. */
export interface PlayerResponse {
    player_id: string;
    firebase_uid: string;
    username: string;
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
/** BattleLimitResponse is the API response for GET /player/battle-limit. */
export interface BattleLimitResponse {
    daily_battle_count: number;
    daily_battle_limit: number;
    can_battle: boolean;
}
/** UserSettings holds per-player application settings. */
export interface UserSettings {
    player_id: string;
    language: string;
    bgm_volume: number;
    se_volume: number;
    push_enabled: boolean;
    updated_at: string;
}
/** ProductResponse is the API response for a purchasable item in the shop. */
export interface ProductResponse {
    product_id: string;
    name: string;
    type: ProductType;
    price: number;
    content: Record<string, unknown>;
    description?: string;
    image_url?: string;
    is_active: boolean;
    is_owned: boolean;
}
/** EpisodeWithStatus is the API response for a single episode with unlock status. */
export interface EpisodeWithStatus {
    episode_id: string;
    faction: string | null;
    episode_number: number;
    title: string;
    thumbnail_url: string | null;
    is_unlocked: boolean;
    is_completed: boolean;
    lock_reasons: LockReason[];
}
/** LockReason describes a single unmet unlock condition for an episode. */
export interface LockReason {
    type: 'level' | 'faction' | 'episode';
    required: unknown;
    current?: unknown;
}
/** NewsArticle is the API response for a news article. */
export interface NewsArticle {
    article_id: string;
    source: CloudNewsSource;
    title: string;
    summary?: string;
    tags: string[];
    published_at?: string;
    fetched_at: string;
}
/** Announcement represents a single announcement entry. */
export interface Announcement {
    id: string;
    title: string;
    body: string;
    type: 'info' | 'warning' | 'maintenance';
    published_at: string;
    expires_at?: string;
}
/** DailyTip represents a single daily tip entry. */
export interface DailyTip {
    id: string;
    text: string;
}
/** SpectateGameInfo is the public view of an active game for the spectate list API. */
export interface SpectateGameInfo {
    game_id: string;
    player1_id: string;
    player2_id: string;
    started_at: string;
}
/** DeckCardEntry represents a card entry in a deck create/update request. */
export interface DeckCardEntry {
    card_id: string;
    art_no: number;
    count: number;
}
/** DeckCreateRequest is the request body for POST /decks. */
export interface DeckCreateRequest {
    deck_name: string;
    cards: DeckCardEntry[];
    playmat_no?: number;
    sleeve_no?: number;
}
/** DeckUpdateRequest is the request body for PUT /decks/:id. */
export interface DeckUpdateRequest {
    deck_name: string;
    cards: DeckCardEntry[];
    playmat_no?: number;
    sleeve_no?: number;
}
/** UpdateSettingsRequest is the request body for PUT /player/settings. */
export interface UpdateSettingsRequest {
    language: string;
    bgm_volume: number;
    se_volume: number;
    push_enabled: boolean;
}
/** PurchaseRequest is the request body for POST /shop/purchase. */
export interface PurchaseRequest {
    product_id: string;
    platform: string;
    purchase_token: string;
}
/** RegisterRequest is the request body for POST /auth/register. */
export interface RegisterRequest {
    username: string;
}
/** PlayerNameRequest is the request body for PUT /player/name. */
export interface PlayerNameRequest {
    name: string;
}
/** SelectFactionRequest is the request body for POST /player/select-faction. */
export interface SelectFactionRequest {
    faction: string;
}
/** DeckDetailResponse is the API response for GET /player/decks/{deckId}. */
export interface DeckDetailResponse {
    deck: Deck;
    cards: DeckCard[];
}
/** ScenarioScriptResponse is the API response for GET /scenarios/{episodeId}/script. */
export interface ScenarioScriptResponse {
    episode_id: string;
    script: string;
}
/** ScenarioCompleteResponse is the API response for POST /scenarios/{episodeId}/complete. */
export interface ScenarioCompleteResponse {
    message: string;
    episode_id: string;
}
/** SelectFactionResponse is the API response for POST /player/select-faction. */
export interface SelectFactionResponse {
    message: string;
    faction: string;
    cards_granted: number;
}
/** PurchaseResponse is the API response for POST /shop/purchase. */
export interface PurchaseResponse {
    message: string;
    product_id: string;
}
/** SubscribeResponse is the API response for POST /shop/subscribe. */
export interface SubscribeResponse {
    message: string;
    expires_at: string;
}
//# sourceMappingURL=models.d.ts.map