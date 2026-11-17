# Kindによるメッセージ送信E2Eテスト

GitHub Actionsで一時的なKindクラスタを作成し、**実際のRunner API・Controller・Runner Agent**を起動します。Agentが起動する子プロセスのCodex App Serverだけを、認証不要のJSON-RPC/stdioテスト用プログラムへ差し替えます。これにより、実APIからのメッセージが子プロセスまで届く経路を、OpenAI APIへの接続なしに検証できます。

## 検証する一連の処理

1. 本物のRunner API、Controller、libSQLのPodがReadyになるまで待つ
2. Runner APIの`POST /v1/conversations`で会話を作成する
3. 同じAPIの`POST /v1/conversations/{id}/messages`で固定のテストメッセージを送信する
4. Runner APIがlibSQLにメッセージを保存し、Kubernetesに`CodexSession`を作成する
5. Controllerが`CodexSession`を非同期にReconcileし、PVC・Service・Podを作る
6. **本物のrunner-agentバイナリ**が起動し、子プロセスをJSON-RPC/stdioで初期化する
7. APIからAgent HTTPを経由し、子プロセスが`thread/start`と`turn/start`を受信して、受信したテスト本文を検証する
8. 子プロセスが`item/agentMessage/delta`と`turn/completed`を通知する
9. AgentがRunner APIのイベント受信エンドポイントに通知を転送し、**Runner APIのSSEでlibSQLに保存済みの`item/agentMessage/delta`と`turn/completed`を検証**してから、ユーザーメッセージの完了状態とテスト用アシスタント応答が保存されることをAPIで再確認する
10. 別の`CodexSession`で、存在しないSecretを参照したPodが`CreateContainerConfigError`を返すことを検証する

**重要:** Codex App Serverだけはテストダブルです。実際のCodex CLIの内部実装・モデル実行・外部認証・課金を再現するものではありません。Go AgentのHTTP/JSON-RPC処理は実コードを実行します。

## CIの構成

`tests/e2e/kustomization.yaml`が本番のCRD、RBAC、Deployment、StatefulSetを再利用し、テスト用ローカルイメージとダミーのSecretへ差し替えます。Kustomizeのビルドは、管理対象リポジトリ内の共通マニフェストを参照するため、E2E専用の`--load-restrictor=LoadRestrictionsNone`を付けて実行します。

`tests/e2e/fake-app-server/`でテスト用のAgentコンテナをビルドします。実際の`cmd/runner-agent`と、JSON-RPC/stdioで応答する最小の`codex`子プロセスを1つのイメージに入れます。AgentのHTTP APIやイベント転送をモックしません。

失敗時は`tests/e2e/collect-debug.sh`でKubernetes Events、Pod状態、Controller/API/libSQLログを収集して`kind-e2e-debug` Artifactに保存します。本番の認証情報は使用しません。CIで検証するテストメッセージは固定のダミー文字列です。

## ローカルでの実行

Docker、Kind、kubectl、curl、jqが必要です。

```sh
kind create cluster --name codex-e2e
docker build -f Dockerfile.runner-api -t local/runner-api:e2e .
docker build -f Dockerfile.runner-controller -t local/runner-controller:e2e .
docker build -f tests/e2e/fake-app-server/Dockerfile -t local/runner-agent:e2e .
kind load docker-image --name codex-e2e local/runner-api:e2e local/runner-controller:e2e local/runner-agent:e2e
bash tests/e2e/run.sh
kind delete cluster --name codex-e2e
```

## 対象外

- **本物のCodex CLIのプロトコル互換性**、ChatGPT認証、モデル実行、外部API、Gitのクローン
- Grafana、Tempo、OpenTelemetry Collector、Tailscale Operator、および本番のSecretやIngress
- 非同期ControllerのReconcileと親APIリクエストを同じOpenTelemetry Trace IDにまとめること

CIではエンドツーエンドの業務フローを再現しますが、上記の対象外領域には別の統合テストや本番環境のSmoke Testが必要です。
