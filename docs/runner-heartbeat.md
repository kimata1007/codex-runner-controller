# runner-agent heartbeat

App Serverのイベントが一定時間発生しない処理でも、セッションPodをidle timeoutで削除しないためにrunner-agentがheartbeatを送信します。

runner-agentはactive runまたは承認待ちの間、起動直後と30秒ごとに次の内部APIへBearerトークン付きPOSTを送ります。turnの完了、失敗、中断イベントを受けるとactive runをクリアし、heartbeatを停止します。これにより、通常の会話が完了した後はidle timeoutでPodを停止できます。

```text
POST /internal/sessions/{conversationID}/heartbeat
Authorization: Bearer <runner-agent-token>
```

Runner APIはKubernetesの`CodexSession.status.lastActivityAt`を更新します。Controllerはこの時刻をidle判定に使うため、長時間のtool実行や承認待ちでもheartbeatが継続する限りPodを削除しません。heartbeatはイベント保存とは分離しており、heartbeatの一時失敗はrunner-agentのApp Server処理を停止させません。
