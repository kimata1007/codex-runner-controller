# Conversation deletion

The history sidebar exposes a per-conversation **⋯** menu on desktop and mobile. Touch/pen long-press (550 ms) also reveals the menu and selects a conversation, showing a separate **削除** button in the top bar. The send button is deliberately not repurposed: deleting a conversation is destructive and should never be confused with sending a message.

Before deleting, the browser displays a confirmation explaining that messages, runs and session workspace can be lost. A successful `DELETE /v1/conversations/:id` returns HTTP 204. A missing conversation returns HTTP 404.

Server operations:
1. Check conversation access through the repository's user-scoped lookup.
2. Delete its Kubernetes CodexSession (if the API is using a Kubernetes activator), allowing owner-reference garbage collection of Pod, Service and non-retained PVC.
3. Delete approvals, events, runs, messages, repository binding and conversation record in FK-safe order.

**Caveats:** Kubernetes garbage collection is asynchronous. Historical CodexSessions with `sessionData.retain: true` may leave their PVCs. libSQL deletes are individually retryable, but are **not an atomic transaction**, so operators should be prepared for partial failure and retry; the UI will surface an error if deletion is incomplete. This needs a transactional follow-up before considering deletion production-hardened. Existing live run event streams may end when their session Pod is deleted. Safeguards for concurrent sends from other clients should also be tested before release.

Changes do not affect the global ChatGPT authentication Secret.
