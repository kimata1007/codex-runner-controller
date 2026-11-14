# runner-agentの承認待ち

App Serverの承認requestをrunner-agent内の`ApprovalManager`で保持します。requestはイベントとしてRunner APIへ転送され、クライアントが承認結果を返すまでJSON-RPC responseを保留します。

runner-agentの内部API:

```text
POST /v1/approvals/{jsonRpcRequestId}
{"decision":"accept"}
```

HTTP APIはBearer認証で保護されます。`response`フィールドへApp Serverプロトコルの完全なJSONを渡す形式も利用できます。承認待ちの実体はPod内のApp Server接続にありますが、Runner APIは承認要求をlibSQLの`approvals`へpendingとして記録し、許可/拒否をrunner-agentへ転送した後にapproved/rejectedへ更新します。Podが削除された場合はPod内の承認待ち接続は無効になるため、保存済み履歴を確認して次のメッセージでthreadを再開します。
