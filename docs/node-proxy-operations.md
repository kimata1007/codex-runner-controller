# Node Proxy と CNI の運用

Node Proxy は Linux ノード上の DaemonSet として動作する。`config/node-proxy` の適用前に Identity Issuer の Service と RBAC を配置し、HTTPS を検証する公開 CA を `codex-security` の ConfigMap `codex-identity-issuer-trust` にキー `ca.crt` で用意する。

テスト対象のプライマリ CNI は、ポイントツーポイント veth を使う Kindnet。DaemonSet の init コンテナは既存の `10-kindnet.conflist` の末尾に `codex-session-cni` を追加する。別の名前の conflist を使うときは `-install-cni` のパスを設定する。Session Pod の作成は対象ノードで CNI の構成が完了してから行う。

| 経路 | 設定値 |
| --- | --- |
| Node Proxy 間 | 各 Node の InternalIP の TCP 9443 |
| Runner API のローカル入口 | `/run/codex-node/runner-api.sock` |
| GitHub・Web・Model Proxy のローカル出口 | 各 Role ディレクトリ内の Pod UID ごとの Unix ソケット |
| Session の GitHub・Web・Model 宛先 | `198.18.0.11:8080`、`198.18.0.12:8080`、`198.18.0.13:8080` |

Runner API と各サービス Proxy は、対応するソケットへのアクセスに `runAsGroup: 65532` を使用する。Node Proxy のカーネル操作には `NET_ADMIN` を使用する。

Session Pod の送信元は CNI が記録した Pod UID、peer veth とネットワーク名前空間で特定する。Node Proxy がバインドしたソケットだけをカーネルが許可する。Session からの IPv6 と UDP は遮断する。IPv4 の外部取得は各サービス Proxy を使う。

Pod 停止時は veth を先に撤去してからフィルターを削除する。Node Proxy 自体の停止中はフィルターが残る。Node Proxy はフィルターが失われたことを検出した場合に該当 veth を撤去する。
