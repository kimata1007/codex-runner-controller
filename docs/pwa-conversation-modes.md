# PWAの会話モードと復元

PWAで`chat`と`implement`を切り替えられます。

- `chat`: リポジトリなしの相談
- `implement`: GitリポジトリURLとrevisionを指定した実装作業

会話IDはsessionStorageに保持し、ページ再読み込み時に`GET /v1/conversations/{id}/messages`で履歴を復元します。Codexのthread状態はPodのPVC側、表示用のメッセージはlibSQL側から復元されます。
