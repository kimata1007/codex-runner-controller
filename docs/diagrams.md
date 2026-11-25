# Codex Runner Controller アーキテクチャ図

本書は [design.md](design.md) の目標アーキテクチャをC4 Level 1〜3、Deployment View、主要シーケンスで表現する。実装履歴は扱わない。各Levelで境界・名称・責務を維持する。通信の矢印は新規接続を開始する側から描き、同一接続の応答は省略する。mTLSはNode Proxy間で終端し、ローカル区間はCNIによって隔離する。

## C4 Level 1: System Context

```mermaid
C4Context
title Level 1: Codex Runner Platform - System Context
Person(user, "User", "会話・実装を指示し、セッションの権限を決定する")
System_Boundary(platform, "Codex Runner Platform") {
    System(client, "Mobile/Web Client", "会話、応答、承認、権限設定")
    System(runnerApi, "Runner API", "ユーザー認証、会話、キュー、実行制御")
    System(sessionRuntime, "Codex Session Runtime", "非信頼AIの起動、停止、再開")
    System(conversationData, "Conversation Data", "会話とセッション固有の状態")
    System(zeroTrust, "Zero Trust Network", "Workload Identity、mTLS、認可、通信経路の強制")
    System(externalProxies, "External Service Proxies", "資格情報を隔離して外部操作を認可")
}
System_Ext(tailscale, "Tailscale", "到達範囲を制限するネットワーク")
System_Ext(openai, "OpenAI API", "モデル推論")
System_Ext(github, "GitHub", "リポジトリとPull Request")
System_Ext(web, "Public Web", "Web検索、公開ページ、パッケージ")
Rel(user, client, "会話を操作し権限を設定")
Rel(client, tailscale, "Tailnetに接続")
Rel(tailscale, runnerApi, "通信を中継")
Rel(client, runnerApi, "ユーザー認証済みHTTPS/SSE")
Rel(runnerApi, conversationData, "会話・Runを保存")
Rel(runnerApi, sessionRuntime, "実行を開始し同一接続で結果を取得", "Zero Trust Network経由")
Rel(sessionRuntime, conversationData, "セッション専用の状態を読み書き")
Rel(runnerApi, zeroTrust, "認可されたセッション制御")
Rel(sessionRuntime, zeroTrust, "非信頼ワークロードの通信を捕捉")
Rel(zeroTrust, externalProxies, "認証・認可済み通信")
Rel(externalProxies, openai, "推論を代理")
Rel(externalProxies, github, "Git操作・PR作成/更新を代理")
Rel(externalProxies, web, "検索・取得を代理")
```

## C4 Level 2: Container View

Zero Trust Networkは全ワークロードの通信に適用される分散基盤であり、単一の中継サーバーではない。論理的な接続先を示す関係はすべて強制された経路を通る。Kubernetes API等の基盤通信は独立した認証とRBACで保護する。

