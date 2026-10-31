# runner-agentとCodex App Server

runner Podでは、`runner-agent`がCodex App Serverを子プロセスとして起動する。ControllerはCodexのJSON-RPCを直接処理せず、Podの作成・削除だけを担当する。

App Serverとの通信はnewline-delimited JSON over stdioである。`internal/agent.JSONRPCClient`は、リクエストIDに対応する応答を待ちつつ、応答より先に届くイベントを`EventHandler`へ渡す。

初期化の順序は次のとおり。

1. App Serverを`codex app-server --listen stdio://`で起動
2. `initialize`を`clientInfo`付きで送信
3. `initialized`通知を送信
4. 後続PRで`thread/start`または`thread/resume`を送信
5. 後続PRで`turn/start`とイベント保存・API転送を追加

公式ドキュメントでもこのstdio、newline-delimited JSON、initialize/initialized、thread/start、turn/startの流れが説明されている。App Serverは継続会話、イベント、承認要求を扱うため、このPod内常駐プロセス構成に適している。
