# Codex App Server の診断ログ

## 現状と使い分け

1つのConversation Podの中で`runner-agent`が`codex app-server --listen stdio://`を子プロセスとして起動します。

- **Podログ**: Runner AgentのHTTPリクエストとイベント転送。Codexの**stdoutはJSON-RPC専用**であり、ログへ転送しません。
- **Codexローカルログ**: `$CODEX_HOME/log`（通常は`/var/lib/codex/log`）に存在する場合があります。Pod内で直接確認できます。
- **Codex stderr**: デフォルトでPodログには転送しません。診断時のみ、以下の設定で有効化できます。

```sh
kubectl -n codex-system exec POD_NAME -c runner-agent -- ls -lah /var/lib/codex/log
kubectl -n codex-system logs POD_NAME -c runner-agent --since=30m
```

## stderrの転送を一時的に有効化する

**重要**: Codexのstderrは、ユーザープロンプト、パス、ツール出力、秘密情報を含む可能性があります。Kubernetesログは後続のログ収集システムに転送・保存され得ます。**通常は無効**にし、信頼できる管理者のみがアクセスできる環境で、必要な間だけ有効化してください。

ControllerのDeploymentに次の環境変数を追加します。

```yaml
- name: CODEX_APP_SERVER_STDERR_TO_POD_LOG
  value: "true"
```

Controllerが**新たに作成する**セッションPodへこの設定を引き継ぎます。既存のセッションPodには反映されません。セッションPodで実行されるrunner-agentがCodexの**stderrだけ**をPodログへ出力します。標準出力のJSON-RPCメッセージ、引数、トークン、プロンプトの全量は記録しません。

この環境変数は**正確に`true`の場合だけ有効**です。診断が終わったらControllerの設定を戻し、影響するセッションPodを再作成してください。ログ収集先の保持期間・アクセス権も確認してください。

## 書き込みと承認の調査

`approvalPolicy=never`は「何でも許可」ではありません。承認を要求しないため、サンドボックスで拒否される操作は別途失敗する場合があります。Codexの`sandbox_mode`（`workspace-write`等）と、PodのPVC所有者・Unixパーミッションは独立しています。

このPRは承認・サンドボックス・Podのファイル権限を変更しません。
