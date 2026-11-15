# Codex RunnerのChatGPT認証

## 認証の共有

ChatGPTへの接続はRunner全体で一度行います。会話用Podごとにサインインする必要はありません。

1. PWAで会話を送信すると、未接続の場合に一時コマンドを表示します。
2. リポジトリのルートで `go run ./cmd/codex-runner auth connect --server <Runner URL> --attempt <one-time ID> --host-id <host ID>` を実行します。
3. ローカルヘルパーが `127.0.0.1` の一時コールバックを開き、ChatGPTの認証・同意を受け付けます。
4. ヘルパーはOAuthトークンをPWAへ渡さず、TLS経由でRunner APIへ直接送ります。APIはID tokenの署名、audience、nonce、有効期限と、ChatGPTプラン利用scopeを検証します。
5. Runner APIはトークン一式をAES-GCMで暗号化し、専用Kubernetes Secretへ保存します。Kubernetes Secretへのアクセス権はRunner APIのServiceAccountに限定します。
6. 各会話Podには短命のaccess tokenだけを渡します。App Serverが401を受けるとRunner APIへrefreshを依頼し、APIがrefresh tokenを一元的に更新して置き換えます。

OpenAIのSIWC OSSフローは `127.0.0.1` のコールバックを指定しているため、PWAのHTTPS URLへ直接コールバックしません。セルフホスト環境ではローカル認証とリモート環境への保護された資格情報転送が案内されています。

## App Server連携

会話Pod内のrunner-agentは、初回にApp Serverの `chatgptAuthTokens` 認証モードを設定します。期限切れ時の `account/chatgptAuthTokens/refresh` JSON-RPC要求をRunner APIへ中継します。このApp Server APIは現在experimentalです。Codex Runnerは会話ごとのPod分離を保つため、この方式を採用します。

## デプロイ設定

`codex-runner-tokens` Secretに、32バイトのランダム暗号化鍵をBase64で登録します。値はSecret管理基盤で生成してください。

```sh
openssl rand -base64 32
```

生成値のSecret key名は `runner-auth-encryption-key` です。暗号化済みトークンは `codex-runner-chatgpt-auth` Secretに保存されます。Kustomizeはこの専用Secretを作成します。APIのServiceAccountには当該Secretのget/updateだけを許可します。

Runner APIは1 replicaで動かしてください。refresh tokenはローテーションされるため、複数API replicaでの同時refreshはサポート対象外です。

既存の静的 `ACCESS_TOKEN` Secret構成も引き続き利用できます。`CODEX_ACCESS_TOKEN_SECRET_NAME`と`CODEX_ACCESS_TOKEN_SECRET_KEY`は会話Podへ固定トークンを注入するための従来設定です。
