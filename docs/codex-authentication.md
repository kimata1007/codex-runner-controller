# Codex RunnerのChatGPT認証

## 接続フロー

1. 画面下部、メッセージ入力欄の直上にある「ChatGPTに接続」を押します。メッセージ送信は必要ありません。
2. Codex Runnerの認証エージェントがCodex App Serverのデバイスコードログインを開始します。
3. 画面に表示されたリンクを開き、コードを入力してChatGPTにサインインします。スマートフォンだけで完了できます。
4. Runner APIが接続状態を確認し、認証情報をAES-GCMで暗号化して専用Kubernetes Secretへ保存します。
5. 以後の会話ではRunner APIが必要な短命access tokenを各会話Podへ渡します。会話Podごとのサインインは不要です。

認証エージェントは会話用Podから独立した1つのDeploymentです。デバイスコードを発行するApp Serverのログイン状態はこのエージェント内に保持され、APIが認証情報を保存した後は暗号化Secretを正本として使います。UIの接続状態確認を続けられない場合、認証エージェントが再起動する前にもう一度サインインしてください。

## App Server連携

認証エージェントは公式の`account/login/start` `chatgptDeviceCode`フローを使います。会話PodのApp Serverはexperimentalな`chatgptAuthTokens`モードでRunner APIから短命tokenを受け取り、期限切れ時にrefreshを依頼します。refresh tokenはRunner APIが一元管理します。

## デプロイ設定

`codex-runner-tokens` Secretに、32バイトのランダム暗号化鍵をBase64で登録します。値はSecret管理基盤で生成してください。

```sh
openssl rand -base64 32
```

生成値のSecret key名は`runner-auth-encryption-key`です。暗号化済みトークンは`codex-runner-chatgpt-auth` Secretに保存されます。Kustomizeはこの専用Secretを作成します。APIのServiceAccountには当該Secretのget/updateだけを許可します。

Runner APIは1 replicaで動かしてください。refresh tokenはローテーションされるため、複数API replicaでの同時refreshはサポート対象外です。

既存の静的`ACCESS_TOKEN` Secret構成も引き続き利用できます。`CODEX_ACCESS_TOKEN_SECRET_NAME`と`CODEX_ACCESS_TOKEN_SECRET_KEY`は会話Podへ固定tokenを注入するための従来設定です。
