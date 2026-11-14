# assistant messageの永続化

PWAはrun中のdeltaをSSEで表示しますが、SSEは一時的な表示経路です。ページ再読み込みや別端末からの閲覧で応答を復元できるよう、Runner APIは完了イベントを受けた時点でassistant messageをlibSQLへ投影します。

投影対象は`agentMessage/delta`、`item/agentMessage/delta`、`item/agent_message/delta`です。イベントpayloadから`delta`文字列をsequence順に連結し、`role=assistant`、`status=completed`のmessageとして保存します。tool、approval、未知の通知はassistant本文へ混ぜません。

assistant messageのIDは`assistant-<runID>`で固定されます。イベント再送や投影リトライでは同じ行を更新するため、重複assistant messageは作成されません。
