# メッセージ受付とrun開始

runnerが設定されたRunner APIでは、次のリクエストがrun開始まで進みます。

```http
POST /v1/conversations/conversation-1/messages
Content-Type: application/json

{"content":"リポジトリを確認してください","threadId":"thread-1"}
```

レスポンスには保存されたメッセージとrunが含まれます。

```json
{
  "message": {"id": "message-1", "status": "queued"},
  "run": {"id": "run-1", "status": "running", "thread_id": "thread-1"}
}
```

`threadId`が空の場合は、libSQLに保存された会話の最新threadがあれば再開し、なければ新しいthreadを作成します。既存のclient message IDを再送した場合は、二重実行を避けるため保存済みメッセージを返します。

Kubernetes構成では、メッセージ保存後にCodexSessionをActiveへ戻し、SessionActivatorがrunner PodのReadyを待ってからrunを開始します。App ServerイベントはlibSQLとSSEへ保存・配信されます。
