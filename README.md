# Codex Runner Controller

Kubernetes上で会話単位のCodexセッションを起動・停止・再開する基盤です。Tailnet内のPWAからRunner APIへ接続し、Runner APIがlibSQLとCodexSessionリソースを管理します。

## 構成

- `runner-api`: 会話、メッセージ、run、App Serverイベントを保存し、セッションPodへ処理を依頼するHTTP API
- `runner-controller`: `CodexSession` CRDを監視し、会話ごとのPVC、Service、セッションPodを作成・削除するController
- `runner-agent`: セッションPod内でCodex App Serverを子プロセスとして起動し、HTTP APIとstdio JSON-RPCを橋渡しするプロセス
- `libSQL/sqld`: 会話とイベントの永続DB。Podを削除しても会話履歴を保持する
- `web`: Tailnet内から利用する同一OriginのレスポンシブPWA

会話開始時にセッションPodを作成し、既存会話なら同じPVC上のthreadを再開します。既定では30分間アクティビティがないとPodだけを削除します。CRD、PVC、Service、libSQLの会話データは残るため、次のメッセージでPodを再作成できます。

詳しい設計は [docs/design.md](docs/design.md)、C4モデルとシーケンス図は [docs/diagrams.md](docs/diagrams.md) を参照してください。

libSQLを設定し、`RUNNER_AGENT_URL`を空にすると、Runner APIは会話ごとの`CodexSession`をKubernetes上で起動してrunを開始します。`RUNNER_AGENT_URL`を設定すると、単一のrunner-agentへ接続する開発用経路になります。`CODEX_MODEL`、`CODEX_CWD`、`CODEX_APPROVAL_POLICY`はthread作成/再開時のオプションです。Kubernetesセッションのnamespace、PVC容量、StorageClassは`RUNNER_SESSION_NAMESPACE`、`RUNNER_SESSION_DATA_SIZE`、`RUNNER_SESSION_STORAGE_CLASS`で指定できます。

## 開発

```bash
go test ./...
go vet ./...
sh scripts/runner-agent-entrypoint_test.sh
kubectl kustomize config > /tmp/codex-runner-manifests.yaml
```

## コンテナイメージ

イメージはコンポーネントごとにビルドします。

```bash
docker build -f Dockerfile.runner-api \
  --tag ghcr.io/kimata1007/codex-runner-api:dev .
docker build -f Dockerfile.runner-controller \
  --tag ghcr.io/kimata1007/codex-runner-controller:dev .
docker build --build-arg CODEX_CLI_VERSION="$(cat CODEX_CLI_VERSION)" \
  -f Dockerfile.runner-agent \
  --tag ghcr.io/kimata1007/codex-runner-agent:dev .
docker build -f Dockerfile.runner-pwa \
  --tag ghcr.io/kimata1007/codex-runner-pwa:dev .
```

`Dockerfile.runner-agent` はGit取得に使う `git` と、固定したCodex CLIのバージョンを含みます。`CODEX_CLI_VERSION`ファイルにバージョンを固定し、CIとリリースで同じ値を使います。

## Kubernetesデプロイ

1. リリースPRをマージすると、SemVerタグ付きの4つのイメージがGHCRへ公開され、共通Kustomizeマニフェストのイメージタグも同じバージョンに更新されます。リリース方法は[リリース手順](docs/release-process.md)を参照してください。
2. 同じNamespaceにRunner APIとrunner-agent間で共有するSecretを作成します。

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: codex-runner-tokens
  namespace: codex-system
type: Opaque
stringData:
  api-token: replace-me
  agent-token: replace-me
  libsql-auth-token: replace-me
```

`api-token`はrunner-agentからRunner APIの内部イベントAPIへの接続に使います。PWAの利用者向けAPIはTailnet内からのみ公開し、利用者にBearer tokenを入力させません。

3. GitHubなどのprivate HTTPSリポジトリを使う場合は、Git tokenを別Secretの `token` キーへ保存し、Runner API Deploymentの `CODEX_GIT_CREDENTIALS_SECRET_NAME` にそのSecret名を設定します。Secretの値をURLやCRDへ埋め込まないでください。詳細は [docs/repository-workspace-bootstrap.md](docs/repository-workspace-bootstrap.md) を参照してください。
4. Tailscaleのサーバーまたはクラスタ内からのみPWA/APIへ到達できるよう、Tailnet側のリバースプロキシ、Service公開、またはport-forwardを設定します。Ingressをインターネットへ公開する設定はこのリポジトリには含めていません。

```bash
kubectl apply -k config/
```

環境ごとの差分はKustomize overlayで管理し、イメージタグ、Secret参照、リソース要求、公開方法をoverlay側で上書きしてください。

## APIとクライアント

モバイル側はKubernetesやCodex App Serverを直接知りません。PWAまたは将来のネイティブアプリはRunner APIのJSON APIとSSEだけを利用します。音声入力は後から同じメッセージAPIへ接続できます。

ローカルでは `LIBSQL_URL` 未設定時にメモリRepositoryへフォールバックできますが、再起動で消えるため本番利用には使いません。Kubernetesでは `LIBSQL_URL` をlibSQL Serviceへ向けます。API認証とagent間認証の環境変数は [docs/api-authentication.md](docs/api-authentication.md) を参照してください。

```bash
curl http://localhost:8080/healthz
curl -X POST http://localhost:8080/v1/conversations \
  -H 'Content-Type: application/json' \
  -d '{"mode":"chat"}'
```