```mermaid
C4Container
title Level 2: Codex Runner Platform - Container View
Person(user, "User", "会話と実行権限を操作する")
System_Ext(tailscale, "Tailscale", "到達範囲の制限")
System_Ext(openai, "OpenAI API", "モデル推論")
System_Ext(github, "GitHub", "Git・PR")
System_Ext(web, "Public Web", "検索・ページ・パッケージ")
Container_Boundary(platform, "Codex Runner Platform") {
    Container(client, "Mobile/Web Client", "Web/mobile", "会話、承認、権限設定")
    Container(runnerApi, "Runner API", "Go", "認証、会話、FIFOキュー、実行制御")
    Container_Boundary(sessionRuntime, "Codex Session Runtime") {
        Container(kubernetesApi, "Kubernetes API Server", "Kubernetes", "Pod UID・配置・ServiceAccountの正本")
        Container(controller, "Codex Runner Controller", "Go/controller-runtime", "Pod・PVC・ServiceAccount・Policyをreconcile")
        Container(sessionPod, "Session Pod", "Sandbox Runtime", "非信頼のrunner-agent、Codex、Shell")
    }
    Container_Boundary(conversationData, "Conversation Data") {
        ContainerDb(conversationStore, "Conversation Store", "Database", "会話・Message・Run・Approval")
        ContainerDb(sessionPvc, "Session PVC", "PVC", "セッション固有の履歴・ワークツリー")
    }
    Container_Boundary(zeroTrust, "Zero Trust Network") {
        Container(cni, "CNI / NetworkPolicy", "Linux networking", "通信捕捉、送信元偽装防止、迂回禁止")
        Container(nodeProxy, "Node Proxy", "Go DaemonSet", "Identity、L4認可、mTLS、監査")
        Container(issuer, "Identity Issuer", "X.509/SPIFFE", "Attestationと短命証明書発行")
        ContainerDb(policy, "Policy Store", "Controller-owned", "有効期限付き認可")
    }
    Container_Boundary(externalProxies, "External Service Proxies") {
        Container(gitProxy, "GitHub Proxy", "Go", "Repository・branch・操作認可と認証代理")
        Container(webProxy, "Web Proxy", "Go", "検索、公開Web、パッケージ取得")
        Container(modelProxy, "Model Proxy", "Go", "認証代理、モデル・予算認可")
    }
}
Rel(user, client, "操作")
Rel(client, tailscale, "Tailnet接続")
Rel(tailscale, runnerApi, "中継")
Rel(client, runnerApi, "HTTPS/SSE")
Rel(runnerApi, conversationStore, "永続化")
Rel(runnerApi, kubernetesApi, "CodexSessionを管理")
Rel(controller, kubernetesApi, "watch/reconcile")
Rel(controller, sessionPod, "ライフサイクル管理")
Rel(controller, policy, "認可情報を配布・失効")
Rel(sessionPod, sessionPvc, "専用PVCを読み書き")
Rel(runnerApi, cni, "制御接続を開始")
Rel(sessionPod, cni, "外部操作の通信を開始")
Rel(cni, nodeProxy, "信頼できるPod UID・宛先を結びつける")
Rel(nodeProxy, issuer, "認証済みノードとして証明書を要求")
Rel(issuer, kubernetesApi, "Pod UID、配置、ServiceAccountを検証")
Rel(nodeProxy, policy, "認可を検証")
Rel(nodeProxy, sessionPod, "対象Podへの制御接続を中継")
Rel(nodeProxy, gitProxy, "mTLS認証済みの通信を中継")
Rel(nodeProxy, webProxy, "mTLS認証済みの通信を中継")
Rel(nodeProxy, modelProxy, "mTLS認証済みの通信を中継")
Rel(gitProxy, github, "認可済みGit・PR操作")
Rel(webProxy, web, "公開検索・取得")
Rel(modelProxy, openai, "認可済み推論")
```

## C4 Level 3-A: Session Pod Component View

Session Pod内には管理用資格情報を置かない。runner-agentも非信頼とする。Runner APIが開始した接続に対してイベント、結果、承認要求を返す。

```mermaid
C4Component
title Level 3-A: Session Pod - Component View
Container(runnerApi, "Runner API", "Go", "実行制御の接続元")
Container(cni, "CNI / NetworkPolicy", "Linux", "強制された通信経路")
Container(nodeProxy, "Node Proxy", "Go", "認証・認可・mTLS")
ContainerDb(sessionPvc, "Session PVC", "PVC", "セッション固有の履歴とWorkspace")
Container_Boundary(sessionPod, "Session Pod — Untrusted") {
    Component(runnerAgent, "runner-agent", "Go", "制御接続の受信、App Server起動、イベント応答")
    Component(codexAppServer, "Codex App Server", "Codex", "thread/start、thread/resume、turn/start")
    Component(workspaceAdapter, "Workspace Adapter", "Git/filesystem", "ワークツリーとGitツール")
    Component(shell, "Shell / Tools", "Process", "編集、ビルド、テスト、Web操作")
}
Rel(runnerApi, cni, "対象Sessionへの接続開始")
Rel(cni, nodeProxy, "通信捕捉")
Rel(nodeProxy, runnerAgent, "認可済み制御接続")
Rel(runnerAgent, codexAppServer, "JSON-RPC over stdio")
Rel(codexAppServer, shell, "コマンドを実行")
Rel(codexAppServer, workspaceAdapter, "Git操作")
Rel(runnerAgent, sessionPvc, "履歴を読み書き")
Rel(workspaceAdapter, sessionPvc, "ワークツリーを読み書き")
Rel(workspaceAdapter, cni, "GitHub Proxyへの通信")
Rel(shell, cni, "Web Proxyへの通信")
Rel(codexAppServer, cni, "Model Proxyへの推論通信")
```

## C4 Level 3-B: Runner API Component View

