#!/usr/bin/env bash
set -uo pipefail

dir="${1:-kind-e2e-debug}"
mkdir -p "$dir"
kubectl get nodes -o wide > "$dir/nodes.txt" 2>&1 || true
kubectl -n codex-system get pods,deploy,sts,svc,pvc,codexsessions -o wide > "$dir/resources.txt" 2>&1 || true
kubectl -n codex-system describe pods > "$dir/pod-descriptions.txt" 2>&1 || true
kubectl -n codex-system get events --sort-by=.lastTimestamp > "$dir/events.txt" 2>&1 || true
for workload in deployment/codex-runner-controller deployment/codex-runner-api statefulset/postgres; do
  name="${workload//\//-}"
  kubectl -n codex-system logs "$workload" --all-containers --tail=300 > "$dir/$name.log" 2>&1 || true
done
for pod in e2e-ready e2e-missing-secret; do
  kubectl -n codex-system describe pod "$pod" > "$dir/$pod.txt" 2>&1 || true
done
echo "Saved safe Kubernetes diagnostics to $dir"
