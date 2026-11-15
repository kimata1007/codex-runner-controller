# メッセージをCodexへ送る

`MessageDispatcher.Dispatch`は、DBに保存済みのユーザーメッセージ1件をCodex App Serverの1 turnとして送信し、そのturnのRun状態を記録します。これはHTTPサーバーでもPod起動処理でもありません。既に動作しているrunner-agentを使います。

処理は3つの役割に分かれています。

1. **threadを準備する** — thread IDがなければrunner-agentにthread作成を依頼し、あれば既存threadの再開を依頼する。
2. **Runを記録する** — StoreにRunを`queued`で作成し、Codexへ送る前に`running`へ更新する。
3. **turnを送る** — runner-agentへユーザーのテキストを送り、送信に失敗したらRunを`failed`にする。

`Dispatch`はrunner-agentが`turn/start`を受け付けた時点で戻ります。Codexの回答や進捗イベントの保存・配信は、この処理の後に別経路で行います。

Runner APIはメッセージを永続化してからdispatcherを呼び出します。Kubernetes構成では、先にCodexSessionをReadyにしてからdispatcherへ渡します。
