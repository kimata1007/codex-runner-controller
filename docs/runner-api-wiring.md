# runner-apiの実行経路

`runner-api`は環境変数でrun開始経路を有効にします。

## 保存だけのローカルモード

libSQLを設定せずに起動するとメモリrepositoryを使い、メッセージを保存します。メモリrepositoryはrunを保存しないため、Kubernetesセッションの起動やAgentへの送信は行いません。

## Kubernetesセッションを使うモード

libSQLを設定し、`RUNNER_AGENT_URL`を空にすると、Runner APIはKubernetes上の会話別セッションを使います。メッセージ送信時の順序は次の通りです。

1. `runnerapi`が会話の存在を確認する。
2. `kubernetes.SessionActivator`が会話IDと同名の`CodexSession`を作成するか、SuspendedならActiveへ戻す。これは`session.Activator`インターフェースを実装する。
3. `CodexSession`コントローラーがPVC、Service、runner-agent Podを準備し、状態をReadyにする。
4. `kubernetes.SessionEndpointResolver`が`http://<session-name>.<namespace>.svc:8081`を選ぶ。
5. `runs.MessageDispatcher`がメッセージを保存し、`agentclient.RunAdapter`経由でthreadを開始または再開してturnを送る。
6. Runner APIがメッセージとrunを返す。

`RUNNER_SESSION_NAMESPACE`はセッションのnamespaceを指定し、省略時は`codex-system`です。`RUNNER_SESSION_DATA_SIZE`と`RUNNER_SESSION_STORAGE_CLASS`でPVC設定を指定できます。Codexのモデルと承認ポリシーは`CODEX_MODEL`と`CODEX_APPROVAL_POLICY`を使用します。承認ポリシーの既定値は`user`です。

Runner APIのServiceAccountには、対象namespace内で`codexsessions`をget/create/updateする権限が必要です。`config/rbac/runner-api-role.yaml`にRoleの例があります。CRDとCodexSessionコントローラーも事前に稼働している必要があります。コントローラー側の`RUNNER_AGENT_IMAGE`設定は従来通り必要です。

## 固定URLの開発モード

`LIBSQL_URL`と`RUNNER_AGENT_URL`を設定すると、Kubernetesセッションを作らず、指定された単一のrunner-agentへ接続します。この場合もrunを保存できるrepositoryが必要です。
