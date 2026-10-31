# セッション起動の責務境界

メッセージAPIは、メッセージを保存したあと、runを開始する前に`SessionActivator`を呼びます。

```text
POST /v1/conversations/{conversationID}/messages
  -> message repository
  -> SessionActivator.Activate(conversationID)
  -> MessageRunner.Start(...)
```

この境界の役割は「その会話のrunner-agentへ到達できる状態を作る」ことです。APIハンドラはKubernetes API、Pod、Service DNSを直接扱いません。後続のアダプターが`CodexSession`を作成またはActiveへ更新し、Serviceのreadinessを待つ実装を担います。

アクティベーションに失敗した場合は`503 Service Unavailable`を返します。保存済みのユーザーメッセージは再試行可能な履歴として残るため、クライアントは同じ`clientMessageId`で再送できます。