```mermaid
C4Component
title Level 3-B: Runner API - Component View
Container(client, "Mobile/Web Client", "Web/mobile", "会話と権限設定")
Container(kubernetesApi, "Kubernetes API Server", "Kubernetes", "CodexSession管理")
ContainerDb(conversationStore, "Conversation Store", "Database", "会話と実行状態")
Container(nodeProxy, "Node Proxy", "Go", "認証・認可済み接続を中継")
Container(sessionPod, "Session Pod", "Sandbox", "非信頼実行")
Container_Boundary(runnerApi, "Runner API") {
    Component(authMiddleware, "Auth Middleware", "Go middleware", "ユーザー認証・所有権検証")
    Component(conversationApi, "Conversation API", "HTTP", "会話管理")
    Component(messageApi, "Message API", "HTTP", "冪等なメッセージ受付")
    Component(queueWorker, "Queue Worker", "Go", "FIFO・原子的claim")
    Component(sessionCoordinator, "Session Coordinator", "Go", "セッション状態・実行制御接続")
    Component(policyManager, "Session Policy Manager", "Go", "ユーザーが認可した権限の確定")
    Component(eventGateway, "Event Gateway", "Go/SSE", "接続上のイベントの検証と配信")
    Component(approvalApi, "Approval API", "HTTP", "承認・拒否")
    Component(store, "Conversation Repository", "Go", "永続化")
}
Rel(client, authMiddleware, "HTTPS/SSE")
Rel(authMiddleware, conversationApi, "認可済み要求")
Rel(authMiddleware, messageApi, "認可済み要求")
Rel(authMiddleware, approvalApi, "認可済み要求")
Rel(authMiddleware, policyManager, "権限の設定")
Rel(conversationApi, store, "会話を保存・取得")
Rel(messageApi, store, "Message・Runを保存")
Rel(queueWorker, store, "FIFO claim")
Rel(queueWorker, sessionCoordinator, "実行を開始")
Rel(policyManager, sessionCoordinator, "認可済み権限")
Rel(sessionCoordinator, kubernetesApi, "CodexSession更新")
Rel(sessionCoordinator, nodeProxy, "制御接続を開始")
Rel(nodeProxy, sessionPod, "認可済み制御接続")
Rel(sessionCoordinator, eventGateway, "同一接続からイベントを受信")
Rel(approvalApi, sessionCoordinator, "承認結果")
Rel(eventGateway, store, "検証済みイベントを保存")
Rel(eventGateway, client, "SSE配信")
Rel(store, conversationStore, "永続化")
```

## C4 Level 3-C: Node Proxy Component View

```mermaid
C4Component
title Level 3-C: Node Proxy - Component View
Container(sessionPod, "Session Pod", "Sandbox", "非信頼通信元")
Container(cni, "CNI / NetworkPolicy", "Linux", "Pod UIDとソケットの信頼できる結合")
Container(issuer, "Identity Issuer", "X.509/SPIFFE", "短命証明書")
ContainerDb(policy, "Policy Store", "Controller-owned", "認可情報")
Container(peer, "Destination Node Proxy", "Go", "独立した受信認可")
Container_Boundary(nodeProxy, "Node Proxy") {
    Component(interceptor, "Traffic Interceptor", "Linux integration", "元ソケット・宛先・Pod UIDを取得")
    Component(identity, "Identity Resolver", "Go", "実ワークロードとIdentityを照合")
    Component(cert, "Certificate Manager", "Go", "秘密鍵・証明書をPod外で管理")
    Component(authz, "Authorization Engine", "Go", "Identity、宛先、ポート、方向を検証")
    Component(transport, "mTLS Transport", "Go TLS 1.3", "信頼チェーンと期待URI SANを検証")
    Component(audit, "Audit Logger", "Go", "許可・拒否・失効を記録")
}
Rel(sessionPod, cni, "通信")
Rel(cni, interceptor, "強制捕捉")
Rel(interceptor, identity, "信頼できる通信元情報")
Rel(identity, authz, "検証済みIdentity")
Rel(identity, cert, "ワークロードIdentity")
Rel(cert, issuer, "AttestationとCSR")
Rel(authz, policy, "権限を取得")
Rel(authz, transport, "許可された経路のみ")
Rel(cert, transport, "短命証明書")
Rel(transport, peer, "相互認証済み通信")
Rel(authz, audit, "認可結果")
Rel(transport, audit, "接続・終了")
```

## C4 Level 3-D: External Service Proxies

各Proxyは独立した信頼境界で、認証済みの呼び出し元を用いて操作単位の認可を行う。以下の共通コンポーネントは各Proxy内で実行する。資格情報をAIへ返さない。

