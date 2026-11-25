#!/usr/bin/env bash
set -euo pipefail
umask 077

if [[ "$(kubectl config current-context)" != kind-codex-identity ]]; then
  echo "The identity test requires the kind-codex-identity context." >&2
  exit 1
fi

cd "$(dirname "$0")/../../.."
work_dir="$(mktemp -d)"
port_forward_pid=
cleanup() {
  if [[ -n "$port_forward_pid" ]]; then
    kill "$port_forward_pid" 2>/dev/null || true
    wait "$port_forward_pid" 2>/dev/null || true
  fi
  rm -rf "$work_dir"
}
trap cleanup EXIT

issuer_host=codex-identity-issuer.codex-security.svc
issuer_image="${IDENTITY_IMAGE:-local/codex-identity-issuer:e2e}"
kubectl kustomize config/identity-issuer |
  sed "s|image: ghcr.io/kimata1007/codex-identity-issuer:[^ ]*|image: $issuer_image|" |
  kubectl apply -f -
openssl req -new -x509 -newkey ed25519 -nodes -days 1 -subj '/CN=Workload E2E Root' \
  -addext 'basicConstraints=critical,CA:TRUE' -addext 'keyUsage=critical,keyCertSign,cRLSign' \
  -keyout "$work_dir/ca.key" -out "$work_dir/ca.crt" 2>/dev/null
openssl req -new -x509 -newkey ed25519 -nodes -days 1 -subj "/CN=$issuer_host" \
  -addext "subjectAltName=DNS:$issuer_host" -addext 'extendedKeyUsage=serverAuth' \
  -keyout "$work_dir/tls.key" -out "$work_dir/tls.crt" 2>/dev/null
openssl req -new -newkey ed25519 -nodes -subj '/CN=ignored' -keyout "$work_dir/workload.key" \
  -addext 'subjectAltName=URI:spiffe://codex.local/system/runner-api' \
  -outform DER -out "$work_dir/workload.csr" 2>/dev/null
kubectl -n codex-security create secret generic codex-identity-signing-root \
  --from-file="ca.crt=$work_dir/ca.crt" --from-file="ca.key=$work_dir/ca.key" --from-file="bundle.crt=$work_dir/ca.crt"
kubectl -n codex-security create secret tls codex-identity-issuer-tls \
  --cert="$work_dir/tls.crt" --key="$work_dir/tls.key"
kubectl -n codex-security rollout status deployment/codex-identity-issuer --timeout=120s
kubectl wait --for=condition=Established --timeout=30s crd/sessionpolicies.codexrunner.io crd/codexsessions.codexrunner.io
kubectl apply -f tests/e2e/identity/workloads.yaml
kubectl -n codex-security rollout status daemonset/codex-node-proxy --timeout=120s

get_proxies() {
  kubectl -n codex-security get pods -l app.kubernetes.io/name=codex-node-proxy -o json
}
proxy_nodes="$(get_proxies | jq -r '[.items[].spec.nodeName] | unique | join(" ")')"
read -r source_node other_node _ <<<"$proxy_nodes"
[[ -n "$source_node" && -n "$other_node" ]]
node_uid="$(kubectl get node "$source_node" -o jsonpath='{.metadata.uid}')"
session_uid="$(kubectl -n codex-sessions get codexsession issuer-e2e -o jsonpath='{.metadata.uid}')"

cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: ServiceAccount
metadata:
  name: issuer-e2e
  namespace: codex-sessions
  ownerReferences:
    - apiVersion: codexrunner.io/v1alpha1
      kind: CodexSession
      name: issuer-e2e
      uid: $session_uid
      controller: true
automountServiceAccountToken: false
EOF
cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: Pod
metadata:
  name: issuer-e2e
  namespace: codex-sessions
  ownerReferences:
    - apiVersion: codexrunner.io/v1alpha1
      kind: CodexSession
      name: issuer-e2e
      uid: $session_uid
      controller: true
spec:
  nodeName: $source_node
  runtimeClassName: codex-gvisor
  serviceAccountName: issuer-e2e
  automountServiceAccountToken: false
  securityContext:
    runAsNonRoot: true
    runAsUser: 65532
    seccompProfile: {type: RuntimeDefault}
  containers:
    - name: session-fixture
      image: alpine:3.24.0@sha256:a2d49ea686c2adfe3c992e47dc3b5e7fa6e6b5055609400dc2acaeb241c829f4
      command: ["/bin/sleep", "3600"]
      securityContext:
        allowPrivilegeEscalation: false
        readOnlyRootFilesystem: true
        capabilities: {drop: [ALL]}
