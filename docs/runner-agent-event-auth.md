# runner-agentからRunner APIへのイベント送信認証

runner-agentがApp ServerイベントをRunner APIへ送るときは、Runner APIのBearer認証に使う `RUNNER_API_TOKEN` を付けます。

ControllerはセッションPodへ `RUNNER_API_TOKEN` をSecretKeyRefで注入し、runner-agentは `RUNNER_EVENT_URL` へのPOSTごとに `Authorization: Bearer <token>` を設定します。トークン値はイベントURL、JSON payload、ログには含めません。

`RUNNER_API_TOKEN` が空の場合、runner-agentはAuthorizationヘッダーを付けません。認証を有効にする場合は、Runner APIとセッションPodに同じSecret由来の値を設定してください。 `RUNNER_AGENT_TOKEN` は逆方向のRunner APIからrunner-agentへのHTTPリクエストで使います。
