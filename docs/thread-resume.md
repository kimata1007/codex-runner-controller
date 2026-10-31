# 会話threadの再開

クライアントはthread IDを永続化しなくても同じConversationを継続できます。Runner APIがメッセージを受け取ったとき、`threadId`が明示されていなければ、libSQLの`runs`からその会話に紐づく最新のthread IDを取得します。

- 最新threadがある場合: runner-agentへ`thread/resume`を送る
- まだthreadがない場合: runner-agentへ`thread/start`を送る
- その後の`turn/start`は新しいRun IDで実行する

thread IDはrunレコードに保存されるため、PWAのsessionStorageを失った場合やセッションPodをidle timeoutで削除して再作成した場合も、PVCとApp Serverの状態を使って同じ会話を再開できます。クライアントが明示的な`threadId`を送った場合は、その値を優先します。
