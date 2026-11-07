# run IDの伝播

Runner APIはCodexのターン開始をrunner-agentに依頼する前に、runを作成して保存します。App Serverからの通知はターン開始HTTP要求への応答とは別に、runner-agentのJSON-RPC標準出力から届きます。そのため、通知をrunに紐付けるには、ターン開始時にrun IDをrunner-agentへ渡す必要があります。

通常のメッセージ送信経路では、各層が次のようにrun IDを引き継ぎます。

```text
MessageDispatcherがRunを作成（run.ID）
  -> ThreadClient.StartTurn(threadID, run.ID, text)
  -> agentclient.RunAdapter
  -> agentclient.ClientがPOST /v1/threads/{threadID}/turnsを送信
     {"text":"...","runId":"run-..."}
  -> runner-agentのHTTPハンドラーがrunIdをRunTrackerに保存
```

これにより、App Serverイベントのコールバックは非同期通知を処理するときに、現在のrun IDを参照できます。App Serverと直接やり取りする低レベルの`SessionAPI.StartTurn`は、Runner APIのrun情報を扱わないため変更していません。
