# Model Proxy の運用

`config/model-proxy` を適用する前に、Identity Issuer と Node Proxy を配置する。`codex-security` の Secret `codex-openai-model` に `api-key` を用意する。Model Proxy は推論のたびにマウント済みのキーを読み込む。

`SessionPolicy` でモデル名、入力サイズ、出力トークン数、1分あたりのリクエスト数と会話全体の予算を設定する。価格は標準処理の100万トークンあたりの米ドルをマイクロドル単位で設定する。たとえば 1 ドルは `1000000`。送信前に利用上限を予約し、使用量を確認できたときに差額を戻す。会話全体の上限はセッション UID に紐づく。

Session 側の Responses API は `http://198.18.0.13:8080/v1`。Codex App Server では Responses を使用するカスタムモデルプロバイダーを設定し、WebSocket と OpenAI 直接認証を無効にする。API キーは Model Proxy の Secret に置く。検索や公開ページの取得には Web Proxy を使用する。
