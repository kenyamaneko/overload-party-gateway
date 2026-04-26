# API リファレンス

> 自動生成 -- 直接編集しない。`data/endpoints.yaml` の `endpoint_groups` セクションから
> `python3 scripts/generate_types.py` で生成される。
>
> WebSocket プロトコル契約は [WS_REFERENCE.md](WS_REFERENCE.md) を参照。

生成日時: `2026-04-26T02:31:48Z`

## Public REST（認証不要）

スプラッシュ画面やバージョンチェック等。認証不要。

**認証**: `none`

**ベースパス**: `/api/v1`

### `GET /api/v1/health`

API ヘルスチェック

**レスポンス**: `{"status":"ok"}`

### `GET /api/v1/version`

アプリバージョン情報を取得

**レスポンス**: `VersionResponse`

| フィールド | 型 | JSON | 説明 |
|---|---|---|---|
| MinimumVersion | `string` | `minimumVersion` | 最低要求バージョン |
| LatestVersion | `string` | `latestVersion` | 最新バージョン |
| ForceUpdate | `boolean` | `forceUpdate` | `true` の場合、クライアントはストアへ誘導する |
| StoreUrl | `string` | `storeUrl` | ストア URL |

**エラー**:

| ステータス | 説明 |
|---|---|
| `500` | 設定ロード失敗 |

### `GET /api/v1/announcements`

アクティブなお知らせ一覧を取得

**レスポンス**: `Announcement[]`

| フィールド | 型 | JSON | 説明 |
|---|---|---|---|
| ID | `string` | `id` | お知らせID |
| Title | `string` | `title` | タイトル |
| Body | `string` | `body` | 本文 |
| Type | `string` | `type` | 種別（`info` / `warning` / `maintenance`） |
| PublishedAt | `string (ISO 8601)` | `published_at` | 公開日時 |
| ExpiresAt | `string (ISO 8601)?` | `expires_at` | 有効期限 |

> 有効期限内のお知らせのみ返す。

### `GET /api/v1/daily`

今日の Tips を取得

**レスポンス**: `DailyTip`

| フィールド | 型 | JSON | 説明 |
|---|---|---|---|
| ID | `string` | `id` | TipID |
| Text | `string` | `text` | Tip テキスト |

### `GET /api/v1/cloud-news`

クラウドニュース記事一覧を取得（ページネーション対応）

**レスポンス**: `NewsArticle[]`（クエリ: `?limit=20&offset=0`）

| フィールド | 型 | JSON | 説明 |
|---|---|---|---|
| ArticleID | `string` | `article_id` | 記事 ULID |
| Source | `string` | `source` | ソース（`aws` / `google-cloud` / `azure` / `oci` / `other`） |
| Title | `string` | `title` | 記事タイトル |
| Summary | `string?` | `summary` | AI 要約（未完了の場合 null） |
| Tags | `string[]` | `tags` | タグ配列 |
| PublishedAt | `string (ISO 8601)?` | `published_at` | 記事の公開日時 |
| FetchedAt | `string (ISO 8601)` | `fetched_at` | 取得日時 |

**エラー**:

| ステータス | 説明 |
|---|---|
| `500` | newsfeed テーブル読み取りエラー |

---

## Auth REST

認証エンドポイント。Firebase Token 検証済みだが PlayerResolve 未適用（未登録ユーザーがアクセスするため）。

**認証**: `firebase`

**ベースパス**: `/api/v1`

### `POST /api/v1/auth/register`

新規プレイヤー登録（name は受け取らず、オンボーディング完了時に確定）

**レスポンス**: `201 Created`

| フィールド | 型 | JSON | 説明 |
|---|---|---|---|
| PlayerID | `string` | `player_id` | プレイヤーID（UUID） |
| FirebaseUID | `string` | `firebase_uid` | Firebase UID |
| Name | `string?` | `name` | プレイヤー名（オンボーディング未完了時は null） |
| Level | `number` | `level` | プレイヤーレベル |
| Exp | `number` | `exp` | 累計経験値 |
| IsPremium | `boolean` | `is_premium` | プレミアム会員か |
| EquippedIconNo | `number?` | `equipped_icon_no` | 装備中のアイコン番号 |
| SelectedFaction | `string?` | `selected_faction` | 選択済みファクション |
| PremiumExpiresAt | `string (ISO 8601)?` | `premium_expires_at` | プレミアム有効期限 |
| CreatedAt | `string (ISO 8601)` | `created_at` | 登録日時 |
| UpdatedAt | `string (ISO 8601)` | `updated_at` | 最終更新日時 |
| LevelExpCurrent | `number` | `level_exp_current` | 現在レベル内の経験値 |
| LevelExpRequired | `number` | `level_exp_required` | 次レベルまでの必要経験値 |

