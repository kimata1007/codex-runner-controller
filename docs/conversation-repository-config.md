# 会話と実装対象リポジトリ

`chat`会話はリポジトリなしで作成できます。`implement`会話は作成時に次の設定を要求します。

```json
{
  "mode": "implement",
  "repository": {
    "url": "https://github.com/example/repo.git",
    "revision": "main"
  }
}
```

リポジトリ設定は会話の一部としてlibSQLの`conversation_repositories`テーブルへ保存され、メッセージ開始時に`SessionActivator`へ渡されます。Kubernetesアダプターはそれを`CodexSession.spec.repository`へ変換します。

認証情報やGitHub tokenはURLや会話レコードには保存しません。Runner APIの`CODEX_GIT_CREDENTIALS_SECRET_NAME`で既定Secretを指定するか、CodexSessionの`credentialsRef`へSecret名を設定します。ControllerはSecretの`token`キーをrunner PodへSecretKeyRefで注入します。
