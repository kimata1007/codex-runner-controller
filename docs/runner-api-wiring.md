# runner-apiの実行経路

`runner-api`は環境変数でrun開始経路を有効にします。

## 保存だけのローカルモード

`RUNNER_AGENT_URL`が空の場合、Runner APIはメッセージをrepositoryへ保存します。libSQL未設定時はメモリrepositoryを使います。

## libSQLとrunner-agentを使うモード

`LIBSQL_URL`と`RUNNER_AGENT_URL`を設定すると、次の依存が接続されます。

1. libSQL repositoryがconversation、message、runを保存する。
2. `agent.RemoteClient`がrunner-agentのHTTP APIを呼ぶ。
3. `agent.RunClient`がHTTPクライアントを`runs.ThreadClient`へ適合させる。
4. `runs.MessageDispatcher`がthread作成/再開、run作成、turn送信を行う。
5. Runner APIがメッセージとrunを返す。

`RUNNER_AGENT_URL`だけを設定した場合は、runを保存できるrepositoryがないため起動エラーにします。現段階ではrunner-agent URLは固定値です。会話ごとに異なるセッションPodのURLを解決する処理は、後続のsession routingで追加します。
