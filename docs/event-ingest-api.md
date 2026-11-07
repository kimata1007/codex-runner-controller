# 内部イベント受信API

runner-agentはCodex App Serverの通知を、Runner APIの内部エンドポイントへ送信します。

```http
POST /internal/runs/{runId}/events
Content-Type: application/json

{"eventType":"item/updated","payload":{...}}
```

受信ハンドラーは次の順で処理します。

1. イベントの形式を検証する
2. イベントストアに保存し、run内の連番を取得する
3. 保存済みイベントをプロセス内の配信先へ通知する
4. 保存したイベントをrunner-agentへ返す

保存に失敗した場合は配信先へ通知しません。再接続後に再生できないイベントを、SSEクライアントが受信するのを防ぎます。

このルートは、ユーザー向けの`/v1`ルートとは分けています。デプロイ時はクラスターネットワーク内だけに公開し、別のnamespaceから接続させる場合はサービス間認証を追加してください。
