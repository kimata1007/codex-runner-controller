# 会話と実装対象リポジトリ

会話はリポジトリなしで作成できます。実装対象を指定する場合は、会話作成時にrepository設定を渡します。`mode`フィールドはありません。

```json
{
  "repository": {
    "url": "https://github.com/example/repo.git",
    "revision": "main"
  }
}
```

```text
POST /v1/conversations
```

リポジトリ設定は会話の一部としてlibSQLの`conversation_repositories`テーブルへ保存され、メッセージ開始時に`SessionActivator`へ渡されます。Kubernetesアダプターはそれを`CodexSession.spec.repository`へ変換します。

認証情報やGitHub tokenはURLや会話レコードには保存しません。Runner APIの`CODEX_GIT_CREDENTIALS_SECRET_NAME`で既定Secretを指定します。Secretの`token`キーをrunner PodへSecretKeyRefで注入します。
