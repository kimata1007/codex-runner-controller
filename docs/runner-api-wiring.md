# runner-apiの実行経路

`runner-api`は環境変数でrun開始経路を有効にします。

## 保存だけのローカルモード

`RUNNER_AGENT_URL`が空の場合、Runner APIはメッセージをrepositoryへ保存します。libSQL未設定時はメモリrepositoryを使います。

## libSQLとrunner-agentを使うモード

`LIBSQL_URL`と`RUNNER_AGENT_URL`を設定すると、次の依存が接続されます。

1. `runnerapi`がメッセージのHTTPリクエストを受け取り、repositoryがconversationとmessageを保存する。
2. `runs.MessageDispatcher`が`agentclient.RunAdapter`を呼び、threadを作成または再開する。
3. `agentclient.Client`がrunner-agentのHTTP APIを呼ぶ。
4. `MessageDispatcher`がrunを作成して保存する。
5. `RunAdapter`が`Client`経由でturnを送る。
6. Runner APIがメッセージとrunを返す。

`RUNNER_AGENT_URL`だけを設定した場合は、runを保存できるrepositoryがないため起動エラーにします。現段階ではrunner-agent URLは固定値です。会話ごとに異なるセッションPodのURLを解決する処理は、後続のsession routingで追加します。
