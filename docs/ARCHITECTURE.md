# Gateway サービス設計

本ドキュメントは **コードを読んでも一見しては分からない設計意図** だけを残す。実装詳細（タイマーの起動順、map のキー構造、env 一覧、各メッセージの JSON 形状）は各ファイルの実装とコメント、および [FEATURE_SPEC.md](FEATURE_SPEC.md) を一次情報とする。

サービス概要・起動手順は [../README.md](../README.md)、REST エンドポイント契約は [../data/openapi.yaml](../data/openapi.yaml)、WS プロトコルは [WS_REFERENCE.md](WS_REFERENCE.md)、DB スキーマは [DATA_DESIGN.md](DATA_DESIGN.md) を参照。

## Gateway の責務境界

gateway はクライアント ⇄ 下流サービス間のプロトコル終端とセッション整合のみを担い、ドメイン状態・ルール判定・永続化されるドメインデータを所有しない。保持するのは WS 接続 map、ゲームセッションのインメモリ索引、`gateway.game_players` テーブル（EXP 付与の冪等キー + playerNum 索引用途）、`gateway.processed_matches` テーブル（match_made の永続 dedup 用途）、各種タイマー。

下流サービスから受け取った JSON ペイロードは `json.RawMessage` で不透明なまま WS へ push する。battle の `game_state` / `action_performed` のようにカード効果や盤面構造を含むペイロードを gateway が解釈すると、battle のドメイン変更に gateway の再デプロイが必要になる。契約型の変更を battle 側だけで完結させるための不変条件。

## battle の passive 設計に起因する gateway 側 orchestration

battle は **意図的に passive な REST-only エンジン** として配置されている。Pub/Sub 非購読、ウォールクロック非保持、player disconnected 概念なし、NPC は 1 手ずつしか進めない。これは「battle は game state マシン、それ以外は呼び出し側が組み立てる」という分業を維持するため。結果、以下の orchestration は gateway 側に残る:

| gateway の役割 | なぜ gateway か |
|---|---|
| match_made の Pub/Sub subscribe | battle は Pub/Sub 購読を持たない。WS 接続を保持しているレイヤでのみ push が成立する |
| ターンタイマー（ウォールクロック）と `reason: turn_timeout` forfeit 送出 | battle は `TimeBank` の減算しか見ず実時間を知らない。WS 側で turn_start を送出した時点を基準に計時できるのは gateway のみ |
| 切断検知と 120 秒猶予タイマー、`reason: disconnect` forfeit 送出 | battle は「disconnected」概念を持たず forfeit アクションしか受け付けない。WS 断を検知できるのは gateway のみ |
| NPC ターンの連続駆動（advance-npc のポーリングループ、最大 200 回） | battle は 1 リクエスト = 1 手を契約に固定している（state 更新を 1 手ずつ WS に流せるように）。連続局面を埋める駆動は呼び出し側の責務 |
| `battle_start` / `turn_start` 合成イベントの送出 | battle は WS 送出タイミングを知らない。battle のイベントシーケンス外の開始バナー用イベントを組み立てて送れるのは WS を持つ gateway のみ |

これらを将来 battle 側に寄せる選択はあるが、その場合 battle は Pub/Sub クライアント・時計・WS/セッション概念を抱え込む。**現在の設計は「battle を肥大化させない」ほうを優先している**。変更する際はこの分業を意識すること。

## 認証信頼境界

クライアント → gateway 入り口で Firebase ID Token を検証する（gateway）。検証済み FirebaseUID を accountclient 経由で PlayerID に解決した後、PlayerID を載せた内部認証 JWT (HS256) を発行し、`X-Internal-Auth` ヘッダで下流サービス（ClusterIP のみに公開）へ渡す。下流サービスは共有秘密鍵でこの JWT を検証し、JWT 内の PlayerID を信頼する（検証部品は `packages/internalauth-go` として配布）。

この信頼チェーンは **下流を VPC 外に公開しない** ことが前提。内部認証 JWT は gateway と下流の共有秘密鍵に依存し呼び出し元の識別を持たないため、下流サービスを将来外部公開する場合は gateway と同等のトークン検証や mTLS の導入が必要になる。

REST では `middleware.UseFirebaseAuth` → `middleware.ResolvePlayer` → `middleware.IssueInternalAuth` のチェーンが `playerID` を context に載せ、WS では upgrade 時に同等の検証・解決を行う。ハンドラは context の `playerID` だけを信頼し、リクエストボディから PlayerID を取らない（クライアント側成りすまし防止）。

## match_made の二重ゲーム作成防止

battle の `CreatePvPGame` は matchId に対して冪等ではなく、呼び出すたびに新しいゲームを作る。match-made の push subscription は at-least-once 配信のみで exactly-once をサポートしないため、gateway 側で matchId ごとの永続的な dedup を持つ (`gateway.processed_matches`)。プロセスの再起動をまたいでも同じ matchId の再送で二重にゲームが作られないよう、dedup 状態は DB に置きインメモリには持たない。

- 受信時にまず matchId を claim する。既に claim 済みで gameID も記録済みなら、battle を呼び出さず記録済みの gameID を再利用する
- claim できたら battle にゲーム作成を依頼し、成功したら gameID を記録する
- battle 呼び出し自体が失敗した場合のみ claim を解放し、Pub/Sub のリトライで再度 claim できるようにする。gameID 記録後の失敗 (`game_players` 挿入など) では claim を解放しない。解放すると再送のたびに battle を再度呼び出し、二重にゲームを作ってしまうため
- claim 済みだが gameID がまだ記録されていない状態で再送されると (battle 呼び出しが競合中、または記録直前でプロセスが停止した場合)、二重作成を避けるためその回は処理をスキップする。この状態からの自動回復は無く、運用での検知に委ねる

