# runner Podの実行時設定

Controllerは`ResourceBuilder`で、セッションPodに次の設定を注入します。

- `RUNNER_EVENT_URL`: runner-agentがApp Serverイベントを送るRunner APIの内部URL
- `RUNNER_HEARTBEAT_URL`: runner-agentがセッションheartbeatを送るRunner APIの内部URL。conversation IDはrunner-agentがパスへ追加する
- `RUNNER_AGENT_TOKEN`: Secretの指定キーから取得するRunner API→agent認証トークン
- `RUNNER_API_TOKEN`: Secretから取得するagent→Runner APIイベント送信用トークン
- `CODEX_CONVERSATION_ID`、`CODEX_MODE`、`CODEX_APPROVAL_POLICY`: `CodexSession.spec`からの実行設定
- 専用ServiceAccountとnon-root/seccomp設定
- session PVCと`/tmp`用の一時`emptyDir`

Controller Deploymentの環境変数で、イベントとheartbeatの送信先・agentイメージ・各Secret名とキーを指定します。トークン値自体はPod specの平文に埋め込まず、`SecretKeyRef`だけをPodへ渡します。

Codexは会話PodのKubernetesセキュリティ境界内で実行し、内部Linux sandbox（Bubblewrap）は使用しません。runner-agentはCodex起動時に`sandbox_mode="danger-full-access"`を明示し、`thread/start`・`thread/resume`・`turn/start`でも同等の設定を固定します。これは**Codex内部の制限だけを解除**するもので、Podの`RuntimeDefault` seccomp、non-root、権限昇格禁止、capabilitiesの削除は維持します。CodexがPod内のファイル・資格情報・ネットワークへアクセスできる点に注意してください。Nodeへのカスタムseccomp profile配布は不要です。承認ポリシーは従来どおり独立して適用します。

ChatGPTプランのOAuth tokenを使う場合は、Controller Deploymentへ`CODEX_ACCESS_TOKEN_SECRET_NAME`と`CODEX_ACCESS_TOKEN_SECRET_KEY`も設定します。runner-agentはそのSecret参照を`ACCESS_TOKEN`としてApp Serverへ継承します。
