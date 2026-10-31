# Codex Runner Controller 全体設計書
- ステータス: Draft
- 作成日: 2026-10-31
- 対象リポジトリ: codex-runner-controller
- 目的: Kubernetes上で、会話単位のCodex実行環境を安全に起動・停止・再開する
## 1. 概要
本システムは、モバイルまたはWeb UIから受け取った会話を、Kubernetes上のセッションPodでCodex App Serverに処理させるための基盤である。
会話が始まるとAPIが会話とメッセージをlibSQLへ保存し、CodexSessionカスタムリソースを作成または更新する。Codex Runner Controllerはそのカスタムリソースを監視し、必要なセッションPodとPVCを作成する。Pod内のrunner-agentはCodex App Serverを子プロセスとして起動し、保存済みのスレッドを再開してターンを実行する。
会話が一定時間アイドルになった場合、ControllerはPodだけを削除する。CodexSession、libSQLの会話記録、必要なPVCは保持する。次のメッセージが来たときは同じCodexSessionを再アクティブ化し、Podを再作成してthread/resumeを実行する。
この設計における「ステートレス」はPodを使い捨てにするという意味であり、会話を再開するための状態まで捨てるという意味ではない。
## 2. 設計方針
### 2.1 JobではなくセッションPodを使う
Kubernetes Jobは、1回の処理が完了したら終了するバッチ処理に適している。一方、今回のCodex実行は、ユーザーの追加入力、ストリーミング、承認要求、実装作業を同じスレッドで継続する必要がある。
したがって、会話ごとにControllerが管理する通常のPodを使用する。
- 1つのConversationに対して最大1つのアクティブなCodexSession
- 1つのCodexSessionに対して最大1つのrunner Pod
- 1つのSessionでは同時に1つのturnだけを実行
- Pod障害時はControllerが再作成する
- アイドル時はPodを削除するが、会話とセッション状態は保持する
### 2.2 Codex App Serverを使用する
Codex App Serverは、アプリケーションからCodexを組み込むためのプロトコルである。initialize、thread/start、thread/resume、turn/start、ストリーミングイベント、承認要求を扱えるため、モバイルUIの会話と実装作業を同じスレッドに接続できる。
Codex App Serverはrunner-agentと標準入出力で接続する。APIサーバーが直接Codexプロセスを操作するのではなく、runner-agentがPod内でApp Serverを起動して管理する。
codex execは単発のスクリプトやCIジョブには適しているが、今回のような継続会話、ストリーミング、割り込み、ユーザー承認を必要とする用途にはApp Serverを採用する。
### 2.3 CRDはインフラの望ましい状態を表す
メッセージ本文やストリーミングイベントをKubernetesのetcdやCRDに保存しない。CRDにはセッションの識別子、リポジトリ、ライフサイクル、認証情報への参照など、ControllerがPodを管理するために必要な情報だけを保存する。
会話、メッセージ、ターン、イベント、承認要求はlibSQLに保存する。
## 3. コンポーネント
### 3.1 Mobile/Web Client
会話画面を提供する。初期リリースではテキスト入力に限定し、音声入力は後続機能とする。
責務:
- 会話の作成
- メッセージの送信
- 会話履歴の表示
- ストリーミング応答の表示
- 承認要求の表示と回答
- 会話中の相談と実装作業の継続
Codex公式モバイルアプリを任意のKubernetesバックエンドへ接続する設計にはしない。本システム用の薄いWebまたはモバイルクライアントを用意する。
### 3.2 API Server
外部クライアントからの唯一の入口である。公開するのはAPI Serverだけで、runner PodやlibSQLを直接インターネットへ公開しない。
責務:
- ユーザー認証と認可
- Conversation、Message、Runの永続化
- CodexSession CRの作成・更新
- runner-agentとの内部通信
- App Serverイベントのクライアントへの配信
- 承認要求の受付
- 冪等性とメッセージ順序の管理
API ServerはPodを直接作成しない。Pod作成はCodex Runner Controllerに委譲する。
### 3.3 Codex Runner Controller
Goとcontroller-runtimeで実装するKubernetes Controllerである。CodexSessionを監視し、セッションPod、PVC、必要なServiceを望ましい状態へ収束させる。
責務:
- CodexSessionのwatchとreconcile
- session Podの作成、readiness確認、削除
- セッションPVCの作成と保持ポリシーの適用
- ownerReferenceによるリソース管理
- idle timeoutの判定
- Pod障害時の再作成とバックオフ
- CodexSession.statusの更新
- Podへの安全な設定・Secret・ServiceAccountの注入
Controllerはユーザーのメッセージ本文を解釈しない。リポジトリや実行権限などの実行条件はAPIとユーザー操作で確定し、ControllerはCRに記載された望ましい状態を実現する。
### 3.4 runner-agent
runner Pod内で起動する小さなプロセスである。Controllerではなく、Pod内の実行責務を担当する。
責務:
- Codex App Serverを子プロセスとして起動
- stdin/stdoutのJSON-RPCを処理
- initializeとinitializedを実行
- 新規セッションではthread/startを実行
- 既存セッションでは保存済みthread IDでthread/resumeを実行
- turn/startを実行
- agentMessage/deltaなどのイベントをAPI Serverへ転送
- turn/completed、failed、interruptedを記録
- approval requestをAPI Serverへ転送
- heartbeatと最終活動時刻を通知
- App Server終了時の再起動またはPod終了
App Serverとrunner-agentは同じコンテナ内で起動する子プロセス構成を基本とする。App Serverを独立したサイドカーにするとstdin/stdoutの接続とプロセスライフサイクルの管理が複雑になるため、初期実装では採用しない。
### 3.5 Codex App Server
runner-agentがPod内で起動する実プロセスである。アプリケーションからは直接公開しない。
代表的なライフサイクル:
1. runner-agentがcodex app-server --listen stdio://を起動
2. initializeを送信
3. initializedを送信
4. 新規ならthread/start、既存ならthread/resume
5. ユーザー入力ごとにturn/start
6. ストリーミングイベントをrunner-agentがAPI Serverへ転送
7. turn/completedまたは承認要求を待つ
### 3.6 libSQL
アプリケーションのシステム・オブ・レコードとして使用する。初期構成では現在のKubernetesクラスタ内にsqldをStatefulSetとして配置し、PVCでデータを保持する。
HTTPで接続できることはクライアントPodをステートレスにできる利点であるが、libSQLサーバー自体はステートフルである。したがって、以下を必須とする。
- StatefulSet
- PVC
- ClusterIP Service
- 認証用Secret
- バックアップと復元手順
- データベース障害時のアラート
現在のクラスタがlocal-pathのノードローカルストレージを使う場合、ノード障害だけではデータを守れない。定期バックアップを別の保存先へ送る設計を別途用意する。
## 4. Kubernetesリソース
### 4.1 CodexSession CRD
主リソースはCodexSessionとする。CodexRunは1ターンの実行記録としてlibSQL上の概念に留め、初期リリースではCRDとして作らない。
例:
    apiVersion: codexrunner.io/v1alpha1
    kind: CodexSession
    metadata:
      name: conversation-001
      namespace: codex-system
    spec:
      conversationId: conversation-001
      desiredState: Active
      idleTimeoutSeconds: 1800
      sessionData:
        storageClassName: local-path
        size: 10Gi
        retain: true
      execution:
        model: default
        approvalPolicy: user
      repository:
        url: https://github.com/kimata1007/example.git
        revision: main
    status:
      phase: Ready
      threadId: thread_123
      podName: codex-session-abc
      workspaceClaim: codex-session-abc
      lastActivityAt: "2026-10-31T00:00:00Z"
      observedGeneration: 1
      conditions: []