## EXP 付与の冪等性設計

`gateway.game_players.exp_awarded` フラグを DB レベルの冪等キーとして使う。`UPDATE ... SET exp_awarded = true WHERE ... AND exp_awarded = false` の影響行数が 0 の Pod は即座に return し、1 の Pod だけが accountclient に付与 RPC を投げる。

重要な設計決定 2 点:

- **RPC 失敗時もフラグは巻き戻さない**。巻き戻すと他 Pod が再度付与を試みる race が成立し、二重付与のリスクが生じる。ロスト許容を受け入れ、account 側の再集計手段（運用ツール）で回復させる前提
- **UPDATE 対象は `player_num = 1` のみ**。prize は 2 プレイヤー同時付与だが、gateway 側の冪等キーは 1 行で十分。player_num=2 の行で同じ条件を書くと、同一ゲームに対して複数の付与トリガが走りうる

## WS 宛先 Pod の単一性前提

gateway は水平スケールするが、Pod 間で「プレイヤー X の接続を持つ Pod はどれか」を追跡しない。接続マップはすべて per-Pod インメモリ。Pod を跨いだ WS push の解決はしない:

- match_made: Pub/Sub の **competing consumer** により 1 メッセージは 1 Pod にしか届かない。メッセージを受領した Pod が「自 Pod に該当プレイヤーの接続があるか」だけ見る。なければ ack して drop（`match_found` は届かない）

**「プレイヤーの WS 接続先 Pod は一意」** の前提は単一接続契約（[FEATURE_SPEC の「単一接続契約」](FEATURE_SPEC.md#単一接続契約)）で担保される。多重デバイス等で同一 playerID が別 Pod に繋がると旧接続が close されるので、定常状態では必ず 1 Pod にしか接続がない。

この設計により Redis / etcd 等のクロス Pod セッションストアを持たずに済んでいる。水平スケールを継続する前提なら、この単一接続契約を破る変更（e.g. マルチデバイス同時接続対応）は WS 経路設計そのものの再検討を伴う。

## `gateway.game_players` テーブルの役割

gateway がドメイン状態を持たないと言いつつ 1 つだけ DB テーブルを持っている理由:

- **EXP 付与の冪等キー** (「EXP 付与の冪等性設計」): インメモリ dedup は Pod 再起動で消えるため、永続化が必要
- **playerNum の索引**: WS message ごとに battle に `playerNum` を問い合わせるのはコストが大きい。match_made 時に確定する `playerID → playerNum` を gateway 側にキャッシュし、以降の game_enter / game_action で参照する

どちらも「WS session 境界の冪等性・低レイテンシ要件」に由来する。battle にこれを寄せると、battle が WS 概念を持ち込むことになり「battle の passive 設計に起因する gateway 側 orchestration」の分業が崩れる。

## 運用

### Pub/Sub push 配信

| エンドポイント | 副作用 | 冪等性の担保 |
|---|---|---|
| `POST /internal/v1/pubsub/match-made` | battle ゲーム作成 + `game_players` 挿入 + WS push | 「match_made の二重ゲーム作成防止」の永続 dedup |

gateway は match_made 専用の受け口として位置づけられ、他サービスが発行するイベントを複数の購読先へ配信する用途で購読しない (ADR-027)。本エンドポイントは Cloud Run が allUsers に公開されるため、[ADR-057](https://github.com/kenyamaneko/overload-party-common/blob/main/docs/adr/057-cloudrun-service-auth-iam-and-rs256.md) が定める呼び出し IAM による到達制御が効かず、代わりに push リクエストに載る Google 発行 OIDC ID トークンをアプリ層で検証する。push 配信の subscription 設定 (push endpoint の URL、dead letter policy 等) はこのリポジトリからは導けない。Terraform 側の設定と併せて変更すること。

### Graceful shutdown

SIGTERM 受信時、**HTTP / WS 新規受付停止 → 既存 WS への close 送出 → in-flight 処理完了待ち** の順にドレインする。preStop hook で k8s Service の endpoint から外れるまでの猶予を稼ぎ、in-flight ゲームセッションの forfeit を最小化する前提。ドレインタイムアウト超過時は強制キャンセルで、そのタイミングで in-flight だった game_action / advance-npc は battle 側で未処理として残るが、WS 再接続時にクライアントが `game_state` を再取得して収束する。

### 環境変数

gateway の env は [internal/config/config.go](../internal/config/config.go) が SSoT。運用上の注意点のみ:

- **`MATCHMAKING_TIMEOUT_SEC`**: マッチ待機タイムアウト。短すぎるとキューが浅い時間帯にユーザーが離脱しやすい。matchmaking サービスのキュー長メトリクスと併せて調整する
- **`UPSTASH_REDIS_URL`**: 対戦ごとの計時（切断猶予・ターン）の写しを保持する Redis の接続先。書き込み失敗は警告ログのみで対戦を止めない。写しからプロセス再起動をまたいだ状態を復元する読み出し経路は後続 Issue で追加する
- **`PUBSUB_PUSH_SERVICE_ACCOUNT_EMAIL`** / **`PUBSUB_PUSH_AUDIENCE`**: push 受け口が受け付けるサービスアカウントと audience。gateway は外部から未認証で到達できるため、この 2 つが push リクエストを識別する唯一の手段になる。値は配信元を定義する Terraform 側と一致させる
