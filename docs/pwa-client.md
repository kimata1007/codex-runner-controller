# PWAクライアント

`web/`は音声入力を含まない最小のテキストクライアントです。ブラウザとスマホのホーム画面追加に対応できるよう、manifestとservice workerを含めています。

実装済みの操作:

- chat会話の作成
- 同一Origin APIから直近100件の会話履歴を取得し、更新日時順に表示
- 入力欄の追加メニューからリポジトリとrevisionを会話の作業先として選択
- メッセージ送信と`clientMessageId`による再送識別
- `threadId`を会話中で引き継ぎ、Codexの同じスレッドへ続けて送信
- runイベントのfetchストリーミング表示
- 同一originでのオフラインShellキャッシュ

PWAはRunner APIと同じoriginから配信し、利用者向け`/v1/` APIにBearer tokenを付けません。認証境界はTailnet側に置きます。イベントストリームは`fetch`のReadableStreamで読み取り、応答中のイベントを画面へ反映します。

PWAを別originで配信する場合は、Runner APIにCORSを追加し、許可originをTailscale内のPWAホストに限定してください。初期構成では同一originのリバースプロキシ配信を推奨します。
