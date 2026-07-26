# Gateway サービス設計

本ドキュメントは **コードを読んでも一見しては分からない設計意図** だけを残す。実装詳細（タイマーの起動順、map のキー構造、env 一覧、各メッセージの JSON 形状）は各ファイルの実装とコメント、および [FEATURE_SPEC.md](FEATURE_SPEC.md) を一次情報とする。

サービス概要・起動手順は [../README.md](../README.md)、REST エンドポイント契約は [../data/openapi.yaml](../data/openapi.yaml)、WS プロトコルは [WS_REFERENCE.md](WS_REFERENCE.md)、DB スキーマは [DATA_DESIGN.md](DATA_DESIGN.md) を参照。

## Gateway の責務境界

gateway はクライアント ⇄ 下流サービス間のプロトコル終端とセッション整合のみを担い、ドメイン状態・ルール判定・永続化されるドメインデータを所有しない。保持するのは WS 接続 map、ゲームセッションのインメモリ索引、`gateway.game_players` テーブル（match_made 起点の冪等キー用途）、各種タイマー。

下流サービスから受け取った JSON ペイロードは `json.RawMessage` で不透明なまま WS へ push する。battle の `game_state` / `action_performed` のようにカード効果や盤面構造を含むペイロードを gateway が解釈すると、battle のドメイン変更に gateway の再デプロイが必要になる。契約型の変更を battle 側だけで完結させるための不変条件。

## battle の passive 設計に起因する gateway 側 orchestration

battle は **意図的に passive な REST-only エンジン** として配置されている。Pub/Sub 非購読、ウォールクロック非保持、player disconnected 概念なし、NPC は 1 手ずつしか進めない。これは「battle は game state マシン、それ以外は呼び出し側が組み立てる」という分業を維持するため。結果、以下の orchestration は gateway 側に残る:

| gateway の役割 | なぜ gateway か |
|---|---|
| match_made の Pub/Sub subscribe | battle は Pub/Sub 購読を持たない。WS 接続を保持しているレイヤでのみ push が成立する |
| ターンタイマー（ウォールクロック）と `reason: turn_timeout` forfeit 送出 | battle は `TimeBank` の減算しか見ず実時間を知らない。WS 側で turn_start を送出した時点を基準に計時できるのは gateway のみ |
| 切断検知と 60 秒猶予タイマー、`reason: disconnect` forfeit 送出 | battle は「disconnected」概念を持たず forfeit アクションしか受け付けない。WS 断を検知できるのは gateway のみ |
| NPC ターンの連続駆動（advance-npc のポーリングループ、最大 200 回） | battle は 1 リクエスト = 1 手を契約に固定している（state 更新を 1 手ずつ WS に流せるように）。連続局面を埋める駆動は呼び出し側の責務 |
| `battle_start` / `turn_start` 合成イベントの送出 | battle は WS 送出タイミングを知らない。battle のイベントシーケンス外の開始バナー用イベントを組み立てて送れるのは WS を持つ gateway のみ |

これらを将来 battle 側に寄せる選択はあるが、その場合 battle は Pub/Sub クライアント・時計・WS/セッション概念を抱え込む。**現在の設計は「battle を肥大化させない」ほうを優先している**。変更する際はこの分業を意識すること。

## 認証信頼境界

クライアント → gateway 入り口で Firebase ID Token を検証する（gateway）。検証済み FirebaseUID を accountclient 経由で PlayerID に解決した後、PlayerID を載せた内部認証 JWT (HS256) を発行し、`X-Internal-Auth` ヘッダで下流サービス（ClusterIP のみに公開）へ渡す。下流サービスは共有秘密鍵でこの JWT を検証し、JWT 内の PlayerID を信頼する（検証部品は `packages/internalauth-go` として配布）。

この信頼チェーンは **下流を VPC 外に公開しない** ことが前提。内部認証 JWT は gateway と下流の共有秘密鍵に依存し呼び出し元の識別を持たないため、下流サービスを将来外部公開する場合は gateway と同等のトークン検証や mTLS の導入が必要になる。

REST では `middleware.UseFirebaseAuth` → `middleware.ResolvePlayer` → `middleware.IssueInternalAuth` のチェーンが `playerID` を context に載せ、WS では upgrade 時に同等の検証・解決を行う。ハンドラは context の `playerID` だけを信頼し、リクエストボディから PlayerID を取らない（クライアント側成りすまし防止）。

## match_made の二重ゲーム作成防止（多層冪等性）

gateway は match_made イベントを Pub/Sub の push 配信で受け取る。push 配信は at-least-once であり、同一メッセージが複数回配信されることがある。単一インスタンス構成でも、この再配信による重複ゲーム作成を防ぐ保険を層状に張っている。

1. **プロセス内 dedup** (`matchId` キー): 実行中のプロセスが同一メッセージを再度受け取っても処理しない。最初の受領時に dedup entry を入れ、後続のハンドラ処理が失敗した場合は entry をロールバックして 500 を返し、Pub/Sub にリトライさせる
2. **battle 側 `matchId` 冪等**: battle の `CreatePvPGame` は `matchId` に対して冪等で、既存ゲームがあれば同じ game を返す。インスタンスの再起動でプロセス内 dedup の状態が失われた後に同じメッセージが再配信されても、二重にゲームが作られない
3. **`gateway.game_players` の UNIQUE 制約**: 同一 `(gameID, playerNum)` の挿入は DB 側で失敗する。インスタンス再起動をまたいだ再配信でも、`game_players` に重複行が入らない