**エラー**:

| ステータス | 説明 |
|---|---|
| `409` | 既に登録済み |

### `POST /api/v1/auth/login`

既存プレイヤーのログイン

**レスポンス**: `PlayerResponse`

| フィールド | 型 | JSON | 説明 |
|---|---|---|---|
| PlayerID | `string` | `player_id` | プレイヤーID（UUID） |
| FirebaseUID | `string` | `firebase_uid` | Firebase UID |
| Name | `string?` | `name` | プレイヤー名（オンボーディング未完了時は null） |
| Level | `number` | `level` | プレイヤーレベル |
| Exp | `number` | `exp` | 累計経験値 |
| IsPremium | `boolean` | `is_premium` | プレミアム会員か |
| EquippedIconNo | `number?` | `equipped_icon_no` | 装備中のアイコン番号 |
| SelectedFaction | `string?` | `selected_faction` | 選択済みファクション |
| PremiumExpiresAt | `string (ISO 8601)?` | `premium_expires_at` | プレミアム有効期限 |
| CreatedAt | `string (ISO 8601)` | `created_at` | 登録日時 |
| UpdatedAt | `string (ISO 8601)` | `updated_at` | 最終更新日時 |
| LevelExpCurrent | `number` | `level_exp_current` | 現在レベル内の経験値 |
| LevelExpRequired | `number` | `level_exp_required` | 次レベルまでの必要経験値 |

**エラー**:

| ステータス | 説明 |
|---|---|
| `404` | プレイヤー未登録 |

---

## Authenticated REST

Firebase Token 検証 + PlayerResolve ミドルウェア適用済み。
リクエストコンテキストに player_id がセットされる。

**認証**: `firebase`

**ベースパス**: `/api/v1`

### `GET /api/v1/player`

自分のプレイヤー情報を取得

**レスポンス**: `PlayerResponse`

| フィールド | 型 | JSON | 説明 |
|---|---|---|---|
| PlayerID | `string` | `player_id` | プレイヤーID（UUID） |
| FirebaseUID | `string` | `firebase_uid` | Firebase UID |
| Name | `string?` | `name` | プレイヤー名（オンボーディング未完了時は null） |
| Level | `number` | `level` | プレイヤーレベル |
| Exp | `number` | `exp` | 累計経験値 |
| IsPremium | `boolean` | `is_premium` | プレミアム会員か |
| EquippedIconNo | `number?` | `equipped_icon_no` | 装備中のアイコン番号 |
| SelectedFaction | `string?` | `selected_faction` | 選択済みファクション |
| PremiumExpiresAt | `string (ISO 8601)?` | `premium_expires_at` | プレミアム有効期限 |
| CreatedAt | `string (ISO 8601)` | `created_at` | 登録日時 |
| UpdatedAt | `string (ISO 8601)` | `updated_at` | 最終更新日時 |
| LevelExpCurrent | `number` | `level_exp_current` | 現在レベル内の経験値 |
| LevelExpRequired | `number` | `level_exp_required` | 次レベルまでの必要経験値 |

**エラー**:

| ステータス | 説明 |
|---|---|
| `404` | プレイヤー未登録 |

### `PUT /api/v1/player/name`

プレイヤー名を変更

**リクエスト**: `PlayerNameRequest`

| フィールド | 型 | JSON | 説明 |
|---|---|---|---|
| Name | `string` | `name` | 新しいプレイヤー名 |

**レスポンス**: `PlayerResponse`

| フィールド | 型 | JSON | 説明 |
|---|---|---|---|
| PlayerID | `string` | `player_id` | プレイヤーID（UUID） |
| FirebaseUID | `string` | `firebase_uid` | Firebase UID |
| Name | `string?` | `name` | プレイヤー名（オンボーディング未完了時は null） |
| Level | `number` | `level` | プレイヤーレベル |
| Exp | `number` | `exp` | 累計経験値 |
| IsPremium | `boolean` | `is_premium` | プレミアム会員か |
| EquippedIconNo | `number?` | `equipped_icon_no` | 装備中のアイコン番号 |
| SelectedFaction | `string?` | `selected_faction` | 選択済みファクション |
| PremiumExpiresAt | `string (ISO 8601)?` | `premium_expires_at` | プレミアム有効期限 |
| CreatedAt | `string (ISO 8601)` | `created_at` | 登録日時 |
| UpdatedAt | `string (ISO 8601)` | `updated_at` | 最終更新日時 |
| LevelExpCurrent | `number` | `level_exp_current` | 現在レベル内の経験値 |
| LevelExpRequired | `number` | `level_exp_required` | 次レベルまでの必要経験値 |

