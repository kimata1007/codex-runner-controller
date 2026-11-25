#!/usr/bin/env bash
set -euo pipefail
umask 077

if [[ "$(kubectl config current-context)" != kind-codex-identity ]]; then
  echo "The transport tests require the kind-codex-identity context." >&2
  exit 1
fi
cd "$(dirname "$0")/../../.."

kubectl create namespace codex-system --dry-run=client -o yaml | kubectl apply -f -
kubectl -n codex-system apply -f config/rbac/identity-issuer-reader.yaml
kubectl -n codex-security delete daemonset codex-node-proxy --ignore-not-found --wait=true --timeout=90s
kubectl -n codex-security get secret codex-identity-issuer-tls -o json |
  jq -r '.data["tls.crt"] | @base64d' |
  kubectl -n codex-security create configmap codex-identity-issuer-trust --from-file=ca.crt=/dev/stdin --dry-run=client -o yaml |
  kubectl apply -f -
kubectl kustomize config/node-proxy |
  sed 's|image: ghcr.io/kimata1007/codex-node-proxy:[^ ]*|image: local/codex-node-proxy:e2e|' |
  kubectl apply -f -
kubectl -n codex-security rollout status daemonset/codex-node-proxy --timeout=120s

nodes="$(kubectl get nodes -l '!node-role.kubernetes.io/control-plane' -o json | jq -r '[.items[].metadata.name] | sort | join(" ")')"
read -r source_node sink_node _ <<<"$nodes"
[[ -n "$source_node" && -n "$sink_node" ]]
node_uid="$(kubectl get node "$source_node" -o jsonpath='{.metadata.uid}')"
node_ip="$(kubectl get node "$source_node" -o json | jq -r '.status.addresses[] | select(.type == "InternalIP") | .address')"
api_ip="$(kubectl -n default get service kubernetes -o jsonpath='{.spec.clusterIP}')"

for role in runner model; do
  name=codex-runner-api
  namespace=codex-system
  if [[ "$role" == model ]]; then
    name=codex-model-proxy
    namespace=codex-security
  fi
  cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: ServiceAccount
metadata: {name: $name, namespace: $namespace}
automountServiceAccountToken: false
---
apiVersion: apps/v1
kind: Deployment
metadata: {name: $name, namespace: $namespace}
spec:
  selector:
    matchLabels: {app.kubernetes.io/name: $name}
  template:
    metadata:
      labels: {app.kubernetes.io/name: $name}
    spec:
      nodeName: $sink_node
      serviceAccountName: $name
      containers:
        - name: fixture
          image: local/codex-transport-fixture:e2e
          imagePullPolicy: Never
          args: ["$role"]
          env:
            - name: POD_UID
              valueFrom:
                fieldRef: {fieldPath: metadata.uid}
          volumeMounts:
            - {name: local-transport, mountPath: /run/codex-node}
      volumes:
        - name: local-transport
          hostPath: {path: /run/codex-node, type: Directory}
EOF
  kubectl -n "$namespace" rollout status "deployment/$name" --timeout=120s
done

cat <<'EOF' | kubectl apply -f -
apiVersion: codexrunner.io/v1alpha1
kind: CodexSession
metadata: {name: transport-e2e, namespace: codex-sessions}
spec:
  conversationId: transport-e2e
  desiredState: Active
  sessionData: {size: 1Gi, retain: false}
  execution: {approvalPolicy: never}
EOF
session_uid="$(kubectl -n codex-sessions get codexsession transport-e2e -o jsonpath='{.metadata.uid}')"
cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: ServiceAccount
metadata:
  name: transport-e2e
  namespace: codex-sessions
  ownerReferences:
    - {apiVersion: codexrunner.io/v1alpha1, kind: CodexSession, name: transport-e2e, uid: $session_uid, controller: true}
