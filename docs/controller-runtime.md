# controller-runtimeへの接続

`cmd/runner-controller`がKubernetes Managerを起動し、`CodexSessionController`を登録する。

起動前に固定タグ付きのrunner-agentイメージを`RUNNER_AGENT_IMAGE`で指定する。未設定の場合は起動しない。

処理の分担は次のとおり。

1. Managerが`CodexSession`、Pod、PVCのwatchを開始する。
2. `CodexSession`のイベントを受けたReconcilerが対象CRを取得する。
3. `KubernetesResourceClient`がcontroller-runtimeのClientを`ResourceClient`へ適合させる。
4. PR #9でテストした状態遷移ロジックがPVC/Podの作成・取得・削除を判断する。
5. Controllerが結果を`CodexSession.status`へ書き戻す。

PodとPVCにはCodexSessionをowner referenceとして設定する。これにより、Kubernetesの所有関係とControllerのwatchが一致し、作成した子リソースの変更で再concileできる。

現時点ではidle timeoutやrunner-agentのReady通知は未接続であり、次の実装でstatusと再concile周期へ追加する。
