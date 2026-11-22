# Controller用Kubernetesマニフェスト

`config/`にはCodex Runnerの各コンポーネントをクラスタへ配置するマニフェストを置きます。

```sh
kubectl apply -k config/
```

構成は次の責務に分かれています。

- `namespace.yaml`: 専用namespace
- `crd/`: `CodexSession`のAPI定義とstatus subresource
- `rbac/`: ControllerがCodexSession、Pod、Service、PVCをreconcileするためのRole
- `manager/`: controller-runtime managerのDeployment
- `api/`, `auth-broker/`, `libsql/`, `web/`: API、認証ブローカー、DB、PWA

イメージタグはリリース時に更新されます。環境overlayを作る場合は、公開済みの固定タグまたはdigestを指定してください。

## RBACの考え方

Controllerはnamespace内のリソースだけを管理するため、ClusterRoleではなくRoleを使用します。CodexSessionのstatus subresourceは、spec変更権限と分けて明示しています。OwnerReferenceで管理するPod/Service/PVCを作成・削除する権限が必要です。
