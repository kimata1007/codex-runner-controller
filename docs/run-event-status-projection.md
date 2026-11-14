# App Server通知からrun状態への反映

Runner APIはApp Serverの通知を永続化した後、完了通知のpayloadに含まれるturn状態をrunとメッセージの状態へ反映し、その後に接続中のSSE購読者へ配信します。

Codex App Serverの`turn/completed`通知には`turn.status`が含まれます。値に応じて、runを`completed`、`failed`、`interrupted`にし、関連するメッセージの状態も更新します。失敗時は`turn.error.message`をrunのエラーメッセージに保存します。

状態を判定できないpayloadやその他の通知はrun状態を変更せず、保存してSSEで配信します。承認要求はこの通知経路の対象外です。
