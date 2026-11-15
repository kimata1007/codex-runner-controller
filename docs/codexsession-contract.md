# CodexSession契約

`CodexSession`は、会話を実行するために必要なKubernetes上の望ましい状態を表す。メッセージ本文やCodexのイベントはCRDへ保存せず、libSQLへ保存する。

次の2つを同じ契約として管理する。

- `api/v1alpha1`: ControllerやAPIが利用するGoのドメイン型と入力検証
- `config/crd/bases/codexrunner.io_codexsessions.yaml`: Kubernetes APIへ登録するCRDスキーマ

ライフサイクルは`spec.desiredState`で表す。`Active`ならControllerはPVC、Service、runner Podを用意し、`Suspended`ならPodを削除して会話履歴とPVCを保持する。idle timeoutはstatusの最終活動時刻と`RequeueAfter`で判定する。`sessionData.retain`はCodexSession自体を削除したときの設定で、既定ではPVCも削除し、`true`を指定した場合だけPVCを残す。
