# Codex Runner Controller アーキテクチャ図
この文書は、C4モデルのLevel 1からLevel 3、および主要なメッセージ処理フローを示す。
## 図の読み方
抽象度が変わっても、次の要素は同じ名前と責務で引き継ぐ。
- Mobile/Web Client
- Runner API
- Codex Session Runtime
- Conversation Data
- Session Pod
- Codex Runner Controller
- Codex App Server
- libSQL
- Session PVC
Level 1では大きな境界だけを示す。Level 2ではその境界の内部にKubernetesのコンテナとデータストアを追加する。Level 3では、Session PodとRunner APIをそれぞれComponentへ分解する。親境界は消さず、分解対象の内部に子要素を追加する。
MermaidのC4記法は現在もexperimental扱いであり、表示環境によって対応状況が異なる可能性がある。[Mermaid C4 diagrams](https://mermaid.js.org/syntax/c4.html)
## C4 Level 1: System Context
Level 1では、ユーザー、クライアント、Runner API、セッション実行環境、会話データという製品上の大きな境界を固定する。
```mermaid
C4Context
title Level 1: Codex Runner Platform - System Context
Person(user, "User", "Tailnetへ接続して会話や実装を指示するユーザー")
System_Boundary(platform, "Codex Runner Platform") {
    System(client, "Mobile/Web Client", "会話の作成、メッセージ送信、応答表示、承認操作")
    System(runnerApi, "Runner API", "クライアントとKubernetes上のCodex実行環境を仲介するアプリケーションAPI")
    System(sessionRuntime, "Codex Session Runtime", "会話単位のCodex実行環境を起動、継続、停止する")
    System(conversationData, "Conversation Data", "会話、メッセージ、実行履歴、承認状態を保持する")
}
System_Ext(tailscale, "Tailscale", "Tailnet内の端末からプライベートに接続するネットワーク境界")
System_Ext(openai, "OpenAI API", "Codex App Serverがモデル推論に使用する外部API")
System_Ext(github, "GitHub", "実装対象のリポジトリとGitHub操作を提供する外部システム")
Rel(user, client, "会話を操作する")
Rel(client, tailscale, "Tailnet経由で接続する")
Rel(tailscale, runnerApi, "プライベートな通信を許可する")
Rel(client, runnerApi, "HTTPSとSSEで利用する")
Rel(runnerApi, sessionRuntime, "セッションを起動、再開、停止する")
Rel(runnerApi, conversationData, "メッセージと実行状態を保存する")
Rel(sessionRuntime, conversationData, "セッション状態を参照する")
Rel(sessionRuntime, openai, "Codex推論を実行する")
Rel(sessionRuntime, github, "リポジトリを読み書きする")
```
## C4 Level 2: Container
Level 2では、Level 1の4つの主要な枠を維持し、その内部をKubernetesの実行単位へ分解する。
- Codex Session Runtime
  - Kubernetes API Server
  - Codex Runner Controller
  - Session Pod
- Conversation Data
  - libSQL
  - Session PVC
```mermaid
C4Container
title Level 2: Codex Runner Platform - Container View
Person(user, "User", "Tailnetへ接続して会話や実装を指示するユーザー")
System_Ext(tailscale, "Tailscale", "Tailnet内のプライベートネットワーク")
System_Ext(openai, "OpenAI API", "モデル推論を提供する外部API")
System_Ext(github, "GitHub", "実装対象リポジトリを提供する外部システム")
Container_Boundary(platform, "Codex Runner Platform") {
    Container(client, "Mobile/Web Client", "Web or mobile client", "会話、ストリーミング表示、承認操作を提供する")
    Container(runnerApi, "Runner API", "Application server", "メッセージを受け付け、会話を保存し、CodexSessionを管理する")
    Container_Boundary(sessionRuntime, "Codex Session Runtime") {
        Container(kubernetesApi, "Kubernetes API Server", "Kubernetes control plane", "CodexSession、Pod、PVCを管理するAPI")
        Container(controller, "Codex Runner Controller", "Go, controller-runtime", "CodexSessionを監視してセッションPodをreconcileする")
        Container(sessionPod, "Session Pod", "Kubernetes Pod", "runner-agentとCodex App Serverを会話単位で実行する")
    }
    Container_Boundary(conversationData, "Conversation Data") {
        ContainerDb(libsql, "libSQL", "sqld, HTTP", "会話、メッセージ、Run、Approvalを永続化する")
        ContainerDb(sessionPvc, "Session PVC", "Kubernetes PersistentVolumeClaim", "Codexのローカル履歴、CODEX_HOME、実装ワークツリーを保持する")
    }
}
Rel(user, client, "会話を操作する")
Rel(client, tailscale, "Tailnetへ接続する")
Rel(tailscale, runnerApi, "プライベート通信を許可する")
Rel(client, runnerApi, "HTTPSとSSE")
Rel(runnerApi, libsql, "会話とメッセージを読み書きする")
Rel(runnerApi, kubernetesApi, "CodexSessionを作成、更新する")
Rel(kubernetesApi, controller, "watchイベントを通知する")
Rel(controller, kubernetesApi, "PodとPVCをreconcileする")
Rel(controller, sessionPod, "Session Podを作成、削除する")
Rel(sessionPod, runnerApi, "コマンド取得とイベント送信")
Rel(sessionPod, sessionPvc, "履歴とワークスペースを保持する")
Rel(sessionPod, openai, "Codex推論を実行する")
Rel(sessionPod, github, "リポジトリを取得、更新する")
```
## C4 Level 3-A: Session Pod Component View
Level 3-Aは、Level 2のSession Podという枠を維持し、その内部をComponentへ分解する。
- Session Pod
  - runner-agent
  - Codex App Server
  - Workspace Adapter
runner-agentとCodex App Serverは初期設計では同じコンテナ内のプロセスである。Workspace Adapterは、Git操作とワークスペース準備を担当する論理Componentとして表現する。
```mermaid
C4Component
title Level 3-A: Session Pod - Component View
Person(user, "User", "会話や実装を指示するユーザー")
Container(client, "Mobile/Web Client", "Web or mobile client", "会話画面")
Container(runnerApi, "Runner API", "Application server", "コマンドキューとイベントAPI")
Container(controller, "Codex Runner Controller", "Go, controller-runtime", "Session Podのライフサイクルを管理する")
ContainerDb(libsql, "libSQL", "sqld, HTTP", "会話と実行履歴")
ContainerDb(sessionPvc, "Session PVC", "PersistentVolumeClaim", "Codex履歴とワークスペース")
System_Ext(openai, "OpenAI API", "モデル推論を提供する")
System_Ext(github, "GitHub", "リポジトリを提供する")
Container_Boundary(sessionPod, "Session Pod") {
    Component(runnerAgent, "runner-agent", "Long-running process", "App Serverを起動し、コマンド、イベント、heartbeat、承認を仲介する")
    Component(codexAppServer, "Codex App Server", "Codex process", "thread/start、thread/resume、turn/start、ストリーミングを実行する")
    Component(workspaceAdapter, "Workspace Adapter", "Git and filesystem adapter", "リポジトリと実装ワークツリーを準備する")
}
Rel(user, client, "会話を操作する")
Rel(client, runnerApi, "HTTPSとSSE")
Rel(runnerApi, runnerAgent, "コマンドを渡しイベントを受け取る")
Rel(controller, runnerAgent, "Podとして起動、停止する")
Rel(runnerAgent, codexAppServer, "JSON-RPC over stdio")
Rel(runnerAgent, sessionPvc, "CODEX_HOMEを読む、書く")
Rel(workspaceAdapter, sessionPvc, "ワークツリーを読む、書く")
Rel(workspaceAdapter, github, "clone、fetch、pushを実行する")
Rel(codexAppServer, openai, "Responses APIへ推論を依頼する")
Rel(runnerAgent, libsql, "実行状態を直接保存しない")
```
## C4 Level 3-B: Runner API Component View
Level 3-Bは、Level 2のRunner APIという枠を維持し、その内部をComponentへ分解する。Session Podは外部のコンテキストとして残す。
- Runner API
  - Auth Middleware
  - Conversation API
  - Message API
  - Session Coordinator
  - Event Gateway
  - Approval API
  - Conversation Store
```mermaid
C4Component
title Level 3-B: Runner API - Component View
Person(user, "User", "Tailnetへ接続して利用するユーザー")
Container(client, "Mobile/Web Client", "Web or mobile client", "会話画面")
Container(controller, "Codex Runner Controller", "Go, controller-runtime", "CodexSessionをreconcileする")
Container(sessionPod, "Session Pod", "Kubernetes Pod", "Codex App Serverを実行する")
ContainerDb(libsql, "libSQL", "sqld, HTTP", "会話と実行履歴を保存する")
Container(kubernetesApi, "Kubernetes API Server", "Kubernetes control plane", "CodexSessionを管理する")
Container_Boundary(runnerApi, "Runner API") {
    Component(authMiddleware, "Auth Middleware", "HTTP middleware", "Tailscale内からのアプリケーション要求を認証、認可する")
    Component(conversationApi, "Conversation API", "HTTP API", "会話の作成と履歴取得を受け付ける")
    Component(messageApi, "Message API", "HTTP API", "ユーザーメッセージを冪等に受け付ける")
    Component(sessionCoordinator, "Session Coordinator", "Application service", "CodexSessionのdesiredStateとRunを管理する")
    Component(eventGateway, "Event Gateway", "SSE and internal API", "runner-agentのイベントをクライアントへ配信する")
    Component(approvalApi, "Approval API", "HTTP API", "承認要求への回答を受け付ける")
    Component(conversationStore, "Conversation Store", "libSQL repository", "Conversation、Message、Run、Approvalを保存する")
}
Rel(user, client, "会話を操作する")
Rel(client, authMiddleware, "HTTPSとSSE")
Rel(authMiddleware, conversationApi, "認証済み要求")
Rel(authMiddleware, messageApi, "認証済み要求")
Rel(authMiddleware, approvalApi, "認証済み要求")
Rel(conversationApi, conversationStore, "会話を読む、書く")
Rel(messageApi, conversationStore, "MessageとRunを保存する")
Rel(messageApi, sessionCoordinator, "セッションを起動する")
Rel(sessionCoordinator, kubernetesApi, "CodexSessionを作成、更新する")
Rel(sessionCoordinator, conversationStore, "Run状態を更新する")
Rel(sessionPod, eventGateway, "イベントとheartbeatを送信する")
Rel(eventGateway, conversationStore, "イベントと応答を保存する")
Rel(eventGateway, client, "SSEでストリーミングする")
Rel(approvalApi, sessionCoordinator, "承認結果を渡す")
Rel(controller, sessionPod, "Podを作成、削除する")
```
## シーケンス図: メッセージ受付からCodex応答まで
このフローでは、Session Podが存在しない場合はControllerが作成し、存在する場合は再利用する。App Serverのthread/startとthread/resumeは、runner-agentが保存済みの状態を見て選択する。
```mermaid
sequenceDiagram
    title Message processing and session activation
    actor User
    participant Client
    participant Tailscale
    participant RunnerAPI
    participant LibSQL
    participant KubernetesAPI
    participant Controller
    participant Kubelet
    participant SessionPod
    participant RunnerAgent
    participant AppServer
    participant OpenAI
    User->>Client: メッセージを入力する
    Client->>Tailscale: Tailnet経由で接続する
    Tailscale-->>Client: アクセスを許可する
    Client->>RunnerAPI: POST message
    RunnerAPI->>LibSQL: MessageとRunを保存する
    RunnerAPI->>KubernetesAPI: CodexSessionをActiveへ更新する
    KubernetesAPI-->>Controller: CodexSession変更を通知する
    Controller->>KubernetesAPI: Session Podを作成または再利用する
    KubernetesAPI-->>Kubelet: Podの望ましい状態を伝える
    Kubelet->>SessionPod: Podを起動する
    SessionPod->>RunnerAgent: runner-agentを起動する
    RunnerAgent->>AppServer: initializeとthread/startまたはthread/resume
    AppServer-->>RunnerAgent: thread ready
    RunnerAgent->>RunnerAPI: pending commandを取得する
    RunnerAPI-->>RunnerAgent: messageとRun IDを返す
    RunnerAgent->>AppServer: turn/start
    AppServer->>OpenAI: Responses APIへ推論を依頼する
    OpenAI-->>AppServer: ストリーミングイベント
    AppServer-->>RunnerAgent: agentMessage deltaとturn completed
    RunnerAgent-->>RunnerAPI: イベントと完了状態を送信する
    RunnerAPI->>LibSQL: assistant messageとRunを完了する
    RunnerAPI-->>Client: SSEで応答を配信する
    Client-->>User: 応答を表示する
```
## 対応関係のルール
- Level 1の枠はLevel 2でも残す。
- Level 2で追加するKubernetesリソースは、Level 1のCodex Session Runtimeの内部に置く。
- Level 2で追加する永続化リソースは、Level 1のConversation Dataの内部に置く。
- Level 3では、分解対象の枠を残したままComponentを追加する。
- Session PodとRunner APIは責務が異なるため、Level 3を2枚に分ける。
- 名前を変更して別の要素に見せない。責務を変更する場合は設計変更として記録する。
- MermaidのC4図でレイアウトが崩れる場合も、要素を削除せず、境界の宣言順と関係の向きを調整する。

