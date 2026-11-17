#!/usr/bin/env bash
set -Eeuo pipefail

NAMESPACE=codex-system
TIMEOUT_SECONDS=180

diagnostics() {
  echo "::group::Kubernetes diagnostic information"
  kubectl get nodes -o wide || true
  kubectl -n "$NAMESPACE" get pods,deploy,sts,svc,pvc,secret,csession -o wide || true
  kubectl -n "$NAMESPACE" describe pods || true
  kubectl -n "$NAMESPACE" get events --sort-by=.lastTimestamp || true
  kubectl -n "$NAMESPACE" logs deploy/codex-runner-controller --all-containers --tail=200 || true
  kubectl -n "$NAMESPACE" logs deploy/codex-runner-api --all-containers --tail=200 || true
  kubectl -n "$NAMESPACE" logs sts/libsql --all-containers --tail=200 || true
  echo "::endgroup::"
}

# Always collect diagnostics after a failing step. A CI upload step persists
# them separately, even when a command fails before reaching the assertions.
trap 'status=$?; if (( status != 0 )); then diagnostics; fi' EXIT

wait_for_phase() {
  local name="$1" expected="$2" deadline=$((SECONDS + TIMEOUT_SECONDS))
  while (( SECONDS < deadline )); do
    phase="$(kubectl -n "$NAMESPACE" get codexsession "$name" -o jsonpath='{.status.phase}' 2>/dev/null || true)"
    if [[ "$phase" == "$expected" ]]; then
      return 0
    fi
    sleep 3
  done
  echo "Timed out: CodexSession/$name did not reach phase $expected" >&2
  return 1
}

wait_for_container_error() {
  local name="$1" deadline=$((SECONDS + TIMEOUT_SECONDS))
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

echo "Checking real controller and API Deployments in Kind"
kubectl apply -k tests/e2e
kubectl -n "$NAMESPACE" rollout status deployment/codex-runner-controller --timeout=180s
kubectl -n "$NAMESPACE" rollout status statefulset/libsql --timeout=180s
kubectl -n "$NAMESPACE" rollout status deployment/codex-runner-api --timeout=180s

echo "Checking real API -> libSQL conversation creation via Service"
kubectl -n "$NAMESPACE" port-forward service/runner-api 18080:8080 >/tmp/e2e-port-forward.log 2>&1 &
forward_pid=$!
trap 'kill "$forward_pid" 2>/dev/null || true' EXIT
for attempt in {1..30}; do
  if curl --fail --silent http://127.0.0.1:18080/readyz >/dev/null; then
    break
  fi
  sleep 1
done
curl --fail --silent --show-error \
  -X POST -H 'Content-Type: application/json' \
  -d '{}' http://127.0.0.1:18080/v1/conversations \
  | grep -q '"id"'
kill "$forward_pid" 2>/dev/null || true
wait "$forward_pid" 2>/dev/null || true
trap 'status=$?; if (( status != 0 )); then diagnostics; fi' EXIT

echo "Checking controller creates and readies the session Pod, PVC and Service"
kubectl apply -f tests/e2e/session-ready.yaml
wait_for_phase e2e-ready Ready
kubectl -n "$NAMESPACE" wait --for=condition=Ready pod/e2e-ready --timeout=120s
kubectl -n "$NAMESPACE" get pvc e2e-ready -o jsonpath='{.status.phase}' | grep -q Bound
kubectl -n "$NAMESPACE" get svc e2e-ready -o jsonpath='{.spec.ports[0].port}' | grep -q '^8081$'

echo "Checking that a missing Secret is surfaced as CreateContainerConfigError"
kubectl apply -f tests/e2e/session-missing-secret.yaml
wait_for_container_error e2e-missing-secret
echo "Kind E2E smoke checks passed"
