# PWAの承認UI

PWAはSSEイベントの`event: approval/requested`を検出し、承認要求の内容と許可・拒否ボタンを会話画面へ表示します。

ボタンは次のRunner APIへ結果を送ります。

```text
POST /v1/approvals/{runID}/{requestID}
{"response":{"decision":"accept"}}
```

SSEはAuthorizationヘッダー付きの`fetch`ストリームなので、標準EventSourceではなく既存のReadableStream処理を使います。
