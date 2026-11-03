# CodexSession契約

`CodexSession`は、会話を実行するために必要なKubernetes上の望ましい状態を表す。メッセージ本文やCodexのイベントはCRDへ保存せず、libSQLへ保存する。

このPRでは、次の2つを同じ契約として固定する。

- `api/v1alpha1`: ControllerやAPIが利用するGoのドメイン型と入力検証
- `config/crd/bases/codex.example.com_codexsessions.yaml`: Kubernetes APIへ登録するCRDスキーマ

ライフサイクルは`spec.desiredState`で表す。`Active`ならControllerはPVCとrunner Podを用意し、`Suspended`ならPodを削除して会話履歴とPVCを保持する。Pod作成や削除、idle timeoutの判定は次のPR以降で実装する。

チャットと実装を分けるモードは設けない。会話中にリポジトリ作業が必要になった時点で、任意の`spec.repository`を設定する。
