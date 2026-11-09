# セッション単位のRunner Agent接続

## 目的

会話ごとに作成されるセッションPodへRunner APIから接続するため、実行開始時に会話IDからrunner-agentのエンドポイントを解決します。

`runs.MessageDispatcher`はKubernetesやHTTP URLを直接知りません。`ThreadClientFactory`から会話用のクライアントを取得し、スレッド開始・再開・turn開始をそのクライアントへ委譲します。

## 現在の実装

現段階では`RUNNER_AGENT_URL`を`StaticEndpointResolver`へ渡します。これは開発用および単一セッションの互換経路です。次の段階で、ResolverをKubernetes API経由の`CodexSession`/Service解決へ差し替えます。

```text
message API
  -> runs.MessageDispatcher(conversationID)
  -> ThreadClientFactory
  -> EndpointResolver(conversationID)
  -> runner-agent Service
```

Runner APIはApp Serverを起動しません。App Serverのプロセスとスレッド状態はセッションPod内に残し、Runner APIはHTTP境界だけを利用します。

## なぜFactoryを先に置くか

Podの作成・起動待ち・Service DNS解決をHTTPハンドラへ埋め込むと、会話保存やrun状態管理とKubernetes依存が混ざります。Factory/Resolver境界を先に固定すると、後続PRで起動処理を追加してもrunライフサイクルのテストを保てます。
