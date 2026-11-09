# セッション単位のRunner Agent接続

## 目的

会話ごとに作成されるセッションPodへRunner APIから接続するため、実行開始時に会話IDからrunner-agentのエンドポイントを解決します。

`runs.MessageDispatcher`はKubernetesやHTTP URLを直接知りません。`ThreadClientFactory`から会話用のクライアントを取得し、スレッド開始・再開・turn開始をそのクライアントへ委譲します。

## 現在の実装

`RUNNER_AGENT_URL`が設定されていれば`StaticEndpointResolver`を使う開発用の単一Agent経路になります。空の場合は、Runner APIが`CodexSession`のReadyを確認してから`ServiceEndpointResolver`で会話IDと同名のServiceを選びます。Kubernetes ServiceのDNS名は`<conversation-id>.<namespace>.svc:8081`です。

```text
message API
  -> runs.MessageDispatcher(conversationID)
  -> ThreadClientFactory
  -> EndpointResolver(conversationID)
  -> runner-agent Service
```

Runner APIはApp Serverを起動しません。App Serverのプロセスとスレッド状態はセッションPod内に残し、Runner APIはHTTP境界だけを利用します。

## なぜFactoryを先に置くか

Podの作成・起動待ちとService DNS解決は別々の責務です。`SessionActivator`がCodexSessionのReadyを待ち、`EndpointResolver`が会話IDから安定したService URLを返します。FactoryはURLをrunner-agent HTTP adapterへ接続し、dispatcherからHTTP詳細を隠します。
