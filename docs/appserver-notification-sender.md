# runner-agentからRunner APIへの通知送信

Codex App Serverは、標準入出力を通じてJSON-RPC通知 (`AppServerNotification`) をrunner-agentへ非同期に送ります。runner-agentは現在のrun IDを付けた `AppServerRunEvent` を作り、Runner APIへHTTP POSTします。通知の `method` は `eventType` に、`params` は `payload` に対応します。受信側の処理は[Runner APIの通知受信](appserver-notification-receiver.md)にあります。

```text
POST {RUNNER_EVENT_URL}/{runId}/events
Content-Type: application/json

{"eventType":"item/updated","payload":{...}}
```

`RUNNER_EVENT_URL`は省略可能です。未設定の場合、agentは通知をログに記録しますが、ネットワーク要求は送りません。これにより、ローカル開発やプロトコルのテストをRunner APIから独立して行えます。

コールバックは短いタイムアウトを設定して非同期に送信します。そのため、APIが遅い場合や一時的に利用できない場合も、App ServerのJSON-RPC読み取りを妨げません。
