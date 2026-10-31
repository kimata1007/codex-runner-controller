# Kubernetesセッションアダプター

セッション起動では、ユースケースの要求とKubernetes操作を別のパッケージで扱います。

```text
runnerapi
  -> session.Activator (インターフェース)
       -> kubernetes.SessionActivator
            -> CodexSessionを作成またはActiveへ更新
            -> controllerのReadyを待つ
       -> kubernetes.SessionEndpointResolver
            -> 会話IDからService DNS名を解決
```

`session.Activation`が渡すのは会話IDだけです。Kubernetesのリソース名、namespace、APIクライアントは`internal/kubernetes`に閉じています。リソース作成はCodexSessionコントローラーが担当し、Runner APIはReadyを確認してからrunを送ります。

会話IDを小文字化し、DNSラベルに使えない文字をハイフンに置き換え、63文字以内のService名を作ります。同じ`SessionName`関数をCodexSession作成とendpoint解決で共有します。
- `KUBERNETES_NAMESPACE`が設定されると、Runner APIはKubernetesのセッション起動とService DNS解決を有効にします。
- `RUNNER_AGENT_URL`が設定されている場合は固定エンドポイントを優先します。ローカル開発や単一runner-agent互換用です。
- `CODEX_APPROVAL_POLICY`が未設定の場合、作成するセッションの既定値は`user`です。
- PVCサイズの既定値は`10Gi`、`retain`は`true`です。
- アイドルタイムアウトの既定値は`1800秒（30分）`です。最後のアクティビティからこの時間が経過すると、ControllerはPodだけを削除し、CR/PVC/Service/DBの会話状態を残します。
- `CODEX_GIT_CREDENTIALS_SECRET_NAME`をRunner API Deploymentに設定すると、新しく作るセッションの`credentialsRef`へそのSecret名を設定します。Secretはセッションと同じNamespaceに置き、`token`キーへHTTPS Git tokenを保存します。未設定なら公開リポジトリまたは別途作成したCRDの`credentialsRef`を使います。

`RUNNER_SESSION_NAMESPACE`が空の場合は`codex-system`を使用します。その他のPVCサイズ、StorageClass、モデル、承認ポリシーの設定は[runner-apiの実行経路](runner-api-wiring.md)を参照してください。

`CodexSession`のアイドルタイムアウトが未指定（`0`）の場合は、API型で定義した`DefaultIdleTimeoutSeconds`（1800秒、30分）を使います。この値をセッション作成時に`spec.idleTimeoutSeconds`へ設定するため、コントローラーは明示的なタイムアウトを使って停止時刻を判定できます。
