# runイベントと永続状態

Runner APIはApp Serverの通知を永続化した後、完了通知のpayloadに含まれるturn状態をrunとメッセージの状態へ反映し、その後に接続中のSSE購読者へ配信します。

Codex App Serverの`turn/completed`通知には`turn.status`が含まれます。値に応じて、runを`completed`、`failed`、`interrupted`にし、関連するメッセージの状態も更新します。失敗時は`turn.error.message`をrunのエラーメッセージに保存します。

状態を判定できないpayloadやその他の通知はrun状態を変更せず、保存してSSEで配信します。承認要求はこの通知経路の対象外です。

| イベント | run | message |
| --- | --- | --- |
| `turn/completed` | `completed` | `completed` |
| `turn/failed` | `failed` | `failed` |
| `turn/interrupted` | `interrupted` | `failed` |
| `approval/requested` | `waiting_approval` | `running` |

runner-agentから届くApp Serverイベントは、まず`events`テーブルへ保存し、その後にrun/messageの状態へ投影します。未知の通知やassistant本文として認識できないdeltaイベントはストリームへ流すだけで状態を変更しません。イベントの永続化後に投影が失敗しても、受信APIはイベントを再送させません。イベントログから再投影できるため、同じイベントを重複保存するより安全です。

`turn/completed`の投影では、同じrunに保存された`item/agentMessage/delta`イベントをsequence順に連結し、assistant messageを作成または更新します。assistant messageのIDはrunから決まるため、投影を再実行しても履歴に重複行を作りません。
