# アイドルセッションの停止

セッションPodは会話履歴を保持する主体ではありません。`CodexSession`とlibSQL、セッションPVCを残したまま、一定時間ユーザー活動がなければPodだけを削除します。

```text
message API
  -> CodexSession.status.lastActivityAt を更新
  -> controller が idleTimeoutSeconds 後に再concile
  -> Podを削除
  -> CR / Service / PVC は保持

次のメッセージ
  -> desiredState=Active
  -> 同じCodexSessionを再利用
  -> 同じService/PVCでPodを再作成
```

`lastActivityAt`が期限前ならcontrollerは期限まで`RequeueAfter`で待ちます。期限を過ぎた場合はPodを削除し、`Suspended` phaseを返します。`Spec.DesiredState`はActiveのままでも、期限切れの`lastActivityAt`を見て再作成を抑止するため、Pod削除イベントで直ちに再起動することはありません。新しいメッセージのアクティベーションが、次の起動を明示的に行います。

既定の`idleTimeoutSeconds`は0（無効）です。本番ではセッション作成時の設定として明示的に与えます。
