# Codex App Server認証

ChatGPTプランでCodex App Serverを使う場合、公式のSign in with ChatGPTフローで取得したOAuth access tokenをKubernetes Secretへ保存します。Controller Deploymentに次の環境変数を設定すると、セッションPodへSecretKeyRefで`ACCESS_TOKEN`として注入します。

```text
CODEX_ACCESS_TOKEN_SECRET_NAME=codex-auth
CODEX_ACCESS_TOKEN_SECRET_KEY=accessToken
```

Secretの値はPod spec、CRD、libSQL、ログへ平文コピーしません。`CODEX_APP_SERVER_ARGS`が未設定で`ACCESS_TOKEN`が存在する場合、runner-agentは公式provider設定（`openai_chatgpt_plan`、Responses API、`env_key=ACCESS_TOKEN`）を付けてApp Serverを起動します。明示的な`CODEX_APP_SERVER_ARGS`を設定した場合は、その引数を優先します。

OAuth tokenの更新時は、Secretをローテーションした後に対象セッションPodを再起動し、保存済みthread IDで`thread/resume`します。Token更新処理そのものはSecret管理基盤または運用側の責務です。
