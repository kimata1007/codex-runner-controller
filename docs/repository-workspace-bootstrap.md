# リポジトリワークスペースの準備

`CodexSession` には `spec.repository.url` と `spec.repository.revision` を指定します。通常のSessionではControllerがこれらをPodへ渡します。Zero Trust Sandboxでは、Canonicalな `https://github.com/<owner>/<repository>[.git]` URLだけを受け付け、同じowner/repositoryのGitHub access grantを必須にします。Podのエントリポイントはrunner-agentの起動前にPVC上のワークスペースを準備します。

## 初回起動

エントリポイントは最初にworkspaceディレクトリを作成します。リポジトリ指定がある場合、ワークスペースが空ならリポジトリをcloneした後、指定リビジョンをcheckoutします。リビジョンはブランチ名、タグ、コミットSHAを指定できます。リポジトリ指定がないchatモードでも、空のworkspaceを`CODEX_CWD`として利用できます。

Zero Trust Sandboxでは `spec.credentialsRef` を拒否します。gitのGitHub HTTPS URLは `http://198.18.0.11:8080/` に一時的に書き換えられ、Node ProxyのCNI経路とSession Identityで認可されます。ProxyはGitHub Appを使い、SessionにTokenを返しません。受信するRepository URLはAccess grantと一致しなければPod生成時に拒否されます。通常Sessionの既存 `credentialsRef` フローは従来どおり利用できます。

Secretの例:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: github-credentials
type: Opaque
stringData:
  token: github_pat_...
```

Secretは `CodexSession` と同じNamespaceに作成します。GitHubなどのHTTPS URLを使う場合、トークンには対象リポジトリを読むための最小権限だけを与えてください。このSecret方式は通常Session用です。

## セッションPodの再起動

ワークスペースに `.git` が存在する場合は、origin URLがCRDのURLと一致することを確認してから、エントリポイントが `fetch --all --prune` の後に指定リビジョンをcheckoutし、hard resetします。originが別リポジトリなら既存データを上書きせずに起動を失敗させます。未コミットの変更はPod再起動時に保持されないため、実装結果はCodexの通常のGit commit/pushフローでリモートへ保存します。

`.git` がなく、ワークスペースが空でもない場合は、既存データを上書きせずに起動を失敗させます。PVC上の意図しないファイルを削除しないための安全策です。

通常Sessionの初期実装はHTTPS認証を対象にします。Zero Trust SandboxはGitHub App Proxyを利用します。SSH deploy keyとSecret Manager連携は未対応です。