実際のCRDでは、repositoryは実装対象がある場合に指定する。会話開始時点でリポジトリなしでも作成できる。
### 4.2 spec
- conversationId: libSQL上のConversation ID
- desiredState: ActiveまたはSuspended
- idleTimeoutSeconds: Podを停止するまでのアイドル時間
- sessionData: Codex履歴とワークスペースを保持するPVCの設定
- execution.model: 使用モデルの論理名
- execution.approvalPolicy: 承認を要求するポリシー
- repository.url: 実装対象リポジトリ
- repository.revision: ブランチ、タグ、またはコミット
- credentialsRef: GitHubやモデル認証のSecret参照
トークン、ユーザーメッセージ、会話本文、OAuth access tokenはCRDに保存しない。
### 4.3 status.phase
初期の状態は次のとおりとする。
- Suspended: Podが存在しない休止状態
- Starting: PodまたはPVCを作成中
- Ready: runner-agentが接続可能
- Busy: turnを実行中
- WaitingForApproval: ユーザー承認待ち
- Failed: 再試行可能なエラー
- Terminating: Pod削除処理中
### 4.4 Pod
PodはControllerが直接作成する。DeploymentやJobは使わない。
Podの構成:
- runner-agentコンテナ
- runner-agentが子プロセスとして起動するCodex App Server
- session-data PVC
- 必要に応じてGitHub認証Secret
-専用ServiceAccount
- readinessProbeとlivenessProbe
- rootless実行、capability drop、seccomp、read-only root filesystem
- workspaceだけ書き込み可能
runner PodはClusterIP ServiceまたはAPI Serverからのみ到達可能にし、外部公開しない。
### 4.5 PVC
Pod削除後も会話を再開するため、セッションごとにPVCを保持する。
PVCには少なくとも次を保存する。
- Codexのローカルスレッド履歴
- CODEX_HOME
- 実装用のGitワークツリー
- 途中生成物
- 必要なセッション固有のキャッシュ
PVCを削除すると、libSQL上の会話履歴は残っていても、App Serverのthread/resumeだけでは同じ実行状態を完全に復元できない場合がある。PVCの保持は、会話再開と実装途中状態の保持に必要である。
## 5. データモデル
### 5.1 conversations
- id
- user_id
- title
- status
- codex_session_name
- created_at
- updated_at
### 5.2 messages
- id
- conversation_id
- role: user、assistant、system
- content
- sequence
- status
- client_message_id
- created_at
conversation_idとsequenceに一意制約を置き、クライアント再送時はclient_message_idで冪等に処理する。
### 5.3 runs
- id
- conversation_id
- message_id
- thread_id
- status: queued、running、waiting_approval、completed、failed、interrupted
- started_at
- completed_at
- error_code
- error_message
### 5.4 events
- id
- run_id
- sequence
- event_type
- payload
- created_at
すべてのdeltaを長期保存するとデータ量が増えるため、初期リリースではUIに必要なイベントと最終メッセージを保存する。監査用途が必要になった場合は保持期間を設定したイベント保存を追加する。
### 5.5 approvals
- id
- run_id
- request_id
- action_type
- summary
- status: pending、approved、rejected、expired
- response
- created_at
- resolved_at
## 6. リクエストから実行までの流れ
### 6.1 新しい会話
1. クライアントがPOST /v1/conversationsを呼ぶ。
2. API ServerがConversationをlibSQLに作成する。
3. API ServerがCodexSessionを作成する。初期状態はdesiredState=Suspendedでもよい。
4. クライアントが最初のメッセージを送る。
5. API ServerがメッセージをlibSQLに保存する。
6. API ServerがCodexSessionのdesiredStateをActiveへ更新する。
7. ControllerがPVCとPodを作成する。
8. runner-agentが起動し、App Serverを起動する。
9. runner-agentがthread/startを実行し、thread IDをAPI Serverへ通知する。
10. runner-agentがキューからメッセージを取得し、turn/startを実行する。
11. イベントをAPI Serverへ転送し、API ServerはlibSQLへ状態を保存しつつクライアントへSSEで配信する。
12. turn/completedを受け取ったらRunとassistant messageを完了させる。
### 6.2 既存会話の再開
1. 休止中のConversationへクライアントがメッセージを送る。
2. API ServerがメッセージをlibSQLへ保存する。
3. API ServerがCodexSessionをActiveへ更新する。
4. ControllerがPodを再作成する。PVCは既存のものを再アタッチする。
5. runner-agentがApp Serverを起動する。
6. 保存済みthread IDを使用してthread/resumeを実行する。
7. 保存済みメッセージを二重に送らず、新しいユーザーメッセージだけをturn/startする。
8. 実行中のRunとイベントを再接続する。
thread IDはlibSQLとCodexSession.statusの両方に保持してもよいが、システム・オブ・レコードはlibSQLとする。statusはControllerや運用者が観測するための参照情報とする。
### 6.3 会話中に実装を開始する場合
会話開始時に相談用と実装用を固定しない。自然言語だけで書き込み権限を自動的に拡張せず、実装操作が必要になった時点で対象リポジトリ、ブランチ、GitHub権限、承認ポリシーを確定する。
既存の会話で実装を開始する場合も、thread IDとPVCを維持したまま、必要なワークスペースを用意する。
実装を開始する前に次を確認する。
- リポジトリとrevisionが明示されている
- GitHub Secretが参照可能である
- ワークスペースPVCがマウントされている
- 書き込み操作の承認ポリシーが設定されている
- Pod削除時も未コミット変更をPVCに保持する
## 7. アイドルとPod削除
Controllerは、次の条件をすべて満たした場合にPodを削除する。
- desiredStateがActiveでない、またはidle timeoutを超えている
- turnがrunningではない
- approvalがpendingではない
- runner-agentのheartbeatが停止中で、削除対象が明確である
削除時に保持するもの:
- CodexSession CR
- Conversation、Message、Run、Approval
- session-data PVC
- thread ID
- session Pod用のClusterIP Service
削除するもの:
- runner Pod
新しいメッセージが来た場合は、API ServerがdesiredStateをActiveへ戻し、Controllerが再作成する。
初期値のidleTimeoutSecondsは1800秒とする。実装中や承認待ちの場合はアイドル扱いにしない。
## 8. APIとrunner-agentの通信
初期実装では、API Serverを実行キューとイベントの窓口とする。
公開API:
- POST /v1/conversations
- GET /v1/conversations/{id}
- GET /v1/conversations/{id}/messages
- POST /v1/conversations/{id}/messages
- GET /v1/runs/{runID}/events
- POST /v1/approvals/{runID}/{requestID}
内部API:
- POST /internal/runs/{runID}/events
- POST /internal/sessions/{id}/heartbeat
runner-agentからAPI Serverへのアウトバウンド接続を基本とする。API ServerがPodへ直接接続する方式に比べ、Podの再作成やネットワーク一時障害に強く、runner Podを外部公開する必要がない。
内部APIはPod単位の短期トークンまたはServiceAccount認証で保護する。メッセージの取得はleaseまたはDBトランザクションで1回だけ実行されるようにする。
## 9. 認証とSecret
認証方式は実装時にProviderとして分離する。
- App Serverへのモデル認証
- ユーザーごとのChatGPT OAuthまたはサーバー管理のAPI認証
- GitHub APIまたはGit over HTTPS/SSH認証
- クライアントとAPI Server間のユーザー認証
アクセストークンをlibSQL、CRD、Podのログへ保存しない。Kubernetes Secretまたは外部Secret管理からPodへ注入し、runner-agentにだけ参照させる。
Codex App ServerをSign in with ChatGPTで利用する場合、App Server起動時にアクセストークンを環境変数へ渡し、トークン更新時はApp Serverを再起動して保存済みthread IDでthread/resumeする。トークン更新をrunner-agentまたはAPI Serverの責務として明確化する。
## 10. 障害時の挙動
### API Server障害
libSQLに保存済みのメッセージは失われない。クライアントはclient_message_idで再送できる。ストリーミング接続は切断されるが、完了済みメッセージは履歴から取得できる。
### Controller障害
Controller再起動後にCodexSessionを再concileする。PodとPVCはownerReferenceに基づき再発見する。Controllerは多重Podを作らないよう、Pod名またはラベルで既存リソースを確認する。
### runner Pod障害
PodはControllerにより再作成される。PVCとthread IDを保持し、runner-agentはApp Serverを再起動してthread/resumeを実行する。実行中Runは一定時間後に再試行可能な状態へ遷移させる。
同じturnを自動再実行すると重複した副作用が発生する可能性があるため、再試行ポリシーは操作の種類ごとに分ける。GitHubへのpushや外部システムへの書き込みは自動再試行せず、ユーザー確認を要求する。
### libSQL障害
新しいメッセージ受付とイベント保存を停止する。既存Podは実行を継続できる場合があるが、状態を保存できないため、一定時間後に安全に停止する。バックアップからの復元手順を運用Runbookに記載する。
### App Server認証期限切れ
runner-agentは認証エラーを検出してAPI Serverへ通知する。API Serverまたは認証Providerがトークンを更新し、App Serverを再起動してthread/resumeする。無限再試行はせず、認証が必要な状態をユーザーへ表示する。
## 11. セキュリティ境界
- 外部公開するのはAPI Serverだけ
- runner PodとlibSQLはClusterIPで内部公開
- runner PodごとにServiceAccountを分離する
- runner Podの権限は最小化する
- ControllerのRBACはCodexSession、Pod、PVC、必要なServiceに限定する
- GitHub Secret、モデル認証、ユーザーOAuthをログへ出さない
- Podはrootless、capability drop、seccompを適用する
- workspaceへの書き込みと外部ネットワーク接続を明示的に許可する
- 外部へのpush、削除、課金、メール送信などは承認を要求する
- 監査対象の操作はApprovalとRunに記録する
## 12. 推奨リポジトリ構成
    api/
      cmd/api/
      internal/http/
      internal/store/
      internal/session/
    controller/
      api/v1alpha1/
      internal/controller/
      cmd/main.go
    runner-agent/
      cmd/runner-agent/
      internal/appserver/
      internal/queue/
      internal/transport/
    deploy/
      crd/
      controller/
      runner/
      libsql/
    docs/
      design.md
      operations.md
      security.md
