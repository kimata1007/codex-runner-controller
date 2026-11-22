# アイドルセッションの停止

セッションPodは会話履歴を保持する主体ではありません。`CodexSession`とlibSQL、セッションPVCを残したまま、一定時間ユーザー活動がなければPodだけを削除します。

```text
message API
  -> session activation時に status.lastActivityAt を更新
  -> controller が idleTimeoutSeconds 後に再concile
  -> Podを削除
  -> CR / Service / PVC は保持

次のメッセージ
  -> desiredState=Active
  -> 同じCodexSessionを再利用
  -> 同じService/PVCでPodを再作成
```

`lastActivityAt`はReady待ちの前に更新します。これにより、停止期限を過ぎたセッションの再開中に古い時刻を見てPodを再削除する競合を避けます。期限前ならcontrollerは期限まで`RequeueAfter`で待ちます。期限を過ぎた場合はPodを削除し、`Suspended` phaseを返します。`Spec.DesiredState`はActiveのままでも、期限切れの`lastActivityAt`を見て再作成を抑止するため、Pod削除イベントで直ちに再起動することはありません。新しいメッセージのアクティベーションが、次の起動を明示的に行います。Starting中、実行中（Busy）、承認待ち（WaitingForApproval）のセッションは停止しません。

`CodexSession`で`idleTimeoutSeconds`が0なら停止しません。Runner APIが作るセッションでは既定値1800秒を使用します。

実行中（Busy）または承認待ち（WaitingForApproval）の間はrunner-agentがheartbeatを送るため、heartbeatが継続する限りidle timeoutでは削除されません。

## 7日間操作のない会話の自動アーカイブ

上記のアイドル停止（既定30分）は一時的なPodの停止で、会話の履歴とPVCを保持します。それとは別に、Runner APIは1時間ごとに会話の最終活動時刻を確認し、**7日間活動がない会話をアーカイブ**します。

- libSQLの会話ステータスを`archived`に変更し、メッセージ・Run・イベント・承認履歴のレコードは保持します。
- 通常の`GET /v1/conversations`から除外され、PWAの履歴一覧から消えます。PWAは1分ごと、または表示再開時に一覧を更新します。
- アーカイブ済みの会話をURLから再び開いたり、新しいメッセージで復活させたりすることはできません。対象APIは`410 Gone`を返し、PWAは新しい会話へ切り替えます。
- Runner APIが既存の`SessionActivator.DeleteSession`を使い、CodexSessionを削除します。所有されるPod・Service・PVCはKubernetesのGCで削除されます。ただし`SessionData.Retain`で保持設定したPVCはその設定に従います。
- `queued`・`running`・`waiting_approval`のRunが存在する会話は自動アーカイブの対象外です。実行中のHeartbeatはKubernetesとlibSQLの両方の活動時刻を更新します。
- Kubernetes側の削除が失敗した場合は次回のアーカイブ処理で再試行します。Runner APIが再起動しても、期限を過ぎたアーカイブ済みの会話を再走査します。

アーカイブ前後の競合を避けるため、会話の期限切れとアーカイブ済みの確認は履歴一覧だけでなく、会話取得・メッセージ投稿APIでも行います。実行済みの記録の恒久削除はこの機能の対象外です。
