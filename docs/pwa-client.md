# PWAクライアント

`web/`は音声入力を含まない最小のテキストクライアントです。ブラウザとスマホのホーム画面追加に対応できるよう、manifestとservice workerを含めています。

実装済みの操作:

- chat会話の作成
- Bearer tokenのsessionStorage保存
- メッセージ送信と`clientMessageId`による再送識別
- runイベントのfetchストリーミング表示
- 同一originでのオフラインShellキャッシュ

イベントストリームは標準`EventSource`ではなく`fetch`のReadableStreamを使います。EventSourceはAuthorizationヘッダーを付けられないため、Runner APIのBearer認証と両立するにはこの方式が必要です。

PWAを別originで配信する場合は、Runner APIにCORSを追加し、許可originをTailscale内のPWAホストに限定してください。初期構成では同一originのリバースプロキシ配信を推奨します。
