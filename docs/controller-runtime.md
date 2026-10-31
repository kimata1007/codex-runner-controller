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

idle timeoutは`RequeueAfter`で期限まで再concileし、heartbeatで`lastActivityAt`を更新する。runner-agentのHTTP readinessはPod probeとSessionActivatorのReady待ちで利用する。Busy/WaitingForApproval phaseはRunner APIが投影し、ControllerはReady Podのreconcileで上書きしない。
