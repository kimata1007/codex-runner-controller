# runner-agent HTTP API

`runner-agent` はPod内でCodex App Serverを管理し、Controller/APIが直接stdioやJSON-RPCを扱わなくて済むようにHTTP境界を提供する。

## エンドポイント

| Method | Path | 目的 |
| --- | --- | --- |
| `GET` | `/healthz` | Podのliveness確認 |
| `POST` | `/v1/threads` | 新しいApp Server threadを作成 |
| `POST` | `/v1/threads/{id}/resume` | 保存済みthreadを再開 |
| `POST` | `/v1/threads/{id}/turns` | ユーザーのテキスト入力を送信 |

Runner APIはまずDBへユーザーメッセージを保存し、対象のCodexSession PodがReadyになった後にこのHTTP APIを呼ぶ。`turns` は実行開始を受け付けた時点で `202 Accepted` を返し、App Serverからのイベントは後続PRでDBとクライアント配信へ接続する。

runner-agentのHTTPポートは `RUNNER_AGENT_ADDR` で変更でき、未指定時は `:8081` でlistenする。Service経由でPodへ到達させるため、外部へ公開せずクラスタ内部だけに配置する。

