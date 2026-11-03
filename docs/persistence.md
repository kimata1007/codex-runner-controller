# 永続化境界

Runner APIは、HTTPハンドラーからデータベース製品を直接参照しない。`conversation.Repository`を境界として、会話とメッセージの保存処理を分離する。

```text
HTTP handler -> conversation.Repository -> memory.Repository (LIBSQL_URL未設定)
                                          libSQL Repository (LIBSQL_URL設定時)
```

この境界の目的は、次の2つである。

1. APIの振る舞いを、データベース接続の有無とは独立してテストできる。
2. 開発用のメモリ実装からlibSQL実装へ置き換えても、HTTP契約を変更せずに済む。

メモリ実装はローカル開発用に残す。プロセス再起動で状態が消えるため、永続運用では`LIBSQL_URL`を設定する。libSQL実装では、`CreateMessage`の冪等性を`client_message_id`の一意制約でも保証する。
