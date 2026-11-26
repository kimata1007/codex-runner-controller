# Sandbox Runtime の運用

実行ノードに gVisor の `runsc` と containerd shim を導入し、RuntimeClass の `runsc` ハンドラーを登録する。kubelet の `podPidsLimit` を設定し、セットアップ済みの対象ノードに `codexrunner.io/sandbox=gvisor` を付ける。`config/sandbox` は RuntimeClass、ResourceQuota、LimitRange、Session Pod の admission policy を作成する。

Session Pod は `codex-sessions` の Restricted Pod Security に従う。admission policy は gVisor の指定、ServiceAccount Token の無効化、読み取り専用ルートファイルシステムを要求する。書き込み可能なデータは Session PVC と容量を制限した一時領域に置く。

Kind のネットワーク受入テストは gVisor `release-20261123.0` を Linux のアーキテクチャごとの SHA-256 で検証する。
