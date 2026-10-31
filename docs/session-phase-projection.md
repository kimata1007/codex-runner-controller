# CodexSession phase projection

Runner APIは実行イベントをRunへ投影すると同時に、会話の`CodexSession.status.phase`へ観測用の状態を投影します。

| 実行状態 | CodexSession phase |
| --- | --- |
| run開始後 | `Busy` |
| 承認要求 | `WaitingForApproval` |
| 完了イベント | `Ready` |
| 失敗または中断 | `Failed` |

ControllerはReadyなPodをreconcileしても、`Busy`と`WaitingForApproval`を`Ready`へ上書きしません。Podが失われた場合はControllerが`Starting`へ戻し、ready後は実行イベントまたは次のメッセージによってphaseを更新します。
