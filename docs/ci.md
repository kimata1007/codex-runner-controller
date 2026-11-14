# CI

`.github/workflows/ci.yaml`は、Pull Requestと`main`へのpushで次を確認します。

Workflowは`ubuntu-24.04`上で実行し、`actions/checkout`は`v7.0.1`、`actions/setup-go`は`v7.0.0`、`actions/setup-node`は`v7.1.0`を使います。Node.jsは`22.23.5`に固定します。

- `go test ./...`
- `go vet ./...`
- `node --check`によるWeb JavaScriptの構文検証とPWA manifestのJSON検証
- `kubectl kustomize config`が成功すること
- 4つのプロジェクト用コンテナイメージをビルドできること（GHCRにはpushしません）

SemVerタグを作るrelease PRとGHCRへのイメージ公開は、`.github/workflows/tagpr-release.yaml`で行います。詳しくは[リリース手順](release-process.md)を参照してください。通常のCIはイメージを公開せず、タグPRのワークフローはActions標準の`GITHUB_TOKEN`だけを使います。
