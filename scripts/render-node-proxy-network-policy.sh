#!/usr/bin/env bash
set -euo pipefail

if [[ $# -eq 0 ]]; then
  echo 'Provide the names of the nodes that run the Session Pods.' >&2
  exit 1
fi

kubectl get nodes "$@" -o json | jq -e '
  [(.items // [.])[] | .status.addresses[] | select(.type == "InternalIP") | .address | select(test("^[0-9.]+$"))] as $ips
  | if ($ips | length) == 0 then error("No IPv4 InternalIP found") else
    {
      apiVersion: "networking.k8s.io/v1",
      kind: "NetworkPolicy",
      metadata: {name: "session-node-proxy-ports", namespace: "codex-sessions"},
      spec: {
        podSelector: {},
        policyTypes: ["Egress"],
        egress: [{
          to: ($ips | unique | map({ipBlock: {cidr: (. + "/32")}})),
          ports: ([18443, 18444, 18445] | map({protocol: "TCP", port: .}))
        }]
      }
    }
    end'
