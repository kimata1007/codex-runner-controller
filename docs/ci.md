# CI

`.github/workflows/ci.yaml`は、Pull Requestと`main`へのpushで次を確認します。

Workflowは`ubuntu-24.04`上で実行し、`actions/checkout`は`v7.0.1`、`actions/setup-go`は`v7.0.0`、`actions/setup-node`は`v7.1.0`を使います。Node.jsは`22.23.5`に固定します。

- `go test ./...`
- `go vet ./...`
- `node --check`によるWeb JavaScriptの構文検証とPWA manifestのJSON検証
- `kubectl kustomize config`が成功すること

CIは外部のGitHub tokenやCodex tokenを必要としません。イメージpublishやクラスタへのdeployは別のリリース工程として扱い、権限の範囲を広げないようにしています。
