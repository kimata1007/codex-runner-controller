# リポジトリワークスペースの準備

実装モードの `CodexSession` には `spec.repository.url` と `spec.repository.revision` を指定します。ControllerはこれらをセッションPodの環境変数へ渡し、Podのエントリポイントがrunner-agentを起動する前にPVC上のワークスペースを準備します。

## 初回起動

ワークスペースが空なら、エントリポイントはリポジトリをcloneした後、指定リビジョンをcheckoutします。リビジョンはブランチ名、タグ、コミットSHAを指定できます。HTTPSリポジトリで `spec.credentialsRef` を指定した場合は、Secretの `token` キーを `GIT_AUTH_TOKEN` として参照し、Gitの一時的なAuthorizationヘッダーに使います。トークンをリポジトリURLへ連結したり、DB・CRDへコピーしたりはしません。

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

Secretは `CodexSession` と同じNamespaceに作成します。GitHubなどのHTTPS URLを使う場合、トークンには対象リポジトリを読むための最小権限だけを与えてください。

## セッションPodの再起動

ワークスペースに `.git` が存在する場合は、origin URLがCRDのURLと一致することを確認してから、エントリポイントが `fetch --all --prune` の後に指定リビジョンをcheckoutし、hard resetします。originが別リポジトリなら既存データを上書きせずに起動を失敗させます。未コミットの変更はPod再起動時に保持されないため、実装結果はCodexの通常のGit commit/pushフローでリモートへ保存します。

`.git` がなく、ワークスペースが空でもない場合は、既存データを上書きせずに起動を失敗させます。PVC上の意図しないファイルを削除しないための安全策です。

この初期実装はHTTPS認証を対象にします。SSH deploy key、GitHub App token、Secret Manager連携は、Secretの取り扱いとローテーション方針を決めた後の拡張とします。
