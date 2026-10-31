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

`RUNNER_SESSION_NAMESPACE`が空の場合は`codex-system`を使用します。その他のPVCサイズ、StorageClass、モデル、承認ポリシーの設定は[runner-apiの実行経路](runner-api-wiring.md)を参照してください。

`CodexSession`のアイドルタイムアウトが未指定（`0`）の場合は、API型で定義した`DefaultIdleTimeoutSeconds`（1800秒、30分）を使います。この値をセッション作成時に`spec.idleTimeoutSeconds`へ設定するため、コントローラーは明示的なタイムアウトを使って停止時刻を判定できます。
