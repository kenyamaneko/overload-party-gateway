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

クライアント → gateway 入り口で Firebase ID Token を検証する（gateway）。検証済み FirebaseUID を accountclient 経由で PlayerID に解決した後、PlayerID を載せた内部認証 JWT (RS256) を発行し、`X-Internal-Auth` ヘッダで下流サービスへ渡す。署名鍵は gateway だけが持ち、下流サービスは対応する公開鍵でこの JWT を検証して、JWT 内の PlayerID を信頼する（検証部品は `packages/internalauth-go` として配布）。下流は検証できるが偽造はできない。

内部認証 JWT が運ぶのはプレイヤーが誰であるかだけで、呼び出し元が gateway であることは示さない。共有秘密鍵は検証する側が署名もできるため、鍵を持てば任意のプレイヤーになりすませる。誰が下流に到達できるかは基盤側の到達制御で決める分担になっており、その設計と内部トークンの非対称鍵化は [ADR-057](https://github.com/kenyamaneko/overload-party-common/blob/main/docs/adr/057-cloudrun-service-auth-iam-and-rs256.md) にある。

REST では `middleware.UseFirebaseAuth` → `middleware.ResolvePlayer` → `middleware.IssueInternalAuth` のチェーンが `playerID` を context に載せ、WS では upgrade 時に同等の検証・解決を行う。ハンドラは context の `playerID` だけを信頼し、リクエストボディから PlayerID を取らない（クライアント側成りすまし防止）。

## match_made の二重ゲーム作成防止

battle の `CreatePvPGame` は matchId に対して冪等ではなく、呼び出すたびに新しいゲームを作る。match-made の push subscription は at-least-once 配信のみで exactly-once をサポートしないため、gateway 側で matchId ごとの永続的な dedup を持つ (`gateway.processed_matches`)。プロセスの再起動をまたいでも同じ matchId の再送で二重にゲームが作られないよう、dedup 状態は DB に置きインメモリには持たない。

- 受信時にまず matchId を claim する。既に claim 済みで gameID も記録済みなら、battle を呼び出さず記録済みの gameID を再利用する
- claim できたら battle にゲーム作成を依頼し、成功したら gameID を記録する
- battle 呼び出し自体が失敗した場合のみ claim を解放し、Pub/Sub のリトライで再度 claim できるようにする。gameID 記録後の失敗 (`game_players` 挿入など) では claim を解放しない。解放すると再送のたびに battle を再度呼び出し、二重にゲームを作ってしまうため
- claim 済みだが gameID がまだ記録されていない状態で再送されると (battle 呼び出しが競合中、または記録直前でプロセスが停止した場合)、二重作成を避けるためその回は処理をスキップする。この状態からの自動回復は無く、運用での検知に委ねる

## 成立通知の一度きりの送出

同じ成立イベントが何度届いても、成立通知 (`match_found`) は 1 回だけ送られる。`gateway.processed_matches.notified` を冪等キーとして使い、`UPDATE ... SET notified = true WHERE ... AND notified = false` で影響行を得た呼び出しだけが WS へ送出する。プロセスの再起動をまたいでも判定できるよう、フラグは DB に置く。

- `game_players` の挿入を終えてからフラグを立てるため、対戦の記録に失敗した回は通知を送らずエラーを返す
- 記録に失敗した回はフラグが false のまま残り、Pub/Sub の再配信で記録が成功したときに通知が送られる

## EXP 付与の冪等性設計

`gateway.game_players.exp_awarded` フラグを DB レベルの冪等キーとして使う。`UPDATE ... SET exp_awarded = true WHERE ... AND exp_awarded = false` の影響行数が 0 なら即座に return し、1 を得た呼び出しだけが accountclient に付与 RPC を投げる。ゲーム終了は game_action の応答・切断猶予切れ・ターンタイムアウトの複数経路から検知され、インスタンスの再起動をまたいで再度検知されることもあるため、冪等キーは DB に置く。

重要な設計決定 2 点:

- **RPC 失敗時もフラグは巻き戻さない**。巻き戻すと別の終了経路や再起動後の再検知が付与をやり直し、二重付与のリスクが生じる。ロスト許容を受け入れ、account 側の再集計手段（運用ツール）で回復させる前提
- **UPDATE 対象は `player_num = 1` のみ**。prize は 2 プレイヤー同時付与だが、gateway 側の冪等キーは 1 行で十分。player_num=2 の行で同じ条件を書くと、同一ゲームに対して複数の付与トリガが走りうる

## WS 接続の単一インスタンス性

gateway は Cloud Run の最大インスタンス数を 1 に固定している（[ADR-058](https://github.com/kenyamaneko/overload-party-common/blob/main/docs/adr/058-gateway-on-cloudrun-single-instance.md)）。接続マップ・ゲームセッション索引・各種タイマーはプロセス内のインメモリだけで持ち、インスタンスを跨いだ WS push の解決を持ち込まずに済んでいる。Redis / etcd のようなクロスインスタンスのセッションストアが要らないのはこの制約に由来する。

- match_made: push は接続を持つプロセスに届く。受け取ったプロセスが「自分に該当プレイヤーの接続があるか」だけ見る。なければ `match_found` は届かないまま対戦だけが成立した状態になる

同一 playerID の重複接続は単一接続契約（[FEATURE_SPEC の「単一接続契約」](FEATURE_SPEC.md#単一接続契約)）で旧接続を close する。

インスタンス数の上限を上げる変更、および単一接続契約を破る変更（マルチデバイス同時接続対応など）は、接続の所有権をプロセスの外に置く設計を伴うため、WS 経路設計そのものの再検討になる。

## `gateway.game_players` テーブルの役割

gateway がドメイン状態を持たないと言いつつ 1 つだけ DB テーブルを持っている理由:

- **EXP 付与の冪等キー** (「EXP 付与の冪等性設計」): インメモリ dedup はインスタンスの再起動で消えるため、永続化が必要
- **playerNum の索引**: WS message ごとに battle に `playerNum` を問い合わせるのはコストが大きい。match_made 時に確定する `playerID → playerNum` を gateway 側にキャッシュし、以降の game_enter / game_action で参照する

どちらも「WS session 境界の冪等性・低レイテンシ要件」に由来する。battle にこれを寄せると、battle が WS 概念を持ち込むことになり「battle の passive 設計に起因する gateway 側 orchestration」の分業が崩れる。

## 運用

### Pub/Sub push 配信

| エンドポイント | 副作用 | 冪等性の担保 |
|---|---|---|
| `POST /internal/v1/pubsub/match-made` | battle ゲーム作成 + `game_players` 挿入 + WS push | 「match_made の二重ゲーム作成防止」「成立通知の一度きりの送出」の永続 dedup |

gateway は match_made 専用の受け口として位置づけられ、他サービスが発行するイベントを複数の購読先へ配信する用途で購読しない (ADR-027)。本エンドポイントは Cloud Run が allUsers に公開されるため、[ADR-057](https://github.com/kenyamaneko/overload-party-common/blob/main/docs/adr/057-cloudrun-service-auth-iam-and-rs256.md) が定める呼び出し IAM による到達制御が効かず、代わりに push リクエストに載る Google 発行 OIDC ID トークンをアプリ層で検証する。push 配信の subscription 設定 (push endpoint の URL、dead letter policy 等) はこのリポジトリからは導けない。Terraform 側の設定と併せて変更すること。

### Graceful shutdown

SIGTERM 受信時、**HTTP / WS 新規受付停止 → 既存 WS への close 送出 → in-flight 処理完了待ち** の順にドレインする。ドレインタイムアウト超過時は強制キャンセルで、そのタイミングで in-flight だった game_action / advance-npc は battle 側で未処理として残るが、WS 再接続時にクライアントが `game_state` を再取得して収束する。

Cloud Run は処理中のインスタンスにも終了を始めることがあり、ドレインの完了は保証されない。対戦ごとの計時が終了とともに失われないよう、期限は Redis に写しを置く（[ADR-059](https://github.com/kenyamaneko/overload-party-common/blob/main/docs/adr/059-gateway-timer-state-in-memory-with-redis-backup.md)）。

### 環境変数

gateway の env は [internal/config/config.go](../internal/config/config.go) が SSoT。運用上の注意点のみ:

- **`CLOUDSQL_CONNECTION_NAME`**: Cloud SQL インスタンスの接続名。値は Terraform 側の接続名と一致させる
- **`MATCHMAKING_TIMEOUT_SEC`**: マッチ待機タイムアウト。短すぎるとキューが浅い時間帯にユーザーが離脱しやすい。matchmaking サービスのキュー長メトリクスと併せて調整する
- **`UPSTASH_REDIS_URL`**: 対戦ごとの計時（切断猶予・ターン）の写しを保持する Redis の接続先。書き込み失敗は警告ログのみで対戦を止めない。写しからプロセス再起動をまたいだ状態を復元する読み出し経路は後続 Issue で追加する
- **`PUBSUB_PUSH_SERVICE_ACCOUNT_EMAIL`** / **`PUBSUB_PUSH_AUDIENCE`**: push 受け口が受け付けるサービスアカウントと audience。gateway は外部から未認証で到達できるため、この 2 つが push リクエストを識別する唯一の手段になる。値は配信元を定義する Terraform 側と一致させる
