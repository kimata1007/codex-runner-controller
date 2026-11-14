# CodexSession phase projection

`CodexSession`は会話ごとにKubernetes APIへ保存されるカスタムリソースです。ここでいうphaseはDeploymentやPodの状態ではなく、そのリソースの`status.phase`欄です。

Runner APIは実行イベントをRunへ投影すると同時に、会話の`CodexSession.status.phase`へ現在の実行状態を記録します。Controllerはこのphaseをアイドルタイムアウトの判定にも使い、`Busy`または`WaitingForApproval`の間は実行中のPodを停止しません。

| 実行状態 | CodexSession phase |
| --- | --- |
| run開始後 | `Busy` |
| 承認要求 | `WaitingForApproval` |
| 完了イベント | `Ready` |
| 失敗または中断 | `Failed` |

ControllerはReadyなPodをreconcileしても、`Busy`と`WaitingForApproval`を`Ready`へ上書きしません。Podが失われた場合はControllerが`Starting`へ戻し、ready後は実行イベントまたは次のメッセージによってphaseを更新します。
