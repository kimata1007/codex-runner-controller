# 承認の永続化

承認要求はrunner-agentのJSON-RPC接続上で待機しますが、監査とUI復元のためにRunner APIが`approvals`テーブルへ保存します。

1. runner-agentが`approval/requested`イベントを送る。
2. Runner APIがイベントを保存し、`requestId`、method、paramsをpendingとして投影する。
3. クライアントが`POST /v1/approvals/{runID}/{requestID}`へ回答する。
4. Runner APIがrunner-agentへ回答を転送する。
5. 転送成功後、approvalをapprovedまたはrejectedへ更新する。

回答を転送できない場合は監査状態をpendingのまま保持します。トークンや会話本文はapprovalレコードへ追加しません。