**エラー**:

| ステータス | 説明 |
|---|---|
| `400` | name が空 |
| `404` | プレイヤー未登録 |

### `GET /api/v1/player/battle-limit`

デイリーバトル残り回数を取得

**レスポンス**: `BattleLimitResponse`

| フィールド | 型 | JSON | 説明 |
|---|---|---|---|
| DailyBattleCount | `number` | `daily_battle_count` | 本日のバトル回数 |
| DailyBattleLimit | `number` | `daily_battle_limit` | デイリーバトル上限（`-1` で無制限 = プレミアム会員） |
| CanBattle | `boolean` | `can_battle` | バトル可能か |

### `GET /api/v1/player/cards`

所持カード一覧を取得

**レスポンス**: `PlayerCardWithDef[]`

| フィールド | 型 | JSON | 説明 |
|---|---|---|---|
| CardID | `string` | `card_id` | カードID |
| ArtNo | `number` | `art_no` | アート番号 |
| Count | `number` | `count` | 所持枚数 |
| CardName | `string` | `card_name` | カード名 |
| ResourceLabel | `string` | `resource_label` | リソースラベル（AWS/Azure/Google Cloud/Oracle のサービス名） |
| Faction | `string` | `faction` | ファクション（`SHE` / `Tenki` / `Sugar` / `Tuners` / `Neutral`） |
| CardType | `string` | `card_type` | カード種別 |
| DeployTurns | `number` | `deploy_turns` | デプロイターン数（0=即時） |
| Resizable | `boolean` | `resizable` | 手動スケール可能か |
| Elastic | `boolean` | `elastic` | 自動スケール対応か |
| Stats | `object` | `stats` | スタッツ（ComputeStats または DataStats） |
| EffectText | `string?` | `effect_text` | エフェクト説明テキスト |
| Restriction | `string` | `restriction` | 制限（`unlimited` / `semi_limited` / `limited` / `forbidden`） |

### `GET /api/v1/player/decks`

デッキ一覧を取得

**レスポンス**: `Deck[]`

| フィールド | 型 | JSON | 説明 |
|---|---|---|---|
| PlayerID | `string` | `player_id` |  |
| DeckID | `number` | `deck_id` | デッキID（自動採番） |
| DeckName | `string` | `deck_name` | デッキ名 |
| IsValid | `boolean` | `is_valid` | バトル使用可能か（都度算出: 30枚 + 全カード所持 + 制限枚数以内） |
| PlaymatNo | `number?` | `playmat_no` | プレイマット番号（null: デフォルト） |
| SleeveNo | `number?` | `sleeve_no` | スリーブ番号（null: デフォルト） |
| CreatedAt | `string (ISO 8601)` | `created_at` |  |
| UpdatedAt | `string (ISO 8601)` | `updated_at` |  |
| DeckCards | `DeckCard[]` | `deck_cards` | デッキのカード構成（`card_id`, `art_no`, `count`） |

### `GET /api/v1/player/decks/:deckId`

デッキ詳細を取得（カード構成付き）

**レスポンス**: `DeckDetailResponse`

| フィールド | 型 | JSON | 説明 |
|---|---|---|---|
| Deck | `Deck` | `deck` | デッキ本体 |
| Cards | `DeckCard[]` | `cards` | デッキ内のカード一覧 |

**エラー**:

| ステータス | 説明 |
|---|---|
| `400` | deckId が不正 |
| `404` | デッキが存在しない |

### `POST /api/v1/player/decks`

新規デッキ作成

**リクエスト**: `DeckCreateRequest`

| フィールド | 型 | JSON | 説明 |
|---|---|---|---|
| DeckName | `string` | `deck_name` | デッキ名 |
| Cards | `DeckCardEntry[]` | `cards` | デッキのカード構成 |
| PlaymatNo | `number?` | `playmat_no` | プレイマット番号 |
| SleeveNo | `number?` | `sleeve_no` | スリーブ番号 |

