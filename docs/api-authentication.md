# API認証

利用者向けのRunner APIはTailnet内からのみ公開します。runner-agentが呼び出す内部APIはBearerトークンでも保護します。

## 設定

- `RUNNER_API_TOKEN`: runner-agentがRunner APIの内部イベントAPIへ送るトークン
- `RUNNER_AGENT_TOKEN`: Runner APIがrunner-agentへ送り、runner-agentがheartbeatでRunner APIへ送るトークン

`RUNNER_API_TOKEN`はrunner-agent→Runner APIの`/internal/runs/`に、`RUNNER_AGENT_TOKEN`はrunner-agent→Runner APIの`/internal/sessions/`とRunner API→runner-agentに使います。PWAが使う`/v1/` APIではどちらも要求しません。空の場合は該当する内部APIの認証を開発用に無効化しますが、本番マニフェストでは両方必須です。

`/v1/` とhealth probeの `/healthz`、`/readyz` はBearer tokenを要求しません。`/internal/` は `Authorization: Bearer <token>` が必要です。トークンは定数時間比較し、レスポンスにトークン値を含めません。

利用者向けAPIはTailnet内だけから到達できるようにしてください。現時点ではRunner APIに利用者の個人識別情報を渡していないため、アプリ内で利用者ごとの認可や監査は行いません。`/internal/` APIはBearer tokenで保護します。
