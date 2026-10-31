# libSQL/sqld接続

`internal/libsql` はRunner APIからsqldへHTTPで接続する小さなクライアントである。sqldのHrana over HTTP v2で、`POST /v2/pipeline` にSQL実行を送る。

## baton

HTTPは接続を持たないが、sqldのSQLストリームは順序を持つ必要がある。そのため、サーバーから返された `baton` を次のリクエストへ返す。クライアントはこのbatonをmutexで保護し、1つのストリームに対するSQLを直列化する。

Runner APIのPodが再起動してbatonを失っても、batonはデータそのものではない。次の要求で新しいストリームを開始できる。会話やメッセージの復元に必要な状態はlibSQLのテーブルに保存される。

## 認証

`NewClient(baseURL, token)` の `token` が空でなければ `Authorization: Bearer ...` を送る。KubernetesではtokenをSecretから環境変数へ注入する。

## 設定例

```text
LIBSQL_URL=http://libsql.codex-system.svc.cluster.local:8080
LIBSQL_AUTH_TOKEN=...
```

Runner APIは`LIBSQL_URL`が設定されている場合、起動時にスキーマを適用してlibSQL RepositoryをHTTPサーバーへ注入する。未設定時のメモリRepositoryはローカル開発用であり、再起動でデータが失われる。
