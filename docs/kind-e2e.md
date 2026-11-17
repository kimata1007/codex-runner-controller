# KindによるKubernetes E2Eテスト

GitHub Actionsの`Kubernetes E2E (Kind)`ジョブは、PRごとに一時的なKubernetesクラスタを構築し、実際のDockerイメージを起動して確認します。

## 何を検証するか

- Runner API・Controller・libSQLをKindの実Podとして起動し、DeploymentとStatefulSetがReadyになること
- Runner APIにHTTPでアクセスし、libSQLへ会話データを保存できること
- `CodexSession`を作成すると、ControllerがPVC・Service・Podを作成し、セッションが`Ready`になること
- 存在しないSecretを参照するセッションPodで、Kubernetesが`CreateContainerConfigError`を報告すること

## CIの設計

`tests/e2e/kustomization.yaml`は本番用の`config/`にあるCRD、RBAC、Deployment、StatefulSetを再利用します。CIではイメージ参照とトークンなどをテスト専用に差し替えます。資格情報はすべて**テスト用の固定ダミー値**であり、本番の認証情報を使いません。

Agentは`tests/e2e/fake-agent/`内の簡易HTTPサーバーを使います。Codex CLIやOpenAI APIへの実アクセスは行いません。そのため、**本物のCodex App ServerとのJSON-RPC互換性や外部認証の成功を保証するテストではありません**。

失敗時には`collect-debug.sh`がPodの状態、Events、Deployment・StatefulSetのログを収集し、GitHub ActionsのArtifact（`kind-e2e-debug`）に保存します。Secretの**値**やPodの環境変数一覧は収集しません。ログの中に機密情報が混入しないよう、本番のSecretをCIへ持ち込まないでください。

## ローカル実行

Docker、Kind、kubectlが必要です。実行例:

```sh
kind create cluster --name codex-e2e
docker build -f Dockerfile.runner-api -t local/runner-api:e2e .
docker build -f Dockerfile.runner-controller -t local/runner-controller:e2e .
docker build -f tests/e2e/fake-agent/Dockerfile -t local/runner-agent:e2e .
kind load docker-image --name codex-e2e local/runner-api:e2e local/runner-controller:e2e local/runner-agent:e2e
bash tests/e2e/run.sh
```

終了後の削除:

```sh
kind delete cluster --name codex-e2e
```

## 対象外・限界

- Grafana、Tempo、OpenTelemetry Collector、Tailscale Operatorはこのジョブでは構築しません。
- 本番のKubernetes Secret、StorageClass、ネットワークACL、Tailscale MagicDNS、Google認証など、本番固有の条件は別途デプロイ後に検証する必要があります。
- KindのSecret不足テストは、同じ`CreateContainerConfigError`という問題の種類を検出できることを確認するもので、Grafana Helm ChartにSecretが存在するかを検証するものではありません。
- OpenTelemetryの実際のトレース保存を検証するE2Eは、Collector・Tempoの構築を含む追加テストとして検討します。

テストを追加した後も、実環境でのデプロイ成功・Pod Ready・Ingress到達性のスモークチェックは重要です。
