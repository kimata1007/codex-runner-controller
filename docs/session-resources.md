# セッションリソース生成

`internal/kube`は`CodexSessionSpec`から、Controllerが作成するPVCとrunner Podの仕様を生成する。

この段階ではKubernetes APIへ書き込まない。リソースを純粋な関数に近い形で組み立ててテストし、次のController PRでCreate/Get/Delete処理とowner referenceを接続する。

生成するリソースは次のとおり。

- PVC: `sessionData.size`をストレージ要求として使用し、Pod削除後のCodex履歴とワークスペースを保持する
- Pod: runner-agentイメージ、会話・モード・承認ポリシーの環境変数、セッションPVC、readiness/liveness probeを持つ

`sessionData.retain`によるPVCの削除可否は、PodとPVCのライフサイクルを管理するController側で扱う。リソース生成関数はPVCを常に同じ名前で返すため、再concile時も既存PVCへ収束できる。
