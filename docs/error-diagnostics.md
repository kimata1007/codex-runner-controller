# Production error diagnostics

Operational failures in Runner API and runner-agent return a **diagnostic ID** to
the browser. Find the matching entry in the component's server logs:

```text
diagnostic_id=<24-character hex value> operation=run_start category=jsonrpc_-32602
```

The server MUST NOT log raw `error.Error()` content. It may contain ChatGPT
access/refresh tokens, Authorization headers, private repository URLs, file
paths, and user prompts. Masking tokens by regular expression is not a
sufficient security boundary; report only hard-coded operation names and
allowlisted categories. The diagnostics package extracts only numeric protocol
codes (HTTP and JSON-RPC) and uses fixed error categories for all other cases.

Correlate across layers using the diagnostic ID in the returned error message.
Each layer records its own diagnostic ID; do not assume IDs are shared across
services. Check the Runner API and session agent logs for the corresponding
failure stage.

Examples:

- `session_activate`: Kubernetes session Pod readiness/activation failed.
- `run_start`: dispatch, token injection, thread start/resume or turn start failed.
- `app_server_request`: agent-to-App-Server JSON-RPC request failed.
- `auth_refresh`: refreshing account tokens failed.
- `event_persist` and `event_projection`: App Server event persistence failed.
- `repository`: underlying conversation/database operation failed.

The browser never receives the original upstream error string.
Even in server logs, the exact cause is deliberately unavailable; use
structured component health, Pod states and numeric status codes to diagnose.

Do not add full request or response body logging, HTTP Authorization headers,
credential values, raw App Server notification fields, or raw errors.
