# Runner APIとlibSQLのマニフェスト

このPRで、Controllerが作成するセッションPodの外側にある2つの常駐サービスを配置します。

- `codex-runner-api`: ClusterIP Service、CodexSession操作用Role、libSQL接続設定
- `libsql`: 1 replicaのStatefulSet、PVC、headless Service

libSQLのデータディレクトリは`/var/lib/sqld`へPVCをマウントします。公式のsqldコンテナイメージを固定バージョンで使い、`SQLD_NODE=primary`で単一primaryとして起動します。libSQLはアプリケーションPodからHTTPで利用しますが、libSQL自身はPVCを持つステートフルなサービスです。

## Secret

`codex-runner-tokens` Secretはこのリポジトリに値を含めません。クラスタへ適用する前に、Secret管理基盤から次のキーを作成してください。

- `api-token`: runner-agentからRunner APIへイベントを送るBearer token
- `agent-token`: Runner APIからセッションPodへ送るBearer token
- `CODEX_GIT_CREDENTIALS_SECRET_NAME`で参照するGit token Secretは、private repositoryを使う場合だけ別途作成する
- `libsql-auth-token`: sqldの認証を有効にする場合に使用するtoken

`api-token`はrunner-agentからのイベント送信、`agent-token`はheartbeatとRunner APIからrunner-agentへの通信に使います。PWAの利用者向けAPIはTailnet内からのみ公開し、Bearer tokenを利用者に入力させません。sqldのBearer認証を有効にする場合は、sqld側にも対応する認証鍵を別途設定してください。API ServiceはClusterIPのみで、外部公開のIngressは含めていません。

## 適用順

```sh
kubectl apply -k config/
```

まずCRD、RBAC、Controller、libSQL、Runner APIのリソースがKustomizeから適用されます。Controllerは`CodexSession`を監視するため、起動順に依存せず各コンポーネントが再試行します。PVC StorageClassはクラスタ環境に合わせてoverlayで上書きしてください。
