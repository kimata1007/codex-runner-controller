# Reconcileロジック

`internal/controller.CodexSessionReconciler`は、KubernetesのAPIクライアントを直接呼び出さず、リソースの取得・作成・削除を抽象化した`ResourceClient`だけに依存する。

状態の収束は次のとおり。

1. `Active`でPVCがなければ、同じセッション名のPVCを作成する。
2. `Active`でPodがなければ、runner Podを作成し、`Starting`を返す。
3. PodにReady条件があれば`Ready`を返す。未Readyなら次のwatch/reconcileを待つ。
4. `Suspended`ならPodだけを削除し、PVCは残して`Suspended`を返す。

この分離により、Kubernetes APIのwatchや再試行と、ビジネスルールである「Podは捨ててもセッションデータは残す」を別々にテストできる。次のPRでは`ResourceClient`をcontroller-runtimeのクライアントへ接続する。
