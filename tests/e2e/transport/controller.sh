#!/usr/bin/env bash
set -euo pipefail

if [[ "$(kubectl config current-context)" != kind-codex-identity ]]; then
  echo "The Session lifecycle test requires the kind-codex-identity context." >&2
  exit 1
fi
cd "$(dirname "$0")/../../.."

# The transport fixture owns its policy. Remove it before starting the controller.
kubectl -n codex-sessions delete codexsession transport-e2e issuer-e2e --ignore-not-found --wait=true
kubectl kustomize config/sandbox-controller |
  sed -e 's|ghcr.io/kimata1007/codex-runner-controller:v[0-9.]*|local/runner-controller:e2e|' \
      -e 's|ghcr.io/kimata1007/codex-runner-agent:v[0-9.]*|local/runner-agent:e2e|' |
  kubectl apply -f -
kubectl -n codex-system rollout status deployment/codex-sandbox-controller --timeout=120s

cat <<'EOF' | kubectl apply -f -
apiVersion: codexrunner.io/v1alpha1
kind: CodexSession
metadata: {name: sandbox-e2e, namespace: codex-sessions}
spec:
  conversationId: sandbox-e2e
  desiredState: Active
  sessionData: {size: 1Gi, retain: false}
  execution: {approvalPolicy: never}
EOF

session() { kubectl -n codex-sessions "$@"; }
security() { kubectl -n codex-security "$@"; }
session wait --for=create pod/sandbox-e2e --timeout=100s
session wait --for=jsonpath='{.status.phase}'=Running pod/sandbox-e2e --timeout=100s
pod_uid="$(session get pod sandbox-e2e -o jsonpath='{.metadata.uid}')"
security wait --for=create "sessionpolicy/$pod_uid" --timeout=100s
session wait --for=condition=RouteReady=false codexsession/sandbox-e2e --timeout=15s

kubectl kustomize config/node-proxy |
  sed 's|image: ghcr.io/kimata1007/codex-node-proxy:[^ ]*|image: local/codex-node-proxy:e2e|' |
  kubectl apply -f -
security rollout status daemonset/codex-node-proxy --timeout=120s
session wait --for=condition=RouteReady codexsession/sandbox-e2e --timeout=120s
session wait --for=jsonpath='{.status.phase}'=Ready codexsession/sandbox-e2e --timeout=15s
session get pod sandbox-e2e -o json | jq -e '
  .spec.runtimeClassName == "codex-gvisor" and
  .spec.automountServiceAccountToken == false and
  any(.status.conditions[]; .type == "codexrunner.io/RouteReady" and .status == "True")
' >/dev/null

session patch codexsession sandbox-e2e --type merge -p '{"spec":{"desiredState":"Suspended"}}'
session wait --for=condition=RouteReady=false codexsession/sandbox-e2e --timeout=30s
session wait --for=delete pod/sandbox-e2e --timeout=100s
security wait --for=delete "sessionpolicy/$pod_uid" --timeout=30s

session patch codexsession sandbox-e2e --type merge -p '{"spec":{"desiredState":"Active"}}'
session wait --for=condition=RouteReady codexsession/sandbox-e2e --timeout=120s
replacement_uid="$(session get pod sandbox-e2e -o jsonpath='{.metadata.uid}')"
[[ "$replacement_uid" != "$pod_uid" ]]
security get "sessionpolicy/$replacement_uid" >/dev/null
kubectl -n codex-system rollout restart deployment/codex-sandbox-controller
kubectl -n codex-system rollout status deployment/codex-sandbox-controller --timeout=120s
[[ "$(session get pod sandbox-e2e -o jsonpath='{.metadata.uid}')" == "$replacement_uid" ]]
session wait --for=condition=RouteReady codexsession/sandbox-e2e --timeout=30s

security delete daemonset codex-node-proxy --wait=true --timeout=90s
session wait --for=condition=RouteReady=false codexsession/sandbox-e2e --timeout=40s
session delete codexsession sandbox-e2e --wait=true --timeout=100s
security wait --for=delete "sessionpolicy/$replacement_uid" --timeout=20s
echo "Session readiness, suspension, replacement, and route expiry passed."
