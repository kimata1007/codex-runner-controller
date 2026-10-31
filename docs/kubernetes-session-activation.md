# Kubernetesセッション起動アダプター

`runner-api`は会話メッセージを受け付けると、`CodexSession`を会話IDから安定して特定し、存在しなければ作成します。既存リソースが`Suspended`なら`Active`へ戻します。

```text
POST message
  -> SessionActivator
      -> CodexSession(name = sanitized conversation ID)
  -> controller reconcile
      -> PVC / Service / Pod
  -> EndpointResolver
      -> http://<session>.<namespace>.svc.cluster.local:8081
```

## 実行時設定

- `KUBERNETES_NAMESPACE`が設定されると、Runner APIはKubernetesのセッション起動とService DNS解決を有効にします。
- `RUNNER_AGENT_URL`が設定されている場合は固定エンドポイントを優先します。ローカル開発や単一runner-agent互換用です。
- `CODEX_APPROVAL_POLICY`が未設定の場合、作成するセッションの既定値は`user`です。
- PVCサイズの既定値は`10Gi`、`retain`は`true`です。

Kubernetesクライアントの作成に失敗した場合は、Runner API全体を終了させず、セッション起動を無効化した状態で起動します。クラスタ上の本番マニフェストではServiceAccount/RBACと`KUBERNETES_NAMESPACE`を必ず設定します。

## 命名

会話IDを小文字化し、DNSラベルに使えない文字を`-`へ変換して63文字以内に切り詰めます。同じ関数を`CodexSession`作成とService endpoint解決で共有するため、APIとrunner-agentが別のPodを参照することはありません。
