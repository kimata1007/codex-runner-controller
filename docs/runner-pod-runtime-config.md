# runner Podの実行時設定

Controllerは`ResourceBuilder`で、セッションPodに次の設定を注入します。

- `RUNNER_EVENT_URL`: runner-agentがApp Serverイベントを送るRunner APIの内部URL
- `RUNNER_HEARTBEAT_URL`: runner-agentがセッションheartbeatを送るRunner APIの内部URL。conversation IDはrunner-agentがパスへ追加する
- `RUNNER_AGENT_TOKEN`: Secretの指定キーから取得するRunner API→agent認証トークン
- `RUNNER_API_TOKEN`: Secretから取得するagent→Runner APIイベント送信用トークン
- `CODEX_CONVERSATION_ID、`CODEX_MODE`、`CODEX_APPROVAL_POLICY`: `CodexSession.spec`からの実行設定
- 専用ServiceAccountとnon-root/seccomp設定
- session PVCと`/tmp`用の一時`emptyDir`

Controller Deploymentの環境変数で、イベントとheartbeatの送信先・agentイメージ・各Secret名とキーを指定します。トークン値自体はPod specの平文に埋め込まず、`SecretKeyRef`だけをPodへ渡します。

## CodexのLinux sandboxを使う場合

Codex App ServerがLinux sandbox helperとして`bwrap`を起動します。Podの`RuntimeDefault` seccomp profileがuser namespace作成を拒否する環境では、コマンド実行が`Operation not permitted`で失敗します。Runner Agentの権限を増やさずに対応するには、user namespace作成を許可するseccomp profileを各Nodeのkubelet seccompディレクトリへ配置し、Controller Deploymentに次を設定します。

```yaml
- name: RUNNER_AGENT_SECCOMP_PROFILE
  value: profiles/codex-runner.json
```

値は`/var/lib/kubelet/seccomp`からの相対パスです。設定するとセッションPodのseccomp profileを`Localhost`へ切り替えます。未設定の場合は従来どおり`RuntimeDefault`を使います。Localhost profileはセッションPod作成時に対象Node上に存在している必要があるため、すべてのスケジュール対象Nodeへ同じprofileを配布してください。

Profileは利用するランタイムの`RuntimeDefault`を基に、Codex sandboxが必要とするuser namespace操作だけを追加許可してください。`Unconfined`、特権コンテナ、`CAP_SYS_ADMIN`の追加で回避しないでください。Kernel、container runtime、AppArmorの設定によってはseccomp profileだけで解決しない場合があります。変更後は、実際のRunner Podでsandbox内のコマンド実行を確認してください。

ChatGPTプランのOAuth tokenを使う場合は、Controller Deploymentへ`CODEX_ACCESS_TOKEN_SECRET_NAME`と`CODEX_ACCESS_TOKEN_SECRET_KEY`も設定します。runner-agentはそのSecret参照を`ACCESS_TOKEN`としてApp Serverへ継承します。