automountServiceAccountToken: false
---
apiVersion: v1
kind: Pod
metadata:
  name: transport-e2e
  namespace: codex-sessions
  ownerReferences:
    - {apiVersion: codexrunner.io/v1alpha1, kind: CodexSession, name: transport-e2e, uid: $session_uid, controller: true}
spec:
  nodeName: $source_node
  runtimeClassName: codex-gvisor
  serviceAccountName: transport-e2e
  automountServiceAccountToken: false
  securityContext:
    runAsNonRoot: true
    runAsUser: 65532
    seccompProfile: {type: RuntimeDefault}
  containers:
    - name: fixture
      image: local/codex-transport-fixture:e2e
      imagePullPolicy: Never
      args: ["session"]
      securityContext:
        allowPrivilegeEscalation: false
        readOnlyRootFilesystem: true
        capabilities: {drop: [ALL]}
EOF
kubectl -n codex-sessions wait --for=condition=Ready --timeout=120s pod/transport-e2e
pod_uid="$(kubectl -n codex-sessions get pod transport-e2e -o jsonpath='{.metadata.uid}')"
pod_ip="$(kubectl -n codex-sessions get pod transport-e2e -o jsonpath='{.status.podIP}')"
account_uid="$(kubectl -n codex-sessions get serviceaccount transport-e2e -o jsonpath='{.metadata.uid}')"
session_identity="spiffe://codex.local/session/transport-e2e/pod/$pod_uid"

session() { kubectl -n codex-sessions exec transport-e2e -- /fixture "$@"; }
runner() { kubectl -n codex-system exec deployment/codex-runner-api -- /fixture "$@"; }

runner connect "$api_ip:443"
session deny "$api_ip:443"
session deny "$node_ip:10250"
runner deny "$pod_ip:8081"
session deny '198.18.0.13:8080'

expires="$(jq -nr 'now + 300 | todate')"
cat <<EOF | kubectl apply -f -
apiVersion: codexrunner.io/v1alpha1
kind: SessionPolicy
metadata:
  name: $pod_uid
  namespace: codex-security
spec:
  workload:
    sessionName: transport-e2e
    sessionUID: $session_uid
    podName: transport-e2e
    podUID: $pod_uid
    nodeName: $source_node
    nodeUID: $node_uid
    serviceAccountName: transport-e2e
    serviceAccountUID: $account_uid
  version: 1
  expiresAt: "$expires"
  revoked: false
  grants:
    - source: $session_identity
      destination: spiffe://codex.local/proxy/model
      protocol: tcp
      port: 8080
    - source: spiffe://codex.local/system/runner-api
      destination: $session_identity
      protocol: tcp
      port: 8081
EOF

expect_response() {
  local expected="$1" actual=
  shift
  for ((attempt = 0; attempt < 8; attempt++)); do
    if actual="$("$@")" && [[ "$actual" == "$expected" ]]; then
      return
    fi
    sleep 1
  done
  echo "The authenticated fixture request did not return the expected response" >&2
  exit 1
}
expect_response 'session control reached' runner control "$pod_uid"
expect_response "$session_identity" session fetch http://198.18.0.13:8080/
kubectl -n codex-security patch deployment codex-model-proxy --type merge \
  -p "{\"spec\":{\"template\":{\"spec\":{\"nodeName\":\"$source_node\"}}}}"
kubectl -n codex-security rollout status deployment/codex-model-proxy --timeout=120s
expect_response "$session_identity" session fetch http://198.18.0.13:8080/
kubectl -n codex-security patch sessionpolicy "$pod_uid" --type merge -p '{"spec":{"version":2,"revoked":true}}'
if session fetch http://198.18.0.13:8080/ || runner control "$pod_uid"; then
  echo 'Revoked policy allowed a route' >&2
  exit 1
fi
kubectl -n codex-security delete daemonset codex-node-proxy --wait=true --timeout=90s
session deny "$api_ip:443"
session deny "$node_ip:10250"
session deny '198.18.0.13:8080'
echo 'Socket-attributed mTLS routing and proxy outage isolation passed.'
