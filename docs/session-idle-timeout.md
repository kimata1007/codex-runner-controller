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
