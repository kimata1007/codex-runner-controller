# runner-agentのprobe

セッションPodのreadiness/liveness probeは`/readyz`と`/healthz`を使います。runner-agent HTTPサーバーは同じ軽量なhealth応答を両方のパスで提供します。

App Serverの`initialize`が完了してからHTTPサーバーを起動するため、`/readyz`が成功したPodはrunner-agentからApp Serverへ接続可能な状態です。認証ミドルウェアを有効にしても、probe用の2パスは認証不要です。
