# runner-agentとrunの対応付け

App Serverの通知はturn開始HTTPレスポンスとは別に、stdio上のJSON-RPC通知として届きます。そのためrunner-agentはturn開始時に受け取った`runId`を`RunTracker`に保存し、非同期通知の処理時に現在のrun IDを参照できるようにします。

```http
POST /v1/threads/thread-1/turns
Content-Type: application/json

{"text":"hello","runId":"run-1"}
```

セッションPodは同時に複数turnを処理しない前提です。次のPRで、`RunTracker`を使ってApp ServerイベントをRunner APIのイベント受信エンドポイントへ転送します。
