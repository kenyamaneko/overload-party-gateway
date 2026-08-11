# overload-party-gateway

クライアントが唯一通信する薄い WS/REST ゲートウェイ。認証・ルーティング・WebSocket リレーを担い、ドメインロジックはすべて下流サービスに委譲する。

REST エンドポイント契約は [data/openapi.yaml](data/openapi.yaml)、設計判断 (Why) は [common の ADR](https://github.com/kenyamaneko/overload-party-common/tree/main/docs/adr) を参照。サービス構成全体の図は [common のシステム構成図](https://github.com/kenyamaneko/overload-party-common#システム構成図) を参照。環境変数・ローカル実行は [docs/SETUP.md](docs/SETUP.md) を参照。

[テスト観点カタログ](https://kenyamaneko.github.io/overload-party-gateway/): テスト名から生成した、テスト済みの観点の一覧。
