# CI

`.github/workflows/ci.yaml`は、Pull Requestと`main`へのpushで次を確認します。

Workflowは`ubuntu-24.04`上で実行し、`actions/checkout`は`v7.0.1`、`actions/setup-go`は`v7.0.0`を使います。

- `go test ./...`
- `go vet ./...`
- `kubectl kustomize config`が成功すること

CIは外部のGitHub tokenやCodex tokenを必要としません。イメージpublishやクラスタへのdeployは別のリリース工程として扱い、権限の範囲を広げないようにしています。
