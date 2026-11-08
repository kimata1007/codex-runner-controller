# Runner APIでのApp Server通知受信

このファイルは[runner-agentの通知送信](appserver-notification-sender.md)に対応する受信側です。runner-agentはCodex App Serverからstdioで受け取った `AppServerNotification` にrun IDを結び付け、`AppServerRunEvent` としてRunner APIへ送ります。

```text
Codex App Server --stdio JSON-RPC--> runner-agent
  AppServerNotification(method, params)
       | runner-agentが現在のrun IDを付ける
       v
  AppServerRunEvent(runID, eventType=method, payload=params)
       | HTTP POST
       v
Runner API --保存--> runs.Event --配信--> SSEクライアント
```

```http
POST /internal/runs/{runId}/events
Content-Type: application/json

{"eventType":"item/updated","payload":{...}}
```

`runId`はApp Serverの通知に含まれていません。runner-agentが`turn/start`を受け付けたときに得たIDを保持し、後からstdioで届く通知に付けます。Runner APIはHTTP POSTを受け取ると、次の順で処理します。

1. `eventType`とJSON形式の`payload`を検証する
2. `runId`に紐付けた実行イベントをイベントストアへ保存し、run内の連番を取得する
3. 保存した`runs.Event`を接続中のSSE購読者へ通知する
4. 保存したイベントをHTTPレスポンスとしてrunner-agentへ返す

ここでいう「実行イベント」は、Codex App ServerのJSON-RPC通知を実行IDに紐付けて保存・配信するための記録です。App Serverの通知そのものや、HTTPの通知機能を指す言葉ではありません。保存に失敗した場合はSSE購読者へ通知しません。再接続後に再生できないイベントを、クライアントが受信するのを防ぎます。

このルートは、ユーザー向けの`/v1`ルートとは分けています。デプロイ時はクラスターネットワーク内だけに公開し、別のnamespaceから接続させる場合はサービス間認証を追加してください。
