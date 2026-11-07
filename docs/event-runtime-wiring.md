# イベント配信の起動時接続

libSQL repositoryを使って`runner-api`を起動すると、イベントの受信からSSE配信まで次の経路で動きます。

```text
runner-agent
  -> POST /internal/runs/{runId}/events
  -> libSQLのeventsテーブル
  -> プロセス内events.Broker
  -> GET /v1/runs/{runId}/events (SSE)
```

同じrepositoryをイベントの保存と読み出しに使います。プロセス内Brokerは接続中のクライアントへ更新を知らせるためのもので、イベントの正本ではありません。再接続時にはlibSQLから未受信のイベントを再生し、その後Brokerの通知を受け取ります。

memory repositoryではイベントストアを利用できないため、会話APIだけを公開します。イベント受信とSSEのルートは登録されません。
