# Codex Runnerのトレース表示項目

Runner API、Runner Agentなどの受信HTTPトレースには、Echoが解決した`http.route`を付け、Span名を`POST /v1/conversations/:id/messages`のようなメソッドとルートテンプレートにします。

サービス間の送信HTTPトレースでは、既知の経路だけを`http.route`に出します。例：

- `POST /v1/threads/:id/turns` — Agentへのターン開始依頼
- `POST /v1/threads/:id/resume` — Agentへのスレッド再開依頼
- `POST /internal/runs/:runID/events` — AgentからRunner APIへのイベント転送
- `POST /internal/sessions/:conversationID/heartbeat` — セッションハートビート
- `GET /v1/codex/settings` — 設定確認

見慣れない外部URLや動的URLをそのまま保存しないため、未登録のパスにはルート属性を付けません。生URL、クエリパラメータ、HTTPヘッダー、リクエスト/レスポンス本文、JSON-RPCパラメータはトレースに入れません。

## エラー

- HTTP 4xx/5xx: `http.response.status_code`、`error.type=http_403`や`http_500`などと`http.server.error`/`http.client.error`イベント。サーバー側の4xxはSpanをエラー扱いにせず、クライアント側の4xxはエラー扱いにします
- 処理中のエラー: `error.type`の安全な分類と、定型文の`error.summary`を含む`error`イベント
- JSON-RPC: 元の`codex.jsonrpc.<method>`Span名は維持し、エラー時はJSON-RPCエラーコードなどを記録

エラーメッセージそのものを記録しないのは意図的です。メッセージにはトークン、プロンプト、ファイル内容、URLなどが含まれ得ます。代わりに、エラーコード・安全な分類・定型説明を使用します。

HTTPアクセスログは従来どおりルートテンプレートを記録し、有効なSpanがあれば`trace_id`と`span_id`も記録します。これらからTempoのトレースとPodログを突き合わせられます。

## 対象外と注意

- CodexのstdioのJSON-RPC本文のロギングは追加しません
- Tempoに送信済みの過去のSpanには新しい属性は付きません
- Kubernetes PodのstderrログとTempoへのイベント出力は別経路です。stderrの生ログをトレースにコピーしません
- 外部APIの未知のパスは機密情報流出防止のため表示しません
