# Conversation Data

libSQL/sqldをシステム・オブ・レコードとして使うための最初のスキーマを `internal/db/migrations/001_initial.sql` に定義する。

## 保存する単位

- `conversations`: ユーザーが見る会話とCodexSessionの対応
- `messages`: ユーザー、assistant、systemのメッセージ
- `runs`: 1つのユーザーメッセージに対するCodexの実行
- `events`: ストリーミングや実行状態のイベント
- `approvals`: Codexからの承認要求とユーザーの回答

会話を `chat` と `implement` のモードには分けない。会話の種類が将来必要になった場合は、別の要件として改めて設計する。

## 冪等性

クライアントは送信ごとに `clientMessageId` を生成する。`messages` の `(conversation_id, client_message_id)` 一意制約により、ネットワーク再送で同じユーザーメッセージが二重登録されないようにする。

## 順序

メッセージは会話内の `sequence`、イベントはRun内の `sequence` で順序を持つ。SSEへ再接続する際は、この順序を使って未受信イベントを再取得できるようにする。

## セキュリティ

メッセージ本文、Codexの認証トークン、GitHubのアクセストークンはKubernetesのCRDへ保存しない。DBの認証情報は後続のKubernetes Secretと環境変数で注入する。
