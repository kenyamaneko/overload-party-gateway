# overload-party-gateway

カードゲーム Overload Party の認証・ルーティング・WebSocket リレーを担うマイクロサービス。

## 技術スタック

| レイヤー | 技術 |
|---|---|
| 言語 | Go |
| フレームワーク | Gin, gorilla/websocket |
| データベース | Cloud SQL PostgreSQL |
| NoSQL | Cloud Firestore |
| セッションストア | Upstash Redis |
| 同期通信 | REST |
| 非同期通信 | WebSocket, Cloud Pub/Sub |

## ドキュメント

| ドキュメント | 内容 |
|---|---|
| [セットアップ](docs/SETUP.md) | 環境変数とローカル実行手順 |
| [API仕様書](data/openapi.yaml) | REST API のエンドポイント定義 |
| [WS仕様書](data/asyncapi.yaml) | WebSocket メッセージの定義 |
| [WS API リファレンス](docs/WS_REFERENCE.md) | WebSocket API の解説 |
| [データ設計書](docs/DATA_DESIGN.md) | テーブル定義 |
| [ブランチ・CI/CD](docs/BRANCHING.md) | ブランチ運用と CI/CD の構成 |
| [ADR](https://github.com/kenyamaneko/overload-party-common/tree/main/docs/adr)（commonリポジトリ） | 設計判断の背景・理由・結果 |
| [システム構成図](https://github.com/kenyamaneko/overload-party-common#システム構成図)（commonリポジトリ） | Overload Party 全体の構成図 |
| [テスト観点カタログ](https://kenyamaneko.github.io/overload-party-gateway/) | テスト名から自動生成した、テスト済みの観点一覧 |
