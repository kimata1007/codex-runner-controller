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

`LIBSQL_URL`を設定すると、起動時にlibSQL/sqldへスキーマを適用して永続Repositoryを使用します。未設定時はローカル開発用のプロセス内メモリRepositoryです。メモリのデータは再起動で消えるため、本番利用にはlibSQLを設定してください。

libSQLを設定し、`RUNNER_AGENT_URL`を空にすると、Runner APIは会話ごとの`CodexSession`をKubernetes上で起動してrunを開始します。`RUNNER_AGENT_URL`を設定すると、単一のrunner-agentへ接続する開発用経路になります。`CODEX_MODEL`、`CODEX_CWD`、`CODEX_APPROVAL_POLICY`はthread作成/再開時のオプションです。Kubernetesセッションのnamespace、PVC容量、StorageClassは`RUNNER_SESSION_NAMESPACE`、`RUNNER_SESSION_DATA_SIZE`、`RUNNER_SESSION_STORAGE_CLASS`で指定できます。

## ローカル実行

```bash
go test ./...
go run ./cmd/runner-api
```

libSQL/sqldを使う場合:

```bash
LIBSQL_URL=http://localhost:8081 \
LIBSQL_AUTH_TOKEN=... \
go run ./cmd/runner-api
```

この例ではsqldがポート8081、Runner APIがポート8080で動いている前提です。認証なしのsqldなら`LIBSQL_AUTH_TOKEN`は省略できます。

別のターミナルから確認できます。

```bash
curl http://localhost:8080/healthz
curl -X POST http://localhost:8080/v1/conversations
```

## クライアント方針

モバイル側はKubernetesやCodex App Serverを直接知りません。Runner APIのHTTPS JSON APIと、後続PRで追加するSSEイベントAPIだけを利用します。

音声入力を後回しにする現段階では、まずレスポンシブWeb/PWAを作る方針を推奨します。Tailnet内のブラウザからすぐ利用でき、アプリストア配布なしでAPI契約と会話UIを検証できます。プッシュ通知、バックグラウンド実行、ネイティブ音声などが必要になった時点で、同じAPIクライアントをExpoへ移す選択肢を残します。
