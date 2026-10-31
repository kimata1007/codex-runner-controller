# CI

`.github/workflows/ci.yaml`は、Pull Requestと`main`へのpushで次を確認します。

- `go test ./...`
- `go vet ./...`
- `kubectl kustomize config`が成功すること

CIは外部のGitHub tokenやCodex tokenを必要としません。イメージpublishやクラスタへのdeployは別のリリース工程として扱い、権限の範囲を広げないようにしています。
