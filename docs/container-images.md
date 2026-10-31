# コンテナイメージ

3つの実行単位を別イメージにします。

```sh
docker build -f Dockerfile.runner-api --tag ghcr.io/kimata1007/codex-runner-api:dev .
docker build -f Dockerfile.runner-controller --tag ghcr.io/kimata1007/codex-runner-controller:dev .
docker build --build-arg CODEX_CLI_VERSION=<pinned-version> \
  -f Dockerfile.runner-agent \
  --tag ghcr.io/kimata1007/codex-runner-agent:dev .
```

## runner-agentのCodex CLI

runner-agentは同じコンテナ内でCodex CLIを子プロセスとして起動するため、Node.jsベースのイメージにCodex CLIを同梱します。`CODEX_CLI_VERSION`は必須のbuild argで、`latest`を暗黙に使わず、CIまたはリリース設定で明示的に固定します。

APIとControllerはdistrolessのnon-rootイメージです。agentはCLIと作業ディレクトリが必要なため、Debian slim上でnon-rootユーザーを作成します。セッションPVCはDeploymentではなく、CodexSession ControllerがPodへマウントします。