初期段階ではAPI、Controller、runner-agentを同じリポジトリで管理する。イメージはコンポーネントごとに分け、リリースは同じバージョン番号で追跡できるようにする。
## 13. 実装フェーズ
### Phase 1: 最小の会話ループ
- Goプロジェクト初期化
- libSQL接続とMigration
- ConversationとMessage API
- CodexSession CRD
- ControllerによるPod作成
- runner-agentによるApp Server起動
- thread/startとturn/start
- 最終応答の保存
### Phase 2: セッション再開
- session-data PVC
- thread ID保存
- thread/resume
- Pod削除とidle timeout
- Pod再作成
- ストリーミングイベント
### Phase 3: リポジトリを使った実装
- Gitリポジトリ取得
- branchとworkspaceの管理
- GitHub Secret
- 差分確認
- 承認要求
- pushやPull Request作成の明示承認
### Phase 4: 運用強化
- NetworkPolicy
- バックアップと復元テスト
- メトリクスとログ
- 失敗時の再試行とdead-letter状態
- 認証更新
- 複数ユーザー対応
- 期限付きPVCとコスト制御
音声入力はPhase 4以降のクライアント機能として扱い、実行基盤と分離する。
## 14. 未決事項
実装開始前に次を確定する。
1. App Serverのモデル認証をChatGPT OAuthにするか、サーバー管理のAPI認証にするか。
2. GitHub操作をSSH、Fine-grained token、GitHub Appのどれで行うか。
3. libSQLのバックアップ先と保持期間。
4. PVCをlocal-pathから別ストレージへ移行する必要があるか。
5. クライアントの初期実装をWeb PWAにするか、Expoベースのモバイルアプリにするか。
6. ユーザー認証と複数ユーザー分離をPhase 1から入れるか。
7. App Serverのローカルスレッド履歴とlibSQLの会話履歴の整合性をどの時点で検証するか。
## 15. 参考資料
- Codex as a platform: https://developers.openai.com/blog/codex-as-a-platform
- Codex App Server: https://developers.openai.com/siwc/token-sharing-open-source/codex-app-server
- App Server preview limitations: https://developers.openai.com/siwc/token-sharing-open-source/preview-limitations
- Kubernetes Custom Resources: https://kubernetes.io/docs/concepts/extend-kubernetes/api-extension/custom-resources/
- Kubernetes Operator pattern: https://kubernetes.io/docs/concepts/extend-kubernetes/operator/
- libSQL server: https://github.com/tursodatabase/libsql/tree/main/libsql-server
