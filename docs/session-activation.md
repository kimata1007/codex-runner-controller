# セッション起動の責務境界

メッセージAPIは、新しいメッセージを保存する前に`SessionActivator`を呼び、セッションが使える状態になってからrunを開始します。会話の存在と`clientMessageId`による重複を先に確認します。

```text
POST /v1/conversations/{conversationID}/messages
  -> conversation repository (existence and duplicate check)
  -> session.Activator.Activate({ConversationID: conversationID, RequestedAt: now})
  -> message repository
  -> MessageRunner.Start(...)
```

この境界の役割は「その会話のrunner-agentへ到達できる状態を作る」ことです。`session.Activator`はユースケース側のインターフェースで、Kubernetes APIの型を知りません。`kubernetes.SessionActivator`が会話IDと同名の`CodexSession`を作成するか、SuspendedならActiveへ戻し、コントローラーが状態をReadyにするまで待ちます。APIハンドラーはKubernetesリソースの作成や状態判定を直接持ちません。

アクティベーションに失敗した場合は`503 Service Unavailable`を返し、メッセージを`queued`へ戻して再試行可能にします。同じ`clientMessageId`で安全に再試行できます。成功済みメッセージの重複要求ではセッションを再起動せず、既存のメッセージを返します。
