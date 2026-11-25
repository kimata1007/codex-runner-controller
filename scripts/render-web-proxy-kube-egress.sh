#!/usr/bin/env bash
set -euo pipefail

role="${1:-web}"
if [[ "$role" != web && "$role" != github ]]; then
  echo 'Expected web or github.' >&2
  exit 1
fi
service="$(kubectl -n default get service kubernetes -o json)"
kubectl -n default get endpointslices -l kubernetes.io/service-name=kubernetes -o json |
  jq -e --argjson service "$service" --arg role "$role" '
    def ipv4: test("^[0-9.]+$");
    [.items[] | select(.addressType == "IPv4") | . as $slice
       | .endpoints[] | select(.conditions.ready != false) | .addresses[] as $ip
       | select($ip | ipv4)
       | $slice.ports[]
       | select(.protocol == "TCP" and (.port | type) == "number")
       | {to: [{ipBlock:{cidr: ($ip + "/32")}}], ports: [{protocol:"TCP", port:.port}]}] as $endpoints
    | if ($endpoints | length) == 0 then error("No IPv4 Kubernetes API endpoint found") else
        {apiVersion:"networking.k8s.io/v1", kind:"NetworkPolicy",
         metadata:{name:($role + "-proxy-kubernetes-api"), namespace:"codex-security"},
         spec:{podSelector:{matchLabels:{"app.kubernetes.io/name":("codex-" + $role + "-proxy")}},
               policyTypes:["Egress"],
               egress:($endpoints + [
                 $service.spec.clusterIPs[] | select(ipv4)
                 | {to:[{ipBlock:{cidr:(. + "/32")}}],ports:[{protocol:"TCP",port:443}]}])}}
      end'
