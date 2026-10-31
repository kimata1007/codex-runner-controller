# Runとイベントの永続化

会話 (`conversation`) はユーザーとの長期的なスレッドであり、実行 (`run`) はその会話に対する1回の `turn/start` である。App Serverが返す通知・進捗・承認要求は、実行に紐付くイベント (`event`) として保存する。

## なぜrunを分けるか

- 1つの会話で複数回の実装指示を実行できる
- 各実行の状態を `queued` / `running` / `waiting_approval` / `completed` などで追跡できる
- Podが削除されても、どの実行が途中だったかをDBから判断できる
- イベントをsequence順で再生でき、後続のSSEクライアントが途中から再接続できる

`AppendEvent` はrunごとのsequenceをDB側で採番し、`ListEvents` は指定したsequenceより後だけを返す。現段階ではRunner Podからのイベント転送は後続PRで接続する。

