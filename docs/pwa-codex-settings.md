# PWAからCodex App Serverの設定を確認する

PWAの「ChatGPT 接続済み」と「利用状況を管理」の横に**設定を確認**ボタンがあります。押すと、実際に起動しているCodex App Serverの設定とアプリ連携の状態を確認できます。

## 取得経路

1. PWAが`GET /v1/codex/settings`をRunner APIに送信
2. 会話が選択されている場合、`conversationId`クエリパラメータで指定したConversation PodのRunner AgentへHTTP GET
3. 会話が選択されていない場合、常駐している認証用Runner Agent（Auth Broker）へHTTP GET
4. Runner Agentが同じPod内のCodex App Serverへstdio JSON-RPCで`config/read`、`app/installed`、`app/read`を送信
5. 返された情報から、必要な項目だけを抽出して同じ経路でPWAに戻す

選択した会話のPodが休止・削除されている場合は取得に失敗します。設定確認のために新しくセッションを起動することはありません。未選択の場合に取得する認証用Agentと会話用Agentは別プロセスであり、設定が一致するとは限りません。

## 表示項目

- Codexのモデル、承認ポリシー、Sandboxモード（`config/read`）
- アプリ共通・個別の承認モードと有効状態（`config/read`）
- インストール済みアプリの有効状態、実行可否（`app/installed`）
- 各ツールの読み取り専用属性、有効状態、無効理由（`app/read`）

`app/installed`または`app/read`の取得に失敗した場合は、アプリ一覧やツール詳細をすべて取得できなかったことを表示し、取得できた範囲の情報とコア設定の表示は継続します。アプリの一覧と個別のアプリ設定は別のものです。

## 安全性

設定内容を変更するAPIは追加していません。Codexから返る`config/read`の生レスポンスをブラウザーに渡すことは**ありません**。APIキー、OAuthトークン、MCPの環境変数、モデルプロバイダーの資格情報、サーバーURLなどは表示対象に含めません。画面上の文字列はすべてテキストとして描画します。

GitHubの読み取り・書き込みツールが表示されても、この画面からGitHub操作が成功することを保証するものではありません。表示はCodex App Serverが報告した利用状態です。