```mermaid
C4Component
title Level 3-D: External Service Proxies - Component View
Container(nodeProxy, "Node Proxy", "Go", "認証済みIdentityを接続に結びつける")
ContainerDb(policy, "Policy Store", "Controller-owned", "Session単位の操作権限")
System_Ext(github, "GitHub", "Git・PR")
System_Ext(web, "Public Web", "検索・取得")
System_Ext(openai, "OpenAI API", "モデル推論")
Container_Boundary(gitProxy, "GitHub Proxy") {
    Component(gitAuthz, "Operation Authorization", "Go", "Repository・ref・PR・操作を検証")
    Component(gitCredential, "GitHub App Token Provider", "Go", "短命Repository限定Token")
    Component(gitAdapter, "Git / PR Adapter", "Git HTTP/API", "認可済みGit・PRを実行")
}
Container_Boundary(webProxy, "Web Proxy") {
    Component(webAuthz, "Operation Authorization", "Go", "検索・取得・利用量を検証")
    Component(webAdapter, "Search / Fetch Adapter", "HTTPS", "公開宛先のDNS・redirect・サイズを検証")
}
Container_Boundary(modelProxy, "Model Proxy") {
    Component(modelAuthz, "Operation Authorization", "Go", "モデル・Session・予算を検証")
    Component(modelCredential, "Model Credential Provider", "Go", "信頼領域で資格情報管理")
    Component(modelAdapter, "Inference Adapter", "HTTPS/streaming", "推論を中継")
}
Rel(nodeProxy, gitAuthz, "認証済み要求")
Rel(nodeProxy, webAuthz, "認証済み要求")
Rel(nodeProxy, modelAuthz, "認証済み要求")
Rel(gitAuthz, policy, "権限検証")
Rel(webAuthz, policy, "権限検証")
Rel(modelAuthz, policy, "権限検証")
Rel(gitAuthz, gitAdapter, "認可")
Rel(gitCredential, gitAdapter, "代理認証")
Rel(gitAdapter, github, "Git・PR")
Rel(webAuthz, webAdapter, "認可")
Rel(webAdapter, web, "検索・取得")
Rel(modelAuthz, modelAdapter, "認可")
Rel(modelCredential, modelAdapter, "代理認証")
Rel(modelAdapter, openai, "推論")
```

## Deployment View: Node Proxyと隔離境界

```mermaid
flowchart TB
    subgraph K8s["Kubernetes Cluster"]
        subgraph Control["管理・セキュリティ領域"]
            API["Runner API"]
            Controller["Codex Runner Controller"]
            Kube["Kubernetes API Server"]
            Issuer["Identity Issuer"]
            Policy["Policy Store"]
            DB[("Conversation Store")]
        end
        subgraph N1["Worker Node A"]
            subgraph Untrusted["非信頼実行領域"]
                A["Session Pod A"]
                B["Session Pod B"]
                PVCa[("Session PVC A")]
                PVCb[("Session PVC B")]
                A --- PVCa
                B --- PVCb
            end
            CNIa["CNI / NetworkPolicy"]
            NPa["Node Proxy A"]
            A --- CNIa
            B --- CNIa
            CNIa --- NPa
        end
        subgraph N2["Worker Node B — 信頼領域"]
            NPb["Node Proxy B"]
            CNIb["CNI / NetworkPolicy"]
            Git["GitHub Proxy"]
            Web["Web Proxy"]
            Model["Model Proxy"]
            NPb --- CNIb
            CNIb --- Git
            CNIb --- Web
            CNIb --- Model
        end
        API --- DB
        API --> Kube
        Controller --> Kube
        Controller --> Policy
        API --> CNIb
        NPa --> Issuer
        NPb --> Issuer
        Issuer --> Kube
        NPa --> Policy
        NPb --> Policy
        NPa <-->|"TLS 1.3 / Workload Identity"| NPb
    end
    Git --> Github["GitHub"]
    Web --> Internet["Public Web"]
    Model --> OpenAI["OpenAI API"]
```

Session Pod AとBは同一Nodeでも通信不可。Node Proxyの証明書と秘密鍵は非信頼領域の外に置く。管理コンポーネントの配置は例示であり、全対象ノードにNode Proxyを配置する。Runner APIからSession Podへの接続は送信側・受信側Node Proxyを経由する。

## シーケンス: Workload Identityの発行

