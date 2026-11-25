# Codex Runner Controller 全体設計書

本書は目標アーキテクチャの規範である。実装の進捗や履歴は記載しない。アーキテクチャ図は [diagrams.md](diagrams.md) を参照する。

## 1. 目的と設計原則

Kubernetes上で会話単位のCodex実行環境を起動・停止・再開し、ユーザーの指示に基づく相談、Web調査、コード編集、ビルド、テスト、Git push、Pull Request作成を自律的に実行する。

**Zero Trust, Maximum Autonomy — AIを信頼せず、AIが自由に動作できる境界を構築する。**

- Session Pod全体を非信頼領域とする。runner-agent、Codex App Server、シェル、取得したソース、依存パッケージ、PVCの内容、AIが返すイベントも信頼しない。
- AIに固定Token、OAuth資格情報、API Key、証明書秘密鍵、Kubernetes API Tokenを渡さない。環境変数、引数、ファイル、ボリューム、履歴、ログのいずれにも配置しない。公開設定値やセッション識別子は秘密として扱わない。
- Workload Identity、mTLS、Authorization、NetworkPolicyを独立した防御層とする。ネットワークの位置や暗黙の信頼で認証を省略しない。
- Node ProxyをGoで自作する。暗号、TLS、OSの隔離機構は標準技術を使用し、汎用Service Mesh全体を再実装しない。
- Runner APIからSession Podへ実行制御接続を開始する。結果・承認要求・イベントはその接続上で返す。
- Default Deny、Fail Closedを徹底する。AIがProxyを迂回したり、別Sessionになりすましたりできないことを必須とする。
- 許可された操作範囲ではコマンド単位の承認を求めない。権限拡張と特に重大な外部操作を別途認可する。

信頼する基盤はKubernetes API、Controller、Runner API、ノードOS、CNI、Node Proxy、Identity Issuer、Policy Store、外部サービスProxyである。各基盤間も明示的な認証・認可を要求する。ノードやCAの侵害はセッション隔離の保証範囲外とし、専用ノードプールと権限の分離により影響を限定する。AIに許可されたデータを許可された外部サービスへ送信するリスクは残る。

## 2. コンポーネント

| コンポーネント | 責務 |
| --- | --- |
| Mobile/Web Client | 会話作成、入力、履歴、SSE、承認、権限設定 |
| Runner API | ユーザー認証・会話認可、Message/Runの永続化、FIFOキュー、セッション制御、イベント検証・SSE、権限の確定 |
| Codex Runner Controller | CodexSessionのreconcile、Pod/PVC/Service/ServiceAccount/通信ポリシーの管理、Idle停止、Identityと権限の失効 |
| Session Pod | 非信頼のrunner-agent、Codex App Server、Shell、Git・Webツール |
| Conversation Store | Conversation、Message、Run、Event、Approvalの永続化 |
| Session PVC | 会話固有のCODEX_HOME、スレッド履歴、Gitワークツリー。秘密情報を保存しない |
| Identity Issuer | ワークロードを検証し、短命X.509証明書を発行する |
| Node Proxy | ノード単位の通信捕捉、送信元Identity解決、L4認可、mTLS、受信認可、監査 |
| Policy Store | 管理側だけが更新できる有効期限付きの認可情報 |
| CNI / NetworkPolicy | 強制的な通信経路、送信元偽装防止、ネットワーク隔離 |
| GitHub Proxy | Git取得・push・PR作成/更新を操作単位で認可し、GitHub認証を代理する |
| Web Proxy | Web検索、公開ページ取得、必要なパッケージ取得を認可する |
| Model Proxy | モデル認証を代理し、利用可能モデル・予算・リクエストを制御する |

会話DBはRunner APIの専用データストアとし、Session Podから直接アクセスさせない。DB製品は独立した永続化設計に従う。Kubernetes CRDにはインフラの望ましい状態のみを保持し、本文・イベント・資格情報は保存しない。

## 3. Workload Identity

Workload Identityはユーザー認証とは独立して、動作中のワークロードを認証する。人間の認可はRunner API、Podの認証はNode ProxyとIdentity Issuerが担当する。IdentityはSPIFFE URI形式とし、例として以下を使用する。

- spiffe://codex.local/system/runner-api
- spiffe://codex.local/system/controller
- spiffe://codex.local/session/conversation-001/pod/PodUID（実際は正規化されたUID）
- spiffe://codex.local/proxy/github
- spiffe://codex.local/proxy/web
- spiffe://codex.local/proxy/model

