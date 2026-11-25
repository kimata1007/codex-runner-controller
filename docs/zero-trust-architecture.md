# Zero Trust AI execution — target architecture

Status: proposed implementation, not an enabled production security boundary.

The Session Pod is hostile, including runner-agent, Codex, shell, workspace and all
responses from the pod. No reusable credentials, Kubernetes tokens, host mounts
or shared writable volumes may be exposed to it. Runner API initiates control
connections; results flow back on those connections.

## C4 Level 1
```mermaid
C4Context
title Zero Trust Codex Runner Platform
Person(user, "User", "Requests autonomous implementation")
System_Boundary(platform, "Codex Runner Platform") {
  System(client, "Mobile/Web Client", "Conversation UI")
  System(api, "Runner API", "Conversation and session authorization")
  System(runtime, "Codex Session Runtime", "Untrusted execution")
  System(data, "Conversation Data", "History and workspace")
  System(security, "Zero Trust Network", "Workload authentication and network authorization")
  System(proxies, "External Service Proxies", "Operation authorization and credential isolation")
}
System_Ext(github, "GitHub", "Repositories and pull requests")
System_Ext(model, "OpenAI", "Model inference")
System_Ext(web, "Public Web", "Search and fetch")
Rel(user, client, "Uses")
Rel(client, api, "HTTPS/SSE")
Rel(api, data, "Persists")
Rel(api, runtime, "Initiates control via Zero Trust Network")
Rel(runtime, security, "Uses enforced network path")
Rel(security, proxies, "Authenticated authorized requests")
Rel(proxies, github, "Git and PR")
Rel(proxies, model, "Inference")
Rel(proxies, web, "Search and fetch")
```

## C4 Level 2
```mermaid
C4Container
title Zero Trust container view
Container(client, "Mobile/Web Client", "Web", "Conversation UI")
Container_Boundary(platform, "Codex Runner Platform") {
  Container(api, "Runner API", "Go", "Queue and session control")
  Container_Boundary(runtime, "Codex Session Runtime") {
    Container(controller, "Codex Runner Controller", "Go", "Reconciles pods and policies")
    Container(kube, "Kubernetes API", "Kubernetes", "Authoritative workload metadata")
    Container(session, "Session Pod", "Sandbox runtime", "Untrusted Codex and shell")
  }
  Container_Boundary(data, "Conversation Data") {
    ContainerDb(db, "Conversation Store", "Database", "Conversation and runs")
    ContainerDb(pvc, "Session PVC", "PVC", "Untrusted workspace")
  }
  Container_Boundary(security, "Zero Trust Network") {
    Container(cni, "CNI", "Linux networking", "Enforces interception and anti-spoofing")
    Container(node, "Node Proxy", "Go DaemonSet", "Per-workload mTLS and network policy")
    Container(ca, "Identity Issuer", "SPIRE or equivalent", "Attested short-lived X.509 identities")
    Container(policy, "Policy Store", "Controller-owned configuration", "Default-deny grants")
  }
  Container_Boundary(proxies, "External Service Proxies") {
    Container(git, "GitHub Proxy", "Go", "Repository and branch authorization")
    Container(model, "Model Proxy", "Go", "Model and budget authorization")
    Container(web, "Web Proxy", "Go", "Public search and safe fetch")
  }
}
Rel(client, api, "HTTPS/SSE")
Rel(api, db, "Persists")
Rel(api, kube, "Manages sessions")
Rel(controller, kube, "Reconciles")
Rel(controller, policy, "Publishes grants")
Rel(session, pvc, "Reads/writes")
Rel(session, cni, "Untrusted network traffic")
Rel(cni, node, "Attested source and enforced path")
Rel(node, ca, "Obtains per-workload certificates")
Rel(node, policy, "Reads grants")
Rel(node, git, "mTLS with workload identity")
Rel(node, model, "mTLS with workload identity")
Rel(node, web, "mTLS with workload identity")
```

## C4 Level 3 — Node Proxy
```mermaid
C4Component
title Node Proxy components
Container(session, "Session Pod", "Sandbox", "Untrusted")
Container(cni, "CNI", "Linux", "Trusted interception")
Container_Boundary(node, "Node Proxy") {
  Component(intercept, "Traffic Interceptor", "Linux integration", "Authenticated source binding")
  Component(resolve, "Identity Resolver", "Go", "Resolve live Pod UID and node placement")
  Component(policy, "Authorization Engine", "Go", "Exact source, destination, port grants")
  Component(cert, "Certificate Manager", "Go", "Per-workload short-lived credentials")
  Component(tls, "mTLS Transport", "Go TLS", "Mutual identity verification")
  Component(audit, "Audit Logger", "Go", "Decision metadata without secrets")
}
Container(peer, "Destination Node Proxy", "Go", "Inbound authorization")
Rel(session, cni, "Traffic")
Rel(cni, intercept, "Trusted source binding")
Rel(intercept, resolve, "Resolve")
Rel(resolve, policy, "Authorize")
Rel(policy, tls, "Allowed traffic only")
Rel(cert, tls, "Credentials")
Rel(policy, audit, "Decision")
Rel(tls, peer, "mTLS")
```

## Security invariants

- Identity is a canonical SPIFFE URI, not a caller-supplied header or an IP.
  Session identities include a non-reusable Pod UID. The issuer must attest node
  placement and workload ownership; a node may not request arbitrary identities.
- TLS 1.3, verified chain, exact URI SAN, proof of key possession. The destination
  independently authorizes the source. Private keys stay outside execution pods.
- Explicit deny by default, even during startup, stale policy, or proxy failure.
  CNI must enforce ingress and egress including same-node, host, IPv6, metadata,
  DNS and direct-IP traffic. Kubernetes NetworkPolicy alone is insufficient.
- Policy changes and pod deletion must revoke active connections within a bounded
  interval. No session can write policy or obtain another session identity.
- GitHub grants are per repository, operation and session-owned branch; no merge
  or protected-branch push. GitHub Actions and untrusted PR workflows must not
  expose privileged credentials. Web search/fetch permits data exfiltration via
  URLs and queries; explicit user acceptance of this residual risk is required.
- Model credentials live outside session pods; API-key and ChatGPT login modes
  are distinct and compatibility must be tested.
- Treat the node, CNI, issuer, controller and external proxies as trusted computing
  base; node compromise is outside the session isolation guarantee.

## Activation gates

Do not enable production sessions on this design until the CNI binding,
certificate issuance/rotation, revocation, policy distribution, inbound/outbound
interception, controller integration, sandbox runtime and external proxy
authorization are implemented and adversarial multi-node tests pass.
Existing deployments remain unchanged while this is developed.
