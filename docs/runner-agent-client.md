# Runner APIからrunner-agentを呼び出す

`runner-agent`はPod内でCodex App Serverを管理します。Runner APIやControllerは、App ServerのstdioやJSON-RPCを直接扱わず、Pod内HTTP APIを呼び出します。

`internal/agent.RemoteClient`は、次の3つの操作を`agent.SessionAPI`に適合させます。

- `POST /v1/threads` — 新しいthreadを作成
- `POST /v1/threads/{threadId}/resume` — PVC上のthreadを再開
- `POST /v1/threads/{threadId}/turns` — ユーザー入力を送信

この境界を置くことで、Runner APIのテストでは実際のCodexプロセスを起動せず、HTTPサーバーを差し替えて実行経路を検証できます。Kubernetes上では、後続のService実装でセッションPodのrunner-agentへ到達させます。
