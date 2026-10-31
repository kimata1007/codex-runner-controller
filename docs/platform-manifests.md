# Runner APIとlibSQLのマニフェスト

このPRで、Controllerが作成するセッションPodの外側にある2つの常駐サービスを配置します。

- `codex-runner-api`: ClusterIP Service、CodexSession操作用Role、libSQL接続設定
- `libsql`: 1 replicaのStatefulSet、PVC、headless Service

libSQLのデータディレクトリは`/var/lib/sqld`へPVCをマウントします。公式のsqldコンテナイメージを使い、`SQLD_NODE=primary`で単一primaryとして起動します。libSQLはアプリケーションPodからHTTPで利用しますが、libSQL自身はPVCを持つステートフルなサービスです。

## Secret

`codex-runner-secrets`はこのリポジトリに値を含めません。クラスタへ適用する前に、少なくとも次のキーをSecret管理基盤から作成してください。

- `runnerApiToken`: PWAからRunner APIへ送るBearer token
- `runnerAgentToken`: Runner APIからセッションPodへ送るBearer token
- `libsqlAuthToken`: sqldの認証を有効にする場合に使用するtoken

Secretがない場合は開発互換のため環境変数が空になり、認証ミドルウェアは無効化されます。本番でSecret未設定のまま公開経路を作らないでください。API ServiceはClusterIPのみで、外部公開のIngressは含めていません。

## 適用順

```sh
kubectl apply -k config/
```

まずCRD、RBAC、Controller、libSQL、Runner APIの順でKustomizeが適用します。イメージタグとPVC StorageClassはクラスタ環境に合わせてoverlayで上書きしてください。
