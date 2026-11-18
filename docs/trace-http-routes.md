# Codex RunnerのHTTPトレース

Runner API、Runner AgentなどのHTTPトレースでは、受信側はEchoのルートテンプレート（例：`POST /v1/conversations/:id/messages`）をSpan名と`http.route`に出します。また、識別子を追跡できるよう**実際のIDを含むパス**を`url.path`に記録します。送信側も既知の内部APIに限り同じ情報を記録します。

- `POST /v1/threads/:id/turns` — ターン開始
- `POST /v1/threads/:id/resume` — スレッド再開
- `POST /internal/runs/:runID/events` — イベント転送
- `POST /internal/sessions/:conversationID/heartbeat` — ハートビート
- `GET /v1/codex/settings` — 設定確認

`url.path`はクエリ文字列・フラグメントを含みません。パス中のGitHub/OpenAIトークンやJWTに一致する要素は伏せます。未知の送信先は、生のパスを記録しません。IDそのものは記録されるため、トレース閲覧権限と保存期間には注意してください。

## エラー

**HTTPステータスと元のエラーメッセージは別の情報**として扱います。

- `http.response.status_code`: ステータス（例：502）
- `exception.message`: エラー発生箇所が保持している**実際のエラー文面をサニタイズ**したもの（例：`start thread: jsonrpc -32601: method not handled`）
- `exception.type` / `error.type`: タイムアウト、認証失敗、JSON-RPCコードなどの分類
- `trace_id`と`span_id`: HTTPアクセスログとトレースの突き合わせ

ステータス値だけを重複させるためのイベントは作りません。エラーを処理したRunner API/AgentのSpanで、実際の原因メッセージを記録します。HTTPの受信/送信トレースにはメソッド、ルート、ID、ステータスが出ますが、HTTPレスポンス本文は**自動取得・記録しません**。

## 機密情報の扱い

エラーメッセージにはトークンなどが混ざる可能性があるため、一般的な認証ヘッダー、Bearer/Basic値、GitHub/OpenAIトークン、JWT、URLのクエリ、`prompt`/`input`/`content`等の構造化されたフィールドを置換してから送ります。長さは1024文字までに制限します。ユーザープロンプトやHTTP/JSON-RPCのリクエスト・レスポンス本文を意図的に送信することはありません。

**制限**: 自由形式のエラーメッセージに含まれる秘密情報を、正規表現ですべて確実に見つけることはできません。フィルタはベストエフォートです。トレースにアクセスできる人・保存期間を制限し、将来的には認証やモデル入力を扱うコードが安全な型付きエラーを返す設計を推奨します。

Codex App ServerのstderrログをTempoへそのまま流すことはありません。
