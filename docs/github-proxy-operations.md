# GitHub Proxy の運用

`config/github-proxy` の前に Identity Issuer と Node Proxy を配置する。`codex-security` に ConfigMap `codex-github-app` の `app-id` と、同名の Secret の `private-key.pem` を用意する。Kubernetes API への経路は `scripts/render-web-proxy-kube-egress.sh github` で生成する。

GitHub App は認可対象リポジトリにインストールする。必要なリポジトリ権限は Contents、Pull requests、Checks、Metadata。保存する各 installation token は対象リポジトリと現在の操作に限定する。GitHub REST API は `2026-03-10` に固定する。

push 対象は `codex/<Session UID>/` 以下のブランチ。対象 prefix に force push と削除を拒否する有効な ruleset を設定し、GitHub App に bypass 権限を与えない。非信頼 PR の変更を特権付き workflow で実行しない。App に Workflows の書き込み権限を付与しない。

Git remote には `http://198.18.0.11:8080/<owner>/<repository>.git` を指定する。PR の作成、タイトル・本文更新、状態・チェック確認には同じ origin の `/v1/pulls` API を使う。

Zero Trust SandboxではCodex App Serverへ `codex_github_proxy` MCP serverを登録し、PRの作成・更新・状態・CIチェック確認を提供する。MCP serverは `/v1/pulls` APIだけを呼び、Repository、Session UID、ブランチprefix、base branchの認可はGitHub Proxyが強制する。PR merge、Repository設定、Secrets取得ツールは公開しない。
