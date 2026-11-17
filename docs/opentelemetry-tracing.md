# OpenTelemetryによる分散トレース

Runner API、Controller、Runner Agent、Codex App Serverの処理を、OpenTelemetry Collector経由でGrafana Tempoに送るための実装です。

## 計測範囲

| コンポーネント | 計測対象 | トレースのつながり |
| --- | --- | --- |
| Runner API | HTTPサーバー、Runner AgentへのHTTP通信、libSQLへのHTTP通信 | HTTP経由でW3C `traceparent`を伝播 |
| Runner Controller | `CodexSession`のReconcile | Kubernetesの非同期イベントなので独立したトレース |
| Runner Agent | HTTPサーバー、Codex App ServerとのJSON-RPC呼び出し、イベント・HeartbeatのHTTP送信 | Runner APIからの`traceparent`を受け取る |
| Codex App Server | Codex側のネイティブOpenTelemetryトレース | 別プロセスが独立して生成するトレース。Go AgentのSpanと自動的な親子関係はない |

計測は**オプトイン**です。サービスの環境変数`OTEL_EXPORTER_OTLP_ENDPOINT`を設定すると、GoのOpenTelemetry SDKがOTLP/HTTPでSpanを送信します。未設定ならSDKは起動せず従来の動作を維持します。

```yaml
- name: OTEL_EXPORTER_OTLP_ENDPOINT
  value: http://otel-collector.observability.svc.cluster.local:4318
```

送信先はベースURLです。Go SDKが`/v1/traces`へ送信します。Controllerは環境変数をPodの生成設定に引き継ぎ、Agentも同じCollectorへ送信します。

## Codex App Server側のトレース

Codex CLIにはGo SDKとは別のエクスポーター設定があります。Agentがデフォルトの`codex app-server`を起動する場合、OTLPエンドポイントが設定されていれば、Codexコマンドの`-c`で`otel.trace_exporter`を指定して起動します。

- 出力は**トレースだけ**に限定し、CodexのOTLPログエクスポーターは無効にします。
- `otel.log_user_prompt=false`を明示し、プロンプト本文を送らないようにします。
- ユーザーが`CODEX_APP_SERVER_ARGS`で独自の起動引数を指定した場合、デフォルト引数は使われないため、必要であれば自分でCodexのトレース設定を追加してください。
- Codex側で生成されたSpanは、Go AgentのJSON-RPC呼び出しから独立したトレースです。標準JSON-RPC/stdioに`traceparent`を渡す仕組みは実装していません。独立トレースの原因調査から始めます。

## セキュリティと制限

- HTTPリクエスト本文・レスポンス本文・URLパス・AuthorizationヘッダーはSpan属性に含めません。
- JSON-RPCの引数・結果・エラー本文は保存しません。Codexのプロンプトや認証トークンも送信しません。
- HTTPの状態コードと処理時間は記録します。内部HTTP呼び出しではトレースコンテキストを引き継ぎます。
- ヘルスチェック`/healthz`と`/readyz`は除外します。
- Collectorが使えないときにも、アプリケーションのリクエストはブロックせず、BatchSpanProcessorによって非同期にエクスポートします。
- Controllerは非同期のReconcileであり、APIリクエストから自動的に親子トレースを継承しません。
- コントローラーとAgentの構成に新しいKubernetes Secretや外部公開ポートは必要ありません。

## 運用手順

GitOps管理側の`my-server`にはRunner APIとControllerの環境変数を追加し、本リポジトリのコードと整合するイメージをリリース・デプロイしてください。既存のイメージタグのままでは実装が反映されません。

Grafana ExploreのTempoで`service.name`を選び、`runner-api`、`runner-controller`、`runner-agent`、Codexのトレースをそれぞれ確認してください。同期HTTP処理は同じTrace IDで追い、KubernetesのReconcileとCodex内部処理は独立したトレースとして確認します。

手元での確認:

```sh
gofmt -l cmd internal
go test ./...
```

依存ライブラリはリポジトリのGo 1.24に対応するOpenTelemetry Go `v1.41.0`に固定します。新しい版がGo 1.25以降を必要とする場合は、Goのバージョン更新と併せて検討します。
