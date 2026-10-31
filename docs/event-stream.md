# イベントストリーム

Runner APIは、App Serverから受け取ったrunイベントをクライアントへ配信するために、永続ストアの履歴とプロセス内Brokerを組み合わせます。

## 配信経路

1. runner-agentがCodex App Serverのイベントを受け取る。
2. Runner APIがイベントをlibSQLへ保存する。
3. 保存したイベントをBrokerへpublishする。
4. `GET /v1/runs/{runId}/events` がSSE接続中のクライアントへ通知する。

Brokerはライブ配信のための短期経路です。Podの再起動やSSEの再接続で失われたイベントを復元できるよう、クライアントはStoreの履歴を先に読み込みます。

## 再接続

クライアントは最後に受信したイベントのsequenceを、SSEの `Last-Event-ID` ヘッダーまたは `after` クエリで指定できます。

```http
GET /v1/runs/run-123/events?after=41
Last-Event-ID: 41
```

サーバーはsequence 41より後の保存済みイベントを返した後、新しいイベントをライブ配信します。Brokerからsequenceの飛びを検知した場合も、Storeから不足分を再取得します。

## 遅いクライアント

Brokerのsubscriberチャネルは有限バッファで、遅いクライアントへのpublishはブロックしません。バッファから落ちたイベントは、次のsequenceの飛びとしてSSEハンドラーが検知し、Storeから再生します。

このため、Brokerは配送保証を担わず、永続Storeが配送保証の基盤になります。
