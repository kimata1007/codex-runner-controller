# Codex App Server認証

## PWAからサインイン

`ACCESS_TOKEN`を設定していない場合、最初のメッセージ送信前にRunner APIが会話用PodのCodex App Serverへ`account/read`を送り、認証状態を確認します。サインインが必要なら`account/login/start`のdevice-codeフローを開始し、PWAに認証ページと一時コードを表示します。認証完了を確認してから、保留中のメッセージを送ります。ブラウザーにはOAuth tokenを渡さず、App Serverが会話Pod内の`CODEX_HOME`へ保存します。

Codex App Serverと作業ディレクトリは会話ごとのPod/PVCに分かれています。そのため、Secretを使わない構成では新しい会話ごとにサインインを求められる場合があります。インストール全体で一度だけサインインする共有アカウント管理は、会話間で安全に認証情報を共有する仕組みが別途必要です。

## 外部tokenを使う場合

既存のSign in with ChatGPTフローなどで取得したOAuth access tokenをKubernetes Secretへ保存できます。Controller Deploymentに次の環境変数を設定すると、各セッションPodへSecretKeyRefで`ACCESS_TOKEN`として注入します。

```text
CODEX_ACCESS_TOKEN_SECRET_NAME=codex-auth
CODEX_ACCESS_TOKEN_SECRET_KEY=accessToken
```

Secretの値はPod spec、CRD、libSQL、ログへ平文コピーしません。`CODEX_APP_SERVER_ARGS`が未設定で`ACCESS_TOKEN`が存在する場合、runner-agentは公式provider設定（`openai_chatgpt_plan`、Responses API、`env_key=ACCESS_TOKEN`）を付けてApp Serverを起動します。明示的な`CODEX_APP_SERVER_ARGS`を設定した場合は、その引数を優先します。

OAuth tokenの更新時は、Secretをローテーションした後に対象セッションPodを再起動し、保存済みthread IDで`thread/resume`します。Token更新処理そのものはSecret管理基盤または運用側の責務です。