Identityの発行・利用手順:

1. Controllerが認可済みCodexSessionからPodと専用ServiceAccountを作成する。Pod UIDとNode割当はKubernetes APIを正本とする。
2. CNIがPodのNetwork Namespace、インターフェース、接続世代をPod UIDに結びつけ、ノード側の信頼できる状態に登録する。
3. Node Proxyはノードに紐づいた認証情報でIdentity Issuerへ接続する。Issuerは認証済みノード、Pod UID、ServiceAccount、配置先、管理対象であることを検証し、当該ノードに存在する許可済みワークロードに限り発行を認める。
4. Node ProxyがPodごとに秘密鍵を生成し、CSRの署名により鍵の所有を証明する。Issuerは要求内の自己申告SANを使用せず、検証済み情報からIdentityを構成し署名する。
5. Node Proxyが短命証明書と秘密鍵をAI Pod外に保持する。初期TTLは5分、期限前にローテーションする。CA鍵はIssuer側に隔離する。証明書・鍵・信頼バンドルの配布とローテーションは標準X.509/SPIFFE準拠の実装を利用する。
6. Pod削除・再作成・移動時は古いPod UIDの発行権限、通信ルート、認可、既存接続を失効させる。IP再利用でIdentityを継承しない。

IPアドレス、AIの自己申告ヘッダー、Session IDだけによる認証は禁止する。WIF（Workload Identity Federation）は外部の認証基盤との信頼連携・短命資格情報交換が必要な場合に限って利用し、クラスタ内部のWorkload Identityとは区別する。

## 4. Node Proxyと通信経路

Node Proxyは各対象ノードにDaemonSetとして配置する。信頼されたノード側のCNI連携がPodの通信を捕捉し、Pod UIDと本来の宛先をNode Proxyへ伝える。AI側から設定変更できない経路を用いる。LinuxのNetwork Namespace、veth、iptables/nftablesまたはeBPFを利用できるが、採用する実装は実際のCNI/Sandbox Runtimeとの互換性を満たすことを条件とする。

Node Proxyの内部コンポーネント:

| コンポーネント | 責務 |
| --- | --- |
| Traffic Interceptor | CNI連携、元ソケット・送信元・宛先の信頼できる取得 |
| Identity Resolver | ライブなPod UID・ノード配置と通信元の照合 |
| Certificate Manager | ワークロードごとの証明書・鍵の取得、更新、破棄 |
| Authorization Engine | 送信元Identity、接続先Identity、ポート、方向の完全一致による認可 |
| mTLS Transport | 双方の証明書を検証して通信を中継 |
| Audit Logger | 許可・拒否・接続ID・Identity・時刻を記録 |

送信側Node Proxyは送信元ワークロードの証明書を用い、受信側Node Proxyは目的ワークロードの証明書を用いる。双方がCAチェーン、有効期限、用途、期待するURI SAN、秘密鍵の所有を検証する。TLS 1.3を使用する。受信側でも独立して送信元と宛先を認可する。同じノード内の通信も同じ認可を通過させる。mTLSは双方の身元と通信の機密性・完全性を検証するものであり、相手の操作を無条件に許可するものではない。

Node ProxyはGitHub操作やWeb検索等の業務ロジックを持たない。外部サービスProxyへ伝えるIdentityは、認証済み接続に結びつける。受信Proxyから宛先サービスへのローカル区間は外部から迂回できないよう隔離し、呼び出し元が任意に指定するヘッダーを信用しない。

## 5. AuthorizationとNetworkPolicy

L4の接続認可とL7の操作認可を分離する。Policy Storeの権限はユーザー認可済みのセッション設定から生成し、Controllerのみが更新できる。未登録・期限切れ・検証失敗時は拒否する。変更・失効は既存接続にも適用し、検証できなくなった接続を有限時間内に切断する。

| 通信元 | 宛先 | 許可範囲 |
| --- | --- | --- |
| Runner API | Session Pod | 対象セッションの制御接続 |
| Runner API | Conversation Store | 会話・実行状態の保存 |
| Runner API / Controller | Kubernetes API | 各RBACで限定された操作 |
| Session Pod | GitHub Proxy | 認可済みRepository・操作 |
| Session Pod | Web Proxy | 検索・公開ページ・パッケージ取得 |
| Session Pod | Model Proxy | 認可済み推論 |
| Node Proxy | Identity Issuer / Policy Store | ノードに許された認証・認可情報 |
| Session Pod | Runner API / DB / Kubernetes API / 他Session / 外部ネットワーク | 新規接続を拒否 |

