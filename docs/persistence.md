# 永続化境界

Runner APIは、HTTPハンドラーからデータベース製品を直接参照しない。`conversation.Repository`を境界として、会話とメッセージの保存処理を分離する。

```text
HTTP handler -> conversation.Repository -> memory.Repository (current)
                                          libSQL Repository (next PR)
```

この境界の目的は、次の2つである。

1. APIの振る舞いを、データベース接続の有無とは独立してテストできる。
2. 開発用のメモリ実装からlibSQL実装へ置き換えても、HTTP契約を変更せずに済む。

このPRではメモリ実装を残す。プロセス再起動で状態が消える制約は解消していないため、永続運用の前提にはしない。libSQL実装では、`CreateMessage`の冪等性を`client_message_id`の一意制約でも保証する。
