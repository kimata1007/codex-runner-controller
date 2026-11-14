# Runner APIの承認プロキシ

PWAはrunner-agentへ直接接続しません。承認結果はRunner APIへ送り、runのConversation IDから対象セッションPodを解決してagentへ転送します。

```text
POST /v1/approvals/{runID}/{requestID}
{"response":{"decision":"accept"}}
  -> run storeでConversationを解決
  -> session Serviceを解決
  -> runner-agent POST /v1/approvals/{requestID}
  -> App ServerへJSON-RPC response
```

run IDをURLに含めることで、承認request IDがセッション内でのみ一意でも、Runner APIは正しいPodを選択できます。SSEの`/v1/runs/{runID}/events`とはパスを分けているため、同一originのルーティングで衝突しません。