表は新規接続の方針であり、許可済み接続の応答と明示的に必要な基盤通信は別途定義する。DNSは必要最小限とし、任意の外部DNS・DoH・直接IP・IPv6・ホスト/メタデータ・同一ノード経路を含む迂回を遮断する。Kubernetes NetworkPolicyの適用例外（ノード通信等）をCNI/ホスト側の強制で補う。NetworkPolicyが実際に有効なCNIを必須とする。

## 6. AI実行環境とライフサイクル

- 1 Conversationにつき最大1アクティブCodexSession、1 Sessionにつき最大1 Pod、1 turnのみ同時実行する。
- 管理領域、認証・Proxy領域、非信頼実行領域をNamespace/RBACで分離する。Session PodにはgVisor等のSandbox Runtime、Restricted Pod Security、non-root、seccomp、capability drop ALL、no privilege escalation、read-only root filesystem、リソース上限を適用する。
- hostNetwork、hostPID、hostIPC、hostPath、特権コンテナ、ServiceAccount Tokenの自動マウント、共有書き込み領域は禁止する。Session PVCと必要最小限の一時領域だけ書き込み可能とする。
- ControllerはPodの起動前から隔離を適用し、Identity、認可、強制経路が整うまでRunner APIへReadyを返さない。隔離不能なら実行しない。
- runner-agentは非信頼Pod内でCodex App Serverをstdio JSON-RPCで起動し、initialize、thread/startまたはthread/resume、turn/start、承認、ストリーミングを仲介する。管理用Tokenや外部資格情報は持たない。
- Runner APIはメッセージを永続化し、FIFOキューから原子的にclaimする。Runner APIが対象Podへの認証済み制御接続を開始し、コマンド送信とイベント受信を行う。Session Podからの新規管理接続を許可しない。イベントのRun/Session所有関係、順序、サイズを検証し、Podが送信する内容を管理権限の証明として扱わない。
- Idle時は実行中・承認待ちでないPodを停止する。PVCと会話履歴は保持する。次のメッセージで新しいPod UIDとIdentityを発行して再開する。削除時は認可・ルート・資格情報を失効させる。

CodexSession CRDはconversationId、desiredState、idleTimeoutSeconds、sessionData、execution、repository、permissionsを持つ。permissionsは管理側が確定した操作範囲とし、Secret名・Tokenは含めない。statusにはphase（Suspended/Starting/Ready/Busy/WaitingForApproval/Failed/Terminating）、threadId、podName、workspaceClaim、lastActivityAt、observedGeneration、conditionsを保持する。

## 7. 外部サービスProxy

### 7.1 GitHub Proxy

GitHub AppのRepository限定・短命installation tokenをProxyだけが取得する。AIは標準git/gh互換インターフェース、または限定された操作APIを利用する。Tokenを返却するcredential helperは禁止する。Git smart HTTPの転送ではpush対象refを検証するか、同等の強制可能なRepository rulesetと組み合わせる。

セッションごとにRepository、許可操作、専用ブランチprefix、PR baseを固定する。clone、fetch、commit、許可ブランチへのpush、PR作成・更新は自律的に実行可能とする。main等の保護ブランチへの直接push、他Sessionのブランチ書き込み、任意Repository、merge、破壊的操作は拒否または別途人間が認可する。GitHub Actions経由の権限昇格やSecret流出を防ぐため、非信頼PRで特権workflowを実行しない。Proxyの資格情報や上流の認証ヘッダーは応答・ログに含めない。

### 7.2 Web Proxy

検索エンジンの検索APIと公開Webページの取得を提供する。必要なパッケージ取得も許可済みの取得経路で提供する。任意のHTTP CONNECTや未検証の汎用トンネルは提供しない。公開HTTPS宛先のみを許可し、DNS解決結果の検証と接続先固定、リダイレクトの再検証、プライベート・予約済みIP、メタデータAPI、クラスタ内部宛先の拒否を行う。取得サイズ、時間、回数、並列数を制限する。検索語やURLによるデータ送信は可能であるため、機密Repositoryの外部通信はユーザーの権限設定に従う。

### 7.3 Model Proxy

OpenAI等への推論リクエストを代理する。API Keyまたは対応するユーザー認証は信頼領域で管理し、Session Podに実資格情報を渡さない。利用者・Session・モデル・予算を認可し、推論ストリーミングを中継する。Codex App Serverの接続先変更とAPI互換性を満たすこと。ChatGPTアカウント認証を外部化できない方式は有効化しない。認証方式ごとに公式に対応する接続方式を検証し、失敗時にToken注入へフォールバックしない。

