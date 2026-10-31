# runner-agentからのイベント転送

App Serverは、要求と応答に使う標準入出力の接続を通じて、非同期に通知を送ります。runner-agentはその通知を受け取り、Runner APIの内部イベントエンドポイントへ転送します。

```text
POST {RUNNER_EVENT_URL}/{runId}/events
Content-Type: application/json

{"eventType":"item/updated","payload":{...}}
```

`RUNNER_EVENT_URL`は省略可能です。未設定の場合、agentはイベントをログに記録しますが、ネットワーク要求は送りません。これにより、ローカル開発やプロトコルのテストをRunner APIから独立して行えます。

コールバックは短いタイムアウトを設定して非同期にイベントを転送します。そのため、APIが遅い場合や一時的に利用できない場合も、App ServerのJSON-RPC読み取りを妨げません。次の層では、受信エンドポイントを実装し、libSQLへの保存とSSEによる配信を接続します。