**レスポンス**: `201 Created`

| フィールド | 型 | JSON | 説明 |
|---|---|---|---|
| PlayerID | `string` | `player_id` |  |
| DeckID | `number` | `deck_id` | デッキID（自動採番） |
| DeckName | `string` | `deck_name` | デッキ名 |
| IsValid | `boolean` | `is_valid` | バトル使用可能か（都度算出: 30枚 + 全カード所持 + 制限枚数以内） |
| PlaymatNo | `number?` | `playmat_no` | プレイマット番号（null: デフォルト） |
| SleeveNo | `number?` | `sleeve_no` | スリーブ番号（null: デフォルト） |
| CreatedAt | `string (ISO 8601)` | `created_at` |  |
| UpdatedAt | `string (ISO 8601)` | `updated_at` |  |
| DeckCards | `DeckCard[]` | `deck_cards` | デッキのカード構成（`card_id`, `art_no`, `count`） |

**エラー**:

| ステータス | 説明 |
|---|---|
| `400` | バリデーションエラー（デッキ名・カード構成不正） |

### `PUT /api/v1/player/decks/:deckId`

デッキ更新

**リクエスト**: `DeckUpdateRequest`

| フィールド | 型 | JSON | 説明 |
|---|---|---|---|
| DeckName | `string` | `deck_name` | デッキ名 |
| Cards | `DeckCardEntry[]` | `cards` | デッキのカード構成 |
| PlaymatNo | `number?` | `playmat_no` | プレイマット番号 |
| SleeveNo | `number?` | `sleeve_no` | スリーブ番号 |

**レスポンス**: `Deck`

| フィールド | 型 | JSON | 説明 |
|---|---|---|---|
| PlayerID | `string` | `player_id` |  |
| DeckID | `number` | `deck_id` | デッキID（自動採番） |
| DeckName | `string` | `deck_name` | デッキ名 |
| IsValid | `boolean` | `is_valid` | バトル使用可能か（都度算出: 30枚 + 全カード所持 + 制限枚数以内） |
| PlaymatNo | `number?` | `playmat_no` | プレイマット番号（null: デフォルト） |
| SleeveNo | `number?` | `sleeve_no` | スリーブ番号（null: デフォルト） |
| CreatedAt | `string (ISO 8601)` | `created_at` |  |
| UpdatedAt | `string (ISO 8601)` | `updated_at` |  |
| DeckCards | `DeckCard[]` | `deck_cards` | デッキのカード構成（`card_id`, `art_no`, `count`） |

**エラー**:

| ステータス | 説明 |
|---|---|
| `400` | バリデーションエラー |
| `404` | デッキが存在しない |

### `DELETE /api/v1/player/decks/:deckId`

デッキ削除

**レスポンス**: `204 No Content`

**エラー**:

| ステータス | 説明 |
|---|---|
| `400` | deckId が不正 |
| `404` | デッキが存在しない |

### `GET /api/v1/player/settings`

プレイヤー設定を取得

**レスポンス**: `PlayerSettings`

| フィールド | 型 | JSON | 説明 |
|---|---|---|---|
| PlayerID | `string` | `player_id` | プレイヤーID |
| Language | `string` | `language` | 言語（`ja` / `en`） |
| BgmVolume | `number` | `bgm_volume` | BGM 音量（0-100） |
| SeVolume | `number` | `se_volume` | SE 音量（0-100） |
| PushEnabled | `boolean` | `push_enabled` | プッシュ通知の有効/無効 |
| UpdatedAt | `string (ISO 8601)` | `updated_at` | 最終更新日時 |

### `PUT /api/v1/player/settings`

プレイヤー設定を更新

**リクエスト**: `UpdateSettingsRequest`

| フィールド | 型 | JSON | 説明 |
|---|---|---|---|
| Language | `string?` | `language` | 言語（`ja` / `en`） |
| BgmVolume | `number?` | `bgm_volume` | BGM 音量（0-100） |
| SeVolume | `number?` | `se_volume` | SE 音量（0-100） |
| PushEnabled | `boolean?` | `push_enabled` | プッシュ通知の有効/無効 |

**レスポンス**: `PlayerSettings`

