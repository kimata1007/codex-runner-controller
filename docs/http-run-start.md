# メッセージ受付とrun開始

Runner APIにメッセージを送ると、メッセージを保存してから、設定済みのMessageDispatcherを通じてCodexのturnを開始できます。

```http
POST /v1/conversations/conversation-1/messages
Content-Type: application/json

{"content":"リポジトリを確認してください","threadId":"thread-1"}
```

`threadId`が空の場合は新しいthreadを開始します。指定した場合は既存threadを再開します。成功時は、保存されたメッセージと作成されたrunを返します。

```json
{
  "message": {"id":"message-1","status":"queued"},
  "run": {"id":"run-1","status":"running","thread_id":"thread-1"}
}
```

同じ`clientMessageId`の再送では、メッセージを二重登録せず、Codexのturnも再送しません。dispatcherを設定しないRunner APIは、従来どおりメッセージの保存だけを行います。
