#!/usr/bin/env bash
set -Eeuo pipefail

NAMESPACE=codex-system
TIMEOUT_SECONDS=180
BASE_URL=http://127.0.0.1:18080
forward_pid=""

diagnostics() {
  echo "::group::Kubernetes E2E diagnostic information"
  kubectl get nodes -o wide || true
  kubectl -n "$NAMESPACE" get pods,deploy,sts,svc,pvc,secret,codexsessions -o wide || true
  kubectl -n "$NAMESPACE" describe pods || true
  kubectl -n "$NAMESPACE" get events --sort-by=.lastTimestamp || true
  for workload in deploy/codex-runner-controller deploy/codex-runner-api sts/libsql; do
    kubectl -n "$NAMESPACE" logs "$workload" --all-containers --tail=200 || true
  done
  echo "::endgroup::"
}

cleanup() {
  local status=$?
  trap - EXIT
  if (( status != 0 )); then diagnostics; fi
  if [[ -n "$forward_pid" ]]; then
    kill "$forward_pid" 2>/dev/null || true
    wait "$forward_pid" 2>/dev/null || true
  fi
  exit "$status"
}
trap cleanup EXIT

wait_for_container_error() {
  local name="$1" deadline=$((SECONDS + TIMEOUT_SECONDS))
  local reason=""
  while (( SECONDS < deadline )); do
    reason="$(kubectl -n "$NAMESPACE" get pod "$name" -o jsonpath='{.status.containerStatuses[0].state.waiting.reason}' 2>/dev/null || true)"
    if [[ "$reason" == "CreateContainerConfigError" ]]; then
      kubectl -n "$NAMESPACE" describe pod "$name" |
        grep -q 'deliberately-absent-git-token'
      return 0
    fi
    sleep 3
  done
  echo "Timed out: expected CreateContainerConfigError for $name; observed: ${reason:-none}" >&2
  return 1
}

wait_for_complete_conversation() {
  local conversation_id="$1" deadline=$((SECONDS + TIMEOUT_SECONDS))
  local body
  while (( SECONDS < deadline )); do
    body="$(curl --fail --silent --show-error --max-time 10 \
      "$BASE_URL/v1/conversations/$conversation_id/messages")"
    if jq -e '
      any(.messages[]; .role == "user" and .content == "e2e-message-from-api" and .status == "completed")
      and any(.messages[]; .role == "assistant" and .content == "e2e-response-from-app-server")
    ' <<< "$body" >/dev/null; then
      return 0
    fi
    sleep 3
  done
  echo "Timed out: real Agent/App Server event was not persisted to libSQL" >&2
  return 1
}

echo "Deploying real Runner API, Controller and libSQL into Kind"
# These test overlays reuse production manifests above tests/e2e. Kustomize
# normally disallows reading outside its directory; relax that restriction
# only for this local, trusted CI test overlay.
kubectl kustomize --load-restrictor=LoadRestrictionsNone tests/e2e | kubectl apply -f -
kubectl -n "$NAMESPACE" rollout status statefulset/libsql --timeout=180s
kubectl -n "$NAMESPACE" rollout status deployment/codex-runner-controller --timeout=180s
kubectl -n "$NAMESPACE" rollout status deployment/codex-runner-api --timeout=180s

kubectl -n "$NAMESPACE" port-forward service/runner-api 18080:8080 >/tmp/e2e-port-forward.log 2>&1 &
forward_pid=$!
api_ready=false
for attempt in {1..40}; do
  if curl --fail --silent --max-time 3 "$BASE_URL/readyz" >/dev/null; then
    api_ready=true
    break
  fi
  sleep 1
done
if [[ "$api_ready" != true ]]; then
  echo "Runner API port-forward did not become ready" >&2
  cat /tmp/e2e-port-forward.log >&2 || true
  exit 1
fi

echo "Creating conversation using the REAL Runner API and libSQL"
conversation_response="$(curl --fail --silent --show-error --max-time 15 \
  -X POST -H 'Content-Type: application/json' -d '{}' "$BASE_URL/v1/conversations")"
conversation_id="$(jq -er '.id | select(type=="string" and length>0)' <<< "$conversation_response")"
echo "Created conversation $conversation_id"

echo "Sending a real message to trigger asynchronous CodexSession reconciliation"
# This request blocks until the Controller has created and readied the session
# Pod and the Runner API has handed the turn to runner-agent.
message_response="$(curl --fail --silent --show-error --max-time 120 \
  -X POST -H 'Content-Type: application/json' \
  -d '{"content":"e2e-message-from-api","clientMessageId":"e2e-message-1"}' \
  "$BASE_URL/v1/conversations/$conversation_id/messages")"
run_id="$(jq -er '.run.id | select(type=="string" and length>0)' <<< "$message_response")"
jq -e --arg id "$conversation_id" '
  .message.conversationId == $id and .message.content == "e2e-message-from-api"
' <<< "$message_response" >/dev/null

echo "Checking that this HTTP request, not a kubectl apply, created the session"
kubectl -n "$NAMESPACE" get codexsession "$conversation_id" >/dev/null
kubectl -n "$NAMESPACE" wait --for=condition=Ready "pod/$conversation_id" --timeout=120s
kubectl -n "$NAMESPACE" get pvc "$conversation_id" -o jsonpath='{.status.phase}' | grep -q '^Bound$'
kubectl -n "$NAMESPACE" get svc "$conversation_id" -o jsonpath='{.spec.ports[0].port}' | grep -q '^8081$'

echo "Checking persisted App Server events for the run through Runner API SSE"
# The real runner-agent forwards child-process notifications to Runner API,
# which persists them in libSQL. Replayed SSE events prove this end-to-end
# path; child-process stderr is not necessarily forwarded to pod logs.
deadline=$((SECONDS + 60))
events_seen=false
while (( SECONDS < deadline )); do
  # SSE is intentionally long-lived. curl's timeout (exit 28) is expected;
  # use the events it streamed before timing out.
  event_stream="$(curl --silent --show-error --max-time 3 \
    "$BASE_URL/v1/runs/$run_id/events?after=0" 2>/dev/null || true)"
  if grep -Fxq 'event: item/agentMessage/delta' <<< "$event_stream" &&
     grep -Fq 'e2e-response-from-app-server' <<< "$event_stream" &&
     grep -Fxq 'event: turn/completed' <<< "$event_stream"; then
    events_seen=true
    break
  fi
  sleep 2
done
if [[ "$events_seen" != true ]]; then
  echo "Timed out: expected App Server delta and turn/completed events for run $run_id" >&2
  echo "Observed event types:" >&2
  grep '^event: ' <<< "${event_stream:-}" >&2 || true
  exit 1
fi

echo "Checking the asynchronous App Server notification returned to Runner API and libSQL"
wait_for_complete_conversation "$conversation_id"
echo "End-to-end user message and simulated Codex response persisted (run $run_id)"

echo "Verifying that missing Kubernetes Secret fails with CreateContainerConfigError"
kubectl apply -f tests/e2e/session-missing-secret.yaml
wait_for_container_error e2e-missing-secret
echo "Kind E2E complete flow and missing Secret tests passed"