1 つ目はプロセスが生きている間の再配信を防ぐが、インスタンス再起動でこの状態は失われる。再起動をまたいだ再配信に対しては、2 つ目が battle 側の二重ゲーム作成を、3 つ目が `game_players` の重複行を防ぐ。片方が欠けると、battle にゴミゲームが残るか `game_players` の挿入で整合が崩れるかのいずれかが起きる。

## EXP 付与の冪等性設計

`gateway.game_players.exp_awarded` フラグを DB レベルの冪等キーとして使う。`UPDATE ... SET exp_awarded = true WHERE ... AND exp_awarded = false` の影響行数が 0 の呼び出しは即座に return し、1 の呼び出しだけが accountclient に付与 RPC を投げる。ゲーム終了は通常決着・ターンタイムアウト・切断確定など複数の経路から呼ばれるほか、インスタンス再起動をまたいで再度呼ばれることもあるため、呼び出し回数によらずこのフラグだけを根拠に一度だけ付与する。

重要な設計決定 2 点:

- **RPC 失敗時もフラグは巻き戻さない**。巻き戻すと後続の呼び出しと race して二重付与のリスクが生じる。ロスト許容を受け入れ、account 側の再集計手段（運用ツール）で回復させる前提
- **UPDATE 対象は `player_num = 1` のみ**。prize は 2 プレイヤー同時付与だが、gateway 側の冪等キーは 1 行で十分。player_num=2 の行で同じ条件を書くと、同一ゲームに対して複数の付与トリガが走りうる

## WS 接続の単一インスタンス性

gateway の WS 接続マップはプロセス内のインメモリ状態であり、外部の共有ストアを介さない。Cloud Run 上のインスタンス数は最大 1 に固定されているため、WS 接続を保持するインスタンスが存在する限りそれは常に 1 つであり、match_made の push も必ずそのインスタンスに届く。プレイヤーの接続の有無は、そのインスタンス自身の接続マップを見るだけで判定できる。存在しなければ `match_found` は届かない。

単一接続契約（[FEATURE_SPEC の「単一接続契約」](FEATURE_SPEC.md#単一接続契約)）は多重ログイン・多重デバイスでのセッション競合を防ぐ業務ルールとして別に存在し、同一 playerID の新規接続が到着すると旧接続を close する。

インスタンス数が最大 1 に固定される制約と単一接続契約が組み合わさることで、Redis / etcd のような分散セッションストアを持たずに済んでいる。

## `gateway.game_players` テーブルの役割

gateway がドメイン状態を持たないと言いつつ 1 つだけ DB テーブルを持っている理由:

- **EXP 付与の冪等キー** (「EXP 付与の冪等性設計」): インメモリの状態はインスタンス再起動で消えるため、永続化が必要
- **playerNum の索引**: WS message ごとに battle に `playerNum` を問い合わせるのはコストが大きい。match_made 時に確定する `playerID → playerNum` を gateway 側にキャッシュし、以降の game_enter / game_action で参照する

どちらも「WS session 境界の冪等性・低レイテンシ要件」に由来する。battle にこれを寄せると、battle が WS 概念を持ち込むことになり「battle の passive 設計に起因する gateway 側 orchestration」の分業が崩れる。

## 運用

### Pub/Sub push 配信

| エンドポイント | 副作用 | 冪等性の担保 |
|---|---|---|
| `POST /internal/v1/pubsub/match_made` | battle ゲーム作成 + `game_players` 挿入 + WS push | 「match_made の二重ゲーム作成防止（多層冪等性）」の 3 層 (ゲーム作成・`game_players` 挿入の重複防止) |

gateway は match_made 専用の受け口として位置づけられ、他サービスが publish するイベントを fan-out 用途で subscribe しない (ADR-027)。到達制御は Cloud Run の呼び出し IAM が担うため、本エンドポイントはアプリ層の認証を行わない (ADR-057)。push 配信の subscription 設定 (push endpoint の URL、dead letter policy 等) はこのリポジトリからは導けない。Terraform 側の設定と併せて変更すること。

### Graceful shutdown

SIGTERM 受信時、新規リクエストの受付を止め、進行中の処理の完了を一定時間待ってから終了する。待機時間を超過すると強制終了し、その時点で in-flight だった game_action / advance-npc は battle 側で未処理として残るが、WS 再接続時にクライアントが `game_state` を再取得して収束する。

### 環境変数

gateway の env は [internal/config/config.go](../internal/config/config.go) が SSoT。運用上の注意点のみ:

- **`MATCHMAKING_TIMEOUT_SEC`**: マッチ待機タイムアウト。短すぎるとキューが浅い時間帯にユーザーが離脱しやすい。matchmaking サービスのキュー長メトリクスと併せて調整する
