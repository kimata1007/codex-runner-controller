#!/usr/bin/env bash
set -euo pipefail

if [[ "$(kubectl config current-context)" != kind-codex-identity ]]; then
  echo "The control stream test requires the kind-codex-identity context." >&2
  exit 1
fi
cd "$(dirname "$0")/../../.."

kubectl -n codex-system delete deployment codex-runner-api --ignore-not-found --wait=true
kubectl -n codex-system apply -k tests/e2e/libsql
kubectl -n codex-system rollout status statefulset/libsql --timeout=120s
kubectl kustomize config/node-proxy |
  sed 's|image: ghcr.io/kimata1007/codex-node-proxy:[^ ]*|image: local/codex-node-proxy:e2e|' |
  kubectl apply -f -
kubectl -n codex-security rollout status daemonset/codex-node-proxy --timeout=120s
kubectl kustomize config/isolated-api |
  sed 's|image: ghcr.io/kimata1007/codex-runner-api:[^ ]*|image: local/runner-api:e2e|' |
  kubectl apply -f -
kubectl -n codex-system rollout status deployment/codex-runner-api --timeout=120s

work_dir="$(mktemp -d)"
kubectl -n codex-system port-forward --address 127.0.0.1 svc/runner-api :8080 >"$work_dir/forward.log" 2>&1 &
forward_pid=$!
cleanup() {
  kill "$forward_pid" 2>/dev/null || true
  wait "$forward_pid" 2>/dev/null || true
  rm -rf "$work_dir"
}
trap cleanup EXIT
port=
for ((attempt = 0; attempt < 30; attempt++)); do
  port="$(sed -n 's/^Forwarding from 127\.0\.0\.1:\([0-9][0-9]*\) -> 8080.*/\1/p' "$work_dir/forward.log")"
  [[ -z "$port" ]] || break
  kill -0 "$forward_pid"
  sleep 1
done
[[ -n "$port" ]]
api="http://127.0.0.1:$port"
conversation="$(curl -fsS --max-time 15 -H 'Content-Type: application/json' -d '{}' "$api/v1/conversations" | jq -er .id)"
message="$(curl -fsS --max-time 120 -H 'Content-Type: application/json' \
  -d '{"content":"e2e-message-from-api","clientMessageId":"control-e2e-1"}' "$api/v1/conversations/$conversation/messages")"
run="$(jq -er '.run.id' <<< "$message")"
deadline=$((SECONDS + 40))
until events="$(curl -sS --max-time 2 "$api/v1/runs/$run/events" 2>/dev/null || true)" &&
  grep -Fxq 'event: turn/completed' <<< "$events"; do
  if (( SECONDS >= deadline )); then
    echo "Expected the terminal event on the Runner API SSE stream" >&2
    exit 1
  fi
  sleep 1
done
grep -Fxq 'id: 1' <<< "$events"
grep -Fxq 'id: 2' <<< "$events"
grep -Fxq 'event: item/agentMessage/delta' <<< "$events"
grep -Fq 'e2e-response-from-app-server' <<< "$events"
curl -fsS --max-time 15 "$api/v1/conversations/$conversation/messages" |
  jq -e 'any(.messages[]; .role == "assistant" and .content == "e2e-response-from-app-server")' >/dev/null
[[ -z "$(kubectl -n codex-sessions get service "$conversation" --ignore-not-found -o name)" ]]
[[ "$(curl -sS --max-time 10 -o /dev/null -w '%{http_code}' -X POST "$api/internal/runs/$run/events")" == 404 ]]

second="$(curl -fsS --max-time 120 -H 'Content-Type: application/json' \
  -d '{"content":"e2e-hold-for-interrupt","clientMessageId":"control-e2e-2"}' "$api/v1/conversations/$conversation/messages")"
run="$(jq -er '.run.id' <<< "$second")"
deadline=$((SECONDS + 40))
until curl -fsS --max-time 5 "$api/v1/conversations/$conversation/approvals" |
  jq -e --arg run "$run" 'any(.approvals[]; .runId == $run and .requestId == "9001")' >/dev/null; do
  if (( SECONDS >= deadline )); then
    echo "Expected a persisted approval request for the second run" >&2
    exit 1
  fi
  sleep 1
done
curl -fsS --max-time 20 -H 'Content-Type: application/json' -d '{"response":{"decision":"accept"}}' \
  "$api/v1/approvals/$run/9001" >/dev/null
deadline=$((SECONDS + 40))
until events="$(curl -sS --max-time 2 "$api/v1/runs/$run/events" 2>/dev/null || true)" &&
  grep -Fq 'e2e-approval-received' <<< "$events"; do
  if (( SECONDS >= deadline )); then
    echo "Expected the App Server to receive the approval response" >&2
    exit 1
  fi
  sleep 1
done
curl -fsS --max-time 20 -X POST "$api/v1/runs/$run/interrupt" >/dev/null
deadline=$((SECONDS + 40))
until events="$(curl -sS --max-time 2 "$api/v1/runs/$run/events" 2>/dev/null || true)" &&
  grep -Fq '"status":"interrupted"' <<< "$events"; do
  if (( SECONDS >= deadline )); then
    echo "Expected the App Server to interrupt the second run" >&2
    exit 1
  fi
  sleep 1
done
echo "Commands, ordered SSE events, approvals and interrupts used the authenticated control stream."
