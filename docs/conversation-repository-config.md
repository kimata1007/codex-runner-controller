# 会話に紐づくリポジトリ

会話はリポジトリを指定せずに作成できます。実装対象を使う場合は、会話作成時に `repository` のURLとrevisionを渡します。

```json
{
  "repository": {
    "url": "https://github.com/example/repo.git",
    "revision": "main"
  }
}
```

設定はlibSQLの `conversation_repositories` テーブルへ保存されます。メッセージを処理するときに設定を `SessionActivator` へ渡し、Kubernetesアダプターが `CodexSession.spec.repository` に反映します。

認証情報はリポジトリURLや会話データに含めず、Secret参照として扱います。
