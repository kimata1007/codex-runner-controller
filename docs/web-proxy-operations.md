# Web Proxy の運用

`config/web-proxy` は公開 IPv4 の HTTPS とクラスタ DNS に発信する Web Proxy を配置する。セッションポリシーの参照に必要な Kubernetes API への経路は、`scripts/render-web-proxy-kube-egress.sh` で生成する。クラスタでグローバル IPv4 アドレスを内部に割り当てる場合は、それらを公開 HTTPS ポリシーの例外にも追加する。

Brave Search を有効にする場合は `codex-security` の Secret `codex-brave-search` に `api-key` をセットする。検索語と取得 URL は監査ログに書かない。検索語や取得 URL は認可された外部サービスへ送信されるため、機密データを含むセッションの権限設定で送信範囲を管理する。

セッション内の入口は `http://198.18.0.12:8080`。

| 操作 | 経路 |
| --- | --- |
| 公開 HTTPS の取得 | `GET /v1/fetch?url=…` |
| Web 検索 | `GET /v1/search?q=…` |
| Go モジュール | `/packages/go/` |
| npm パッケージ | `/packages/npm/` |
| PyPI Simple API | `/packages/pypi/simple/` |
