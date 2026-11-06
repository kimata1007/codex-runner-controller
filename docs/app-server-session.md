# App Serverのセッションライフサイクル

`runner-agent` はPodごとに1つの `agent.Session` を作る。App ServerのプロセスはPod内で生きている間だけメモリ上の接続を持つが、スレッドのIDはAPI/DB側で保持する。

## 新規セッション

1. `runner-agent` がApp Serverをstdioで起動する
2. `initialize` を送り、`initialized` 通知を送る
3. `thread/start` を送り、返された `thread.id` をAPI/DBへ保存する
4. `turn/start` でユーザー入力を送る

## Pod削除後の再開

1. idle timeoutでPodだけが削除される
2. `CodexSession`、DBのメッセージ、PVC上のCODEX_HOME/thread履歴は残る
3. 次のユーザー入力でControllerがPodを再作成する
4. `runner-agent` がApp Serverを初期化する
5. DBから取得した `thread.id` で `thread/resume` を送り、同じ会話へ戻る
6. その後の入力は同じ `thread.id` に対して `turn/start` する

このため、App Serverをクラスタ全体で常駐させる必要はない。ControllerがPodの存在を管理し、API/DBが会話の外部状態を保持することで、アイドル時のPod削除と会話の継続を両立する。

