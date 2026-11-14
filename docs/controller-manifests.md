# Controller用Kubernetesマニフェスト

`config/`にはCodex Runner Controllerをクラスタへ配置する最小構成を置きます。

```sh
kubectl apply -k config/
```

構成は次の責務に分かれています。

- `namespace.yaml`: Tailscale内で利用するための専用namespace
- `crd/`: `CodexSession`のAPI定義とstatus subresource
- `rbac/`: ControllerがCodexSession、Pod、Service、PVCをreconcileするためのRole
- `manager/`: controller-runtime managerのDeployment

このPRはControllerだけを対象にします。Runner API、runner-agent、libSQLは別Deploymentとして追加します。サンプルのControllerイメージは`ghcr.io/kimata1007/codex-runner-controller:v0.1.0`です。イメージをpublishした固定バージョンまたはdigestに合わせてください。

## RBACの考え方

Controllerはnamespace内のリソースだけを管理するため、ClusterRoleではなくRoleを使用します。CodexSessionのstatus subresourceは、spec変更権限と分けて明示しています。OwnerReferenceで管理するPod/Service/PVCを作成・削除する権限が必要です。
