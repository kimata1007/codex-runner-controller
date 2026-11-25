# Identity Issuer の運用

`config/identity-issuer` は Identity Issuer、Service、参照専用 RBAC、CRD を配置する。Kubernetes v1.32 以降の Pod-bound TokenReview を使用する。実行用 Namespace には Restricted Pod Security を適用する。

## 外部設定

`codex-security` に次の Secret を用意する。

| Secret | 内容 |
| --- | --- |
| `codex-identity-signing-root` | `ca.crt` と `ca.key` は署名用の自己署名 CA。 `bundle.crt` は現在信頼する CA 証明書を連結した PEM。 |
| `codex-identity-issuer-tls` | `tls.crt` と `tls.key` は Issuer Service の HTTPS 証明書。接続に用いる DNS 名を SAN に含める。 |

Node Proxy には HTTPS 証明書を検証する CA と、Audience が `codexrunner.io/identity-issuer` の投影 ServiceAccount Token を設定する。Issuer は `codex-security` の `codex-node-proxy` DaemonSet に属する、稼働中の Pod を認証する。署名用 CA の秘密鍵は Issuer の Secret マウントでのみ取り扱う。

## ローテーションと失効

1. 新旧両方の公開 CA を `bundle.crt` に入れ、クライアントへ行き渡るまで待つ。
2. 新しい `ca.crt` と `ca.key` を同じ Secret リビジョンで更新する。
3. 古い CA の最終発行から最大証明書有効期間が経過したら、古い公開 CA をバンドルから外す。

証明書の既定 TTL は 5 分。セッション用証明書の有効期限は対応する `SessionPolicy` と CA の期限で短縮される。発行を停止するには `SessionPolicy.spec.version` を増やして `revoked` を有効にする。Pod UID を再認可する操作は Kubernetes API が拒否する。

`tests/e2e/identity` では 2 台の Worker を使い、実際の TokenReview、他ノードからの発行拒否、失効、発行済み証明書の署名を検証する。
