# Run ID propagation

The Runner API creates and stores a run before it asks the runner-agent to
start the corresponding Codex turn. App Server notifications arrive separately
on the runner-agent's JSON-RPC stdout reader, so the agent needs that run ID
when the turn starts.

The regular request path passes the ID through each layer:

```text
MessageDispatcher creates Run (run.ID)
  -> ThreadClient.StartTurn(threadID, run.ID, text)
  -> RunClient
  -> RemoteClient POST /v1/threads/{threadID}/turns
     {"text":"...","runId":"run-..."}
  -> runner-agent HTTP handler stores runId in RunTracker
```

The App Server event callback can then read the current run ID while handling
asynchronous notifications. The low-level `SessionAPI.StartTurn` remains
unchanged because it talks directly to the App Server and does not own Runner
API run metadata.
