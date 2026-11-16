# Conversation deletion

The history sidebar exposes a per-conversation **⋯** menu on desktop and mobile. Touch/pen long-press (550 ms) also reveals the menu and selects a conversation, showing a separate **削除** button in the top bar. The send button is deliberately not repurposed: deleting a conversation is destructive and should never be confused with sending a message.

Before deleting, the browser displays a confirmation explaining that messages, runs and session workspace can be lost. A successful `DELETE /v1/conversations/:id` returns HTTP 204. A missing conversation returns HTTP 404.

Server operations:
1. Check conversation access through the repository's user-scoped lookup.
2. Delete its Kubernetes CodexSession (if the API is using a Kubernetes activator), allowing owner-reference garbage collection of Pod, Service and non-retained PVC.
3. Delete approvals, events, runs, messages, repository binding and conversation record in FK-safe order.

**DB atomicity:** libSQL deletion runs as a parameterized Hrana batch inside `BEGIN IMMEDIATE` / `COMMIT`. Each statement depends on the previous step's success; on error the batch executes `ROLLBACK`. A statement failure is reported to the API without returning raw SQL errors to the browser. If a transport failure occurs after the server may have committed, the client cannot prove the outcome; retry only after checking whether the conversation remains.\n\n**Caveats:** Kubernetes garbage collection is asynchronous. Historical CodexSessions with `sessionData.retain: true` may leave their PVCs. Kubernetes deletion and the libSQL transaction are two separate systems and **not distributed-transaction atomic**; a DB error after the session CR deletion can leave the conversation record temporarily present while its Pod has been removed. Existing live run event streams may end when their session Pod is deleted. Safeguards for concurrent sends from other clients should also be tested before release.

Changes do not affect the global ChatGPT authentication Secret.
