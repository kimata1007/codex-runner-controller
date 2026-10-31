# runner Podの実行時設定

Controllerは`ResourceBuilder`で、セッションPodに次の設定を注入します。

- `RUNNER_EVENT_URL`: runner-agentがApp Serverイベントを送るRunner APIの内部URL
- `RUNNER_AGENT_TOKEN`: Secretの指定キーから取得するRunner API→agent認証トークン
- `CODEX_CONVERSATION_ID`、`CODEX_MODE`、`CODEX_APPROVAL_POLICY`: `CodexSession.spec`からの実行設定
- 専用ServiceAccountとnon-root/seccomp設定
- session PVCと`/tmp`用の一時`emptyDir`

Controller Deploymentの環境変数で、イベント送信先・agentイメージ・Secret名を指定します。トークン値自体はPod specの平文に埋め込まず、`SecretKeyRef`だけをPodへ渡します。
