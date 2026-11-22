# セッションリソース生成

`internal/kubernetes`は`CodexSessionSpec`から、Controllerが作成するPVCとrunner Podの仕様を生成する。

この段階ではKubernetes APIへ書き込まない。リソースを純粋な関数に近い形で組み立ててテストし、次のController PRでCreate/Get/Delete処理とowner referenceを接続する。

生成するリソースは次のとおり。

- PVC: `sessionData.size`をストレージ要求として使用し、Pod削除後のCodex履歴とワークスペースを保持する
- Pod: 明示的に固定したrunner-agentイメージ、会話・承認ポリシーの環境変数、セッションPVC、readiness/liveness probeを持つ

`ResourceBuilder.RunnerImage`は必須とし、バージョンタグのないイメージや浮動する`latest`タグは拒否する。`CODEX_HOME`はセッションPVCへ向ける。runner-agentイメージのビルドとプローブ用HTTPサーバーの実装は後続PRの責務とする。

PVCはCodexSessionが残っている間、アイドル時のPod削除後も保持され、次のPod起動時に再利用される。`sessionData.retain`はCodexSession自体を削除したときのPVCの扱いを指定し、`false`ならPVCも削除し、`true`ならPVCを残す。通常の会話ではPVCも削除するのが既定で、保持が必要な運用だけ`true`を指定する。リソース生成関数はPVCを常に同じ名前で返すため、再concile時も既存PVCへ収束できる。