```mermaid
sequenceDiagram
    participant Controller
    participant Kube as Kubernetes API Server
    participant CNI as CNI / NetworkPolicy
    participant NP as Node Proxy
    participant Issuer as Identity Issuer
    participant Policy as Policy Store
    Controller->>Kube: Podと専用ServiceAccountを作成
    Kube-->>Controller: Pod UIDと配置情報
    CNI->>CNI: Pod UIDとネットワーク経路を結合し隔離
    CNI->>NP: 信頼されたワークロード情報
    NP->>NP: Pod固有の秘密鍵とCSRを生成
    NP->>Issuer: ノード認証済み接続で証明書を要求
    Issuer->>Kube: ノード配置・UID・ServiceAccountを検証
    Kube-->>Issuer: 検証対象の状態
    Issuer->>Issuer: CSR署名検証・Identity確定
    Issuer-->>NP: 短命X.509証明書
    Controller->>Policy: 有効期限付き認可を配布
    NP->>Policy: 認可取得
    Note over NP,Controller: 隔離・Identity・認可が整うまでReadyにしない
```

## シーケンス: メッセージ受付から自律実行・PR作成まで

```mermaid
sequenceDiagram
    actor User
    participant Client
    participant API as Runner API
    participant DB as Conversation Store
    participant Controller
    participant NP1 as Source Node Proxy
    participant NP2 as Destination Node Proxy
    participant Agent as runner-agent
    participant Codex as Codex App Server
    participant Model as Model Proxy
    participant Web as Web Proxy
    participant Git as GitHub Proxy
    participant GitHub
    User->>Client: 実装指示・セッション権限を確定
    Client->>API: POST message
    API->>DB: MessageとRunを保存・FIFO claim
    API->>Controller: Kubernetes API経由でSessionをActiveへ
    Controller-->>API: 隔離・Identity・認可済みReady
    API->>NP1: 対象Sessionへの制御接続
    NP1->>NP2: mTLS・送信認可
    NP2->>Agent: 受信認可済み制御接続
    Agent->>Codex: initialize / thread/start または resume / turn/start
    Note over Codex,Git: 各外部操作はCNIとNode Proxy間のmTLSを経由
    Codex->>Model: 推論要求
    Model-->>Codex: 推論ストリーム
    Codex->>Web: Web検索・ページ取得
    Web-->>Codex: 調査結果
    Codex->>Git: clone / fetch
    Git->>GitHub: Repository限定の代理認証
    GitHub-->>Git: Gitデータ
    Git-->>Codex: Gitデータ
    Codex->>Codex: 編集・ビルド・テスト・commit
    Codex->>Git: 認可済みbranchへpush・PR作成
    Git->>GitHub: 操作認可・短命Tokenで代理実行
    GitHub-->>Git: PR URL
    Git-->>Codex: PR URL
    Codex-->>Agent: イベント・完了状態
    Agent-->>NP2: 元の制御接続で結果を返す
    NP2-->>NP1: mTLS
    NP1-->>API: 元の制御接続でイベント受信
    API->>DB: 所有権・順序を検証し永続化
    API-->>Client: SSEで応答とPR URLを配信
    Client-->>User: 結果表示・PRレビュー
```

## シーケンス: 権限失効と停止

```mermaid
sequenceDiagram
    participant API as Runner API
    participant Controller
    participant Policy as Policy Store
    participant NP as Node Proxy
    participant Issuer as Identity Issuer
    participant Kube as Kubernetes API Server
    API->>Controller: Kubernetes API経由でSuspend/Delete
    Controller->>Policy: Pod UIDの認可を失効
    Policy-->>NP: 失効通知または期限切れ
    NP->>NP: 新規通信拒否・既存接続切断
    Controller->>Issuer: 発行権限を失効
    Controller->>Kube: Podを削除
    NP->>NP: ルートと秘密鍵を破棄
    Note over Controller,Kube: 必要なSession PVCと会話履歴は保持
```

## 図の整合性

- Level 1の境界はLevel 2で維持し、Level 3では対象Containerの内部を分解する。
- Node Proxyと外部サービスProxyの責務を混同しない。
- Session PodからRunner API、Conversation Store、Kubernetes API、他Session、外部ネットワークへの直接接続は許可しない。
- Session PVCへのアクセスは対象Sessionのみに限定する。
- mTLS・Identity・認可・NetworkPolicy・Sandboxは独立した防御層とする。
- C4図の表示はMermaidの対応状況に依存する。[Mermaid C4 diagrams](https://mermaid.js.org/syntax/c4.html)
