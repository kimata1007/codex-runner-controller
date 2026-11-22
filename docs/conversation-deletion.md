# Conversation archiving

The history sidebar exposes an archive action from each conversation menu on desktop and mobile. Touch/pen long-press (550 ms) also selects a conversation and reveals an **アーカイブ** action in the top bar. Archiving keeps the message history in the database and removes the conversation's Kubernetes session resources.

Before archiving, the browser confirms that history will remain while session workspace resources are removed. A successful `DELETE /v1/conversations/:id` returns HTTP 204. The endpoint keeps its existing path and method for compatibility, but it archives the conversation instead of permanently deleting it. A missing conversation returns HTTP 404.

Server operations:
1. Check conversation access through the repository's user-scoped lookup.
2. Delete its Kubernetes CodexSession, allowing owner-reference garbage collection of Pod, Service and non-retained PVC.
3. Update the conversation status to `archived`; messages, runs, events, approvals and repository binding remain in history.

The Kubernetes deletion and database status update cannot share one atomic transaction because they are separate systems. If session deletion succeeds and the status update fails, the conversation remains in the database and the same request can be retried. The database status update is a single owner-scoped statement. Kubernetes garbage collection is asynchronous; retained PVCs may remain, and PV reclaim policy may retain storage after a PVC is removed.

Changes do not affect the global ChatGPT authentication Secret.