EOF
kubectl -n codex-sessions wait --for=condition=Ready --timeout=120s pod/issuer-e2e
pod_uid="$(kubectl -n codex-sessions get pod issuer-e2e -o jsonpath='{.metadata.uid}')"
account_uid="$(kubectl -n codex-sessions get serviceaccount issuer-e2e -o jsonpath='{.metadata.uid}')"
session_identity="spiffe://codex.local/session/issuer-e2e/pod/$pod_uid"

kubectl -n codex-security port-forward --address 127.0.0.1 service/codex-identity-issuer :8443 \
  >"$work_dir/port-forward.log" 2>&1 &
port_forward_pid=$!
local_port=
for ((attempt = 0; attempt < 30; attempt++)); do
  local_port="$(sed -n 's/^Forwarding from 127\.0\.0\.1:\([0-9][0-9]*\).*/\1/p' "$work_dir/port-forward.log")"
  [[ -z "$local_port" ]] || break
  kill -0 "$port_forward_pid"
  sleep 1
done
[[ -n "$local_port" ]]

write_auth() {
  local pod
  pod="$(get_proxies | jq -r --arg node "$1" '.items[] | select(.spec.nodeName == $node) | .metadata.name')"
  {
    printf 'Authorization: Bearer '
    kubectl -n codex-security exec "$pod" -- cat /var/run/codex/token
    printf '\n'
  } >"$work_dir/auth"
}

issue() {
  local uid="$1" expected_status="$2" status
  jq -nc --arg uid "$uid" --arg csr "$(base64 <"$work_dir/workload.csr" | tr -d '\n')" \
    '{podUID:$uid,csr:$csr}' >"$work_dir/request"
  status="$(curl --silent --show-error --max-time 15 --noproxy '*' \
    --resolve "$issuer_host:$local_port:127.0.0.1" --cacert "$work_dir/tls.crt" \
    --header @"$work_dir/auth" --header 'Content-Type: application/json' \
    --data-binary @"$work_dir/request" --output "$work_dir/response" --write-out '%{http_code}' \
    "https://$issuer_host:$local_port/v1/certificates")"
  if [[ "$status" != "$expected_status" ]]; then
    echo "Expected HTTP $expected_status, received $status from issuer" >&2
    exit 1
  fi
}

check_certificate() {
  jq -er '.certificate' "$work_dir/response" |
    openssl base64 -d -A |
    openssl x509 -inform DER -out "$work_dir/leaf.crt"
  openssl verify -CAfile "$work_dir/ca.crt" "$work_dir/leaf.crt"
  openssl x509 -in "$work_dir/leaf.crt" -noout -ext subjectAltName | grep -F "URI:$1"
}

write_auth "$source_node"
issue '' 200
check_certificate "spiffe://codex.local/node/$node_uid"
issue "$pod_uid" 403

expires="$(jq -nr 'now + 300 | todate')"
cat <<EOF | kubectl apply -f -
apiVersion: codexrunner.io/v1alpha1
kind: SessionPolicy
metadata:
  name: $pod_uid
  namespace: codex-security
spec:
  workload:
    sessionName: issuer-e2e
    sessionUID: $session_uid
    podName: issuer-e2e
    podUID: $pod_uid
    nodeName: $source_node
    nodeUID: $node_uid
    serviceAccountName: issuer-e2e
    serviceAccountUID: $account_uid
  version: 1
  expiresAt: "$expires"
  revoked: false
  grants:
    - source: $session_identity
      destination: spiffe://codex.local/proxy/model
      protocol: tcp
      port: 8080
EOF
issue "$pod_uid" 200
check_certificate "$session_identity"
write_auth "$other_node"
issue "$pod_uid" 403
write_auth "$source_node"

if kubectl -n codex-security patch sessionpolicy "$pod_uid" --type merge -p '{"spec":{"revoked":true}}'; then
  echo 'Kubernetes accepted a policy change without an increased version' >&2
  exit 1
fi
kubectl -n codex-security patch sessionpolicy "$pod_uid" --type merge -p '{"spec":{"version":2,"revoked":true}}'
issue "$pod_uid" 403
if kubectl -n codex-security patch sessionpolicy "$pod_uid" --type merge -p '{"spec":{"version":3,"revoked":false}}'; then
  echo 'Kubernetes accepted the reactivation of a revoked Pod UID' >&2
  exit 1
fi
echo 'Identity issuance, cross-node denial, and irreversible revocation passed.'
