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

会話の最終活動から7日が経過すると、Runner APIは会話を`archived`にします。通常の会話一覧から隠し、既存URLからの再開も受け付けません。Controllerは同じ最終活動時刻を基準にCodexSessionとPVCを削除し、ServiceはOwnerReferenceによるGCで削除します。libSQLの会話、メッセージ、run、イベント、承認記録は削除しません。heartbeatは最終活動時刻を更新するため、実行中や承認待ちのセッションも活動中として扱われます。

`lastActivityAt`はReady待ちの前に更新します。これにより、停止期限を過ぎたセッションの再開中に古い時刻を見てPodを再削除する競合を避けます。期限前ならcontrollerは期限まで`RequeueAfter`で待ちます。期限を過ぎた場合はPodを削除し、`Suspended` phaseを返します。`Spec.DesiredState`はActiveのままでも、期限切れの`lastActivityAt`を見て再作成を抑止するため、Pod削除イベントで直ちに再起動することはありません。新しいメッセージのアクティベーションが、次の起動を明示的に行います。Starting中、実行中（Busy）、承認待ち（WaitingForApproval）のセッションは停止しません。

`CodexSession`で`idleTimeoutSeconds`が0なら停止しません。Runner APIが作るセッションでは既定値1800秒を使用します。

実行中（Busy）または承認待ち（WaitingForApproval）の間はrunner-agentがheartbeatを送るため、heartbeatが継続する限りidle timeoutでは削除されません。