| フィールド | 型 | JSON | 説明 |
|---|---|---|---|
| PlayerID | `string` | `player_id` | プレイヤーID |
| Language | `string` | `language` | 言語（`ja` / `en`） |
| BgmVolume | `number` | `bgm_volume` | BGM 音量（0-100） |
| SeVolume | `number` | `se_volume` | SE 音量（0-100） |
| PushEnabled | `boolean` | `push_enabled` | プッシュ通知の有効/無効 |
| UpdatedAt | `string (ISO 8601)` | `updated_at` | 最終更新日時 |

**エラー**:

| ステータス | 説明 |
|---|---|
| `400` | バリデーションエラー |

### `GET /api/v1/cards`

全カード定義一覧を取得（所持情報付き）

**レスポンス**: `PlayerCardWithDef[]`

| フィールド | 型 | JSON | 説明 |
|---|---|---|---|
| CardID | `string` | `card_id` | カードID |
| ArtNo | `number` | `art_no` | アート番号 |
| Count | `number` | `count` | 所持枚数 |
| CardName | `string` | `card_name` | カード名 |
| ResourceLabel | `string` | `resource_label` | リソースラベル（AWS/Azure/Google Cloud/Oracle のサービス名） |
| Faction | `string` | `faction` | ファクション（`SHE` / `Tenki` / `Sugar` / `Tuners` / `Neutral`） |
| CardType | `string` | `card_type` | カード種別 |
| DeployTurns | `number` | `deploy_turns` | デプロイターン数（0=即時） |
| Resizable | `boolean` | `resizable` | 手動スケール可能か |
| Elastic | `boolean` | `elastic` | 自動スケール対応か |
| Stats | `object` | `stats` | スタッツ（ComputeStats または DataStats） |
| EffectText | `string?` | `effect_text` | エフェクト説明テキスト |
| Restriction | `string` | `restriction` | 制限（`unlimited` / `semi_limited` / `limited` / `forbidden`） |

### `GET /api/v1/games/:gameId/log`

ゲームログを JSON で取得（battle サーバーにプロキシ）

**レスポンス**: Battle サーバーのゲームログ JSON

**エラー**:

| ステータス | 説明 |
|---|---|
| `404` | ゲームが存在しない |
| `500` | Battle サーバー接続エラー |

### `GET /api/v1/games/:gameId/log/text`

ゲームログをテキスト形式で取得

**レスポンス**: `text/plain` 形式のゲームログ

**エラー**:

| ステータス | 説明 |
|---|---|
| `404` | ゲームが存在しない |

### `GET /api/v1/npc/models`

NPC モデル一覧を取得（battle サーバーにプロキシ）

**レスポンス**: `NpcModel[]`

| フィールド | 型 | JSON | 説明 |
|---|---|---|---|
| Model | `string` | `model` | NPC モデル ID。`npc_battle_start` で使用する |
| Faction | `string` | `faction` | ファクション名 |
| Difficulty | `string` | `difficulty` | 難易度（`easy` / `hard`） |
| DisplayName | `string` | `display_name` | NPC の表示名 |

**エラー**:

| ステータス | 説明 |
|---|---|
| `404` | NPC モデルが存在しない |

### `GET /api/v1/spectate/games`

観戦可能なアクティブゲーム一覧を取得

**レスポンス**: `SpectateGameInfo[]`

| フィールド | 型 | JSON | 説明 |
|---|---|---|---|
| GameID | `string` | `game_id` | ゲームID（ULID） |
| Player1ID | `string` | `player1_id` | プレイヤー1 ID |
| Player2ID | `string` | `player2_id` | プレイヤー2 ID |
| StartedAt | `string (ISO 8601)` | `started_at` | ゲーム開始日時 |

### `POST /api/v1/player/select-faction`

ファクション選択（初回無料、カード付与あり）

**リクエスト**: `SelectFactionRequest`

| フィールド | 型 | JSON | 説明 |
|---|---|---|---|
| Faction | `string` | `faction` | ファクション（`SHE` / `Tenki` / `Sugar` / `Tuners`） |

**レスポンス**: `SelectFactionResponse`

| フィールド | 型 | JSON | 説明 |
|---|---|---|---|
| Message | `string` | `message` | 結果メッセージ |
| Faction | `string` | `faction` | 選択されたファクション |
| CardsGranted | `number` | `cards_granted` | 付与されたカード枚数 |

**エラー**:

| ステータス | 説明 |
|---|---|
| `400` | 不正なファクション名 |
| `409` | 既に同じファクションを選択済み |

### `GET /api/v1/shop/products`

