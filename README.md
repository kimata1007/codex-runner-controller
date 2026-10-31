# Codex Runner Controller

Kubernetes上で会話単位のCodexセッションを起動・停止・再開するための基盤です。
全体設計は [docs/design.md](docs/design.md)、アーキテクチャ図と主要フローは [docs/diagrams.md](docs/diagrams.md) にまとめています。

## 開発状況

最初の実装PRでは、後続のコンポーネントが依存するRunner APIの最小HTTP契約を実装しています。

- `GET /healthz`, `GET /readyz`
- `POST /v1/conversations`
- `GET /v1/conversations/{id}`
- `POST /v1/conversations/{id}/messages`
- `GET /v1/conversations/{id}/messages`
- `clientMessageId`によるメッセージ再送の冪等性

現在のストアはプロセス内メモリです。これはAPI契約とテストを先に固定するための段階的実装であり、プロセス再起動でデータが消えるため本番利用はできません。次のPRでlibSQL永続化へ置き換えます。

## ローカル実行

```bash
go test ./...
go run ./cmd/runner-api
```

別のターミナルから確認できます。

```bash
curl http://localhost:8080/healthz
curl -X POST http://localhost:8080/v1/conversations \
  -H 'Content-Type: application/json' \
  -d '{"mode":"chat"}'
```

## クライアント方針

モバイル側はKubernetesやCodex App Serverを直接知りません。Runner APIのHTTPS JSON APIと、後続PRで追加するSSEイベントAPIだけを利用します。

音声入力を後回しにする現段階では、まずレスポンシブWeb/PWAを作る方針を推奨します。Tailnet内のブラウザからすぐ利用でき、アプリストア配布なしでAPI契約と会話UIを検証できます。プッシュ通知、バックグラウンド実行、ネイティブ音声などが必要になった時点で、同じAPIクライアントをExpoへ移す選択肢を残します。