## 8. APIとデータ

公開APIはRunner APIに限定する。Tailnet等は到達範囲を制限するが、ユーザー認証・会話所有権の検証を省略しない。

- POST /v1/conversations
- GET /v1/conversations/{id}
- GET /v1/conversations/{id}/messages
- POST /v1/conversations/{id}/messages
- GET /v1/runs/{runID}/events
- POST /v1/approvals/{runID}/{requestID}

データモデル:

| Entity | 主な属性 |
| --- | --- |
| Conversation | id, user_id, title, status, codex_session_name, created_at, updated_at |
| Message | id, conversation_id, role, content, sequence, status, client_message_id, created_at |
| Run | id, conversation_id, message_id, thread_id, status, started_at, completed_at, error_code, error_message |
| Event | id, run_id, sequence, event_type, payload, created_at |
| Approval | id, run_id, request_id, action_type, summary, status, response, created_at, resolved_at |
| SessionPolicy | session, pod_uid, grants, version, expires_at |
| SecurityAudit | timestamp, source_identity, destination_identity, operation, decision, reason, request_id |

Messageはconversation_idとsequenceを一意とし、client_message_idで冪等に処理する。Runの状態はqueued/running/waiting_approval/completed/failed/interruptedとする。永続化を先に行い、SSE再接続で必要な履歴を復元する。SecurityAuditには秘密、検索語や本文等の機密データを無条件に保存しない。保管期間・アクセス権を設定する。

## 9. 障害と運用

| 障害 | 挙動 |
| --- | --- |
| Node Proxy / CNI | 直接通信へフォールバックせず拒否 |
| Identity Issuer / Policy Store | 新規認証・認可に失敗したら拒否。既存接続も期限や失効に従い切断 |
| Runner API / Conversation Store | 永続化できない実行を開始せず、再接続・冪等処理で復旧 |
| Controller | 再起動後reconcile。多重Podを作らない |
| Session Pod | 新しいIdentityで再作成。PVCからthread/resume |
| 外部Proxy / 資格情報期限切れ | 操作を失敗させ、信頼領域で再取得。AIへ資格情報を渡さない |

外部書き込みの結果が不明な場合は無条件再試行せず、冪等性または上流の状態確認によって二重実行を防ぐ。DB/PVCのバックアップ・復元、CA鍵の保護・ローテーション、監査ログ、利用量とリソース上限を運用要件とする。

## 10. 実装・受入条件

全コンポーネントのコード、Controller統合、デプロイマニフェスト、コンテナイメージ、CI、E2Eを揃える。次を満たさない構成を本番のゼロトラスト実行環境として有効化してはならない。

| 検証 | 合格条件 |
| --- | --- |
| Workload Identity | 他Pod/他Node/削除済みPodのIdentityを取得・偽装できない |
| mTLS | 不正CA、期限切れ、別Identity、平文接続を拒否 |
| CNI/NetworkPolicy | 同一・異なるノード、直接IP、IPv6、DNS、ホスト、メタデータ経由の迂回を拒否 |
| Authorization | 別Session、未許可Repository、保護branch、merge、DB/K8sを拒否。失効済み接続を切断 |
| Secretless | env、proc、ファイル、PVC、ログ、Proxy応答に実資格情報がない |
| Sandbox | ホスト・他Session・共有データへのアクセスを拒否し、リソース上限が機能する |
| 正常系 | 会話、再開、検索、取得、編集、ビルド、テスト、Git push、PR作成/更新、モデル推論が動作 |
| 障害系 | Proxy/CNI/Issuer/Policyの停止・期限切れでFail Closed、復旧時も権限逸脱なし |

実クラスタに相当する複数Nodeの環境で正常系と攻撃想定E2Eを実行する。単体テストのみでは通信経路の強制・隔離の成立を証明したことにはならない。

## 11. 参考資料

- [Kubernetes ServiceAccount](https://kubernetes.io/docs/concepts/security/service-accounts/)
- [Kubernetes NetworkPolicy](https://kubernetes.io/docs/concepts/services-networking/network-policies/)
- [Pod Security Standards](https://kubernetes.io/docs/concepts/security/pod-security-standards/)
- [SPIFFE](https://spiffe.io/)
- [TLS 1.3 (RFC 8446)](https://www.rfc-editor.org/rfc/rfc8446)
- [gVisor](https://gvisor.dev/)
- [GitHub Apps](https://docs.github.com/en/apps)