ショップ商品一覧を取得（購入済みフラグ付き）

**レスポンス**: `{products: ProductResponse[]}`

| フィールド | 型 | JSON | 説明 |
|---|---|---|---|
| ProductID | `string` | `product_id` | 商品ID |
| Name | `string` | `name` | 商品名 |
| Type | `string` | `type` | 商品種別（`faction_set` / `cosmetic` / `subscription`） |
| Price | `number` | `price` | 価格（円） |
| Content | `object` | `content` | 商品内容（種別により構造が異なる） |
| Description | `string?` | `description` | 商品説明 |
| ImageURL | `string?` | `image_url` | 商品画像 URL |
| IsActive | `boolean` | `is_active` | 販売中か |
| IsOwned | `boolean` | `is_owned` | 購入済みか |

### `POST /api/v1/shop/purchase`

商品を購入

**リクエスト**: `PurchaseRequest`

| フィールド | 型 | JSON | 説明 |
|---|---|---|---|
| ProductID | `string` | `product_id` | 商品ID |
| Platform | `string` | `platform` | プラットフォーム（`ios` / `android`） |
| PurchaseToken | `string` | `purchase_token` | 購入トークン |

**レスポンス**: `PurchaseResponse`

| フィールド | 型 | JSON | 説明 |
|---|---|---|---|
| Message | `string` | `message` | 結果メッセージ |
| ProductID | `string` | `product_id` | 購入した商品ID |

**エラー**:

| ステータス | 説明 |
|---|---|
| `400` | リクエスト不正 |
| `402` | レシート検証失敗 |
| `404` | 商品が存在しない |
| `409` | 既に購入済み |

### `POST /api/v1/shop/subscribe`

プレミアムサブスクリプション開始

**リクエスト**: `PurchaseRequest`

| フィールド | 型 | JSON | 説明 |
|---|---|---|---|
| ProductID | `string` | `product_id` | 商品ID |
| Platform | `string` | `platform` | プラットフォーム（`ios` / `android`） |
| PurchaseToken | `string` | `purchase_token` | 購入トークン |

**レスポンス**: `SubscribeResponse`

| フィールド | 型 | JSON | 説明 |
|---|---|---|---|
| Message | `string` | `message` | 結果メッセージ |
| ExpiresAt | `string (ISO 8601)` | `expires_at` | サブスクリプション有効期限 |

**エラー**:

| ステータス | 説明 |
|---|---|
| `400` | リクエスト不正 |
| `402` | レシート検証失敗 |

### `GET /api/v1/scenarios`

エピソード一覧を取得（アンロック状態付き、`?lang=ja`）

**レスポンス**: `{episodes: EpisodeWithStatus[]}`

| フィールド | 型 | JSON | 説明 |
|---|---|---|---|
| EpisodeID | `string` | `episode_id` | エピソードID |
| Faction | `string?` | `faction` | ファクション名 |
| EpisodeNumber | `number` | `episode_number` | エピソード番号 |
| Title | `string` | `title` | エピソードタイトル |
| ThumbnailURL | `string?` | `thumbnail_url` | サムネイル画像 URL |
| IsUnlocked | `boolean` | `is_unlocked` | アンロック済みか |
| IsCompleted | `boolean` | `is_completed` | クリア済みか |
| LockReasons | `LockReason[]` | `lock_reasons` | 未達のアンロック条件（アンロック済みの場合は空配列） |

### `GET /api/v1/scenarios/:episodeId/script`

エピソードスクリプトを取得（`?lang=ja`）

**レスポンス**: `ScenarioScriptResponse`

| フィールド | 型 | JSON | 説明 |
|---|---|---|---|
| EpisodeID | `string` | `episode_id` | エピソードID |
| Script | `string` | `script` | スクリプト（`.ks` 形式） |

**エラー**:

| ステータス | 説明 |
|---|---|
| `403` | エピソード未アンロック |
| `404` | エピソードが存在しない |

### `POST /api/v1/scenarios/:episodeId/complete`

エピソードをクリア済みにする

**レスポンス**: `ScenarioCompleteResponse`

| フィールド | 型 | JSON | 説明 |
|---|---|---|---|
| Message | `string` | `message` | 結果メッセージ |
| EpisodeID | `string` | `episode_id` | エピソードID |

**エラー**:

| ステータス | 説明 |
|---|---|
| `403` | エピソード未アンロック |
| `404` | エピソードが存在しない |

---
