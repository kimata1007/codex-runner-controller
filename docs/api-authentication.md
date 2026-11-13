# API認証

Tailscaleはネットワーク境界を提供しますが、Runner APIとrunner-agentのHTTPリクエスト自体もBearerトークンで保護します。

## 設定

- `RUNNER_API_TOKEN`: モバイル/PWAクライアントがRunner APIへ送るトークン
- `RUNNER_AGENT_TOKEN`: Runner APIがrunner-agentへ送る内部トークン

どちらもKubernetes Secretから環境変数へ注入します。空の場合は開発用に認証を無効化しますが、本番マニフェストでは必須です。

`/healthz`と`/readyz`はServiceのprobeから利用できるよう認証不要です。それ以外のルートは`Authorization: Bearer <token>`が必要です。トークンは定数時間比較し、レスポンスにトークン値を含めません。

認証はTailscaleの代替ではありません。Tailscale ACLで到達元を絞り、アプリケーション認証で誤接続や内部サービスのなりすましを防ぐ二重の境界として扱います。
