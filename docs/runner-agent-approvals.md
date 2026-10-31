# runner-agentの承認待ち

App Serverの承認requestをrunner-agent内の`ApprovalManager`で保持します。requestはイベントとしてRunner APIへ転送され、クライアントが承認結果を返すまでJSON-RPC responseを保留します。

runner-agentの内部API:

```text
POST /v1/approvals/{jsonRpcRequestId}
{"decision":"accept"}
```

HTTP APIはBearer認証で保護されます。`response`フィールドへApp Serverプロトコルの完全なJSONを渡す形式も利用できます。承認待ちの実体はPod内のApp Server接続にあり、Podが削除された場合は承認も無効になるため、次のメッセージでthreadを再開します。
