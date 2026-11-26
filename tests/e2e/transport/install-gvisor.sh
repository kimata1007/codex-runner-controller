#!/usr/bin/env bash
set -euo pipefail
umask 077

if [[ "$(kubectl config current-context)" != kind-codex-identity ]]; then
  echo 'The gVisor installer is limited to the kind-codex-identity test cluster.' >&2
  exit 1
fi
release=release-20261123.0
nodes="$(kind get nodes --name codex-identity)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
cd "$work"
arch="$(docker exec "$(printf '%s\n' "$nodes" | head -1)" uname -m)"
case "$arch" in
  x86_64) digest=19a7e2fa382d46c8cba71a0469a416c6249e4760a09300626d897aacdf26c45d ;;
  aarch64) digest=646fa7abdd5400fd11a87572ba6f6eb7c36d9e953e72baf10cc4a031cbee55d8 ;;
  *) echo "Unsupported gVisor architecture $arch" >&2; exit 1 ;;
esac
archive="gvisor-$arch.tar.bz2"
curl --fail --silent --show-error --location --max-time 180 --output "$archive" \
  "https://github.com/google/gvisor/releases/download/$release/$archive"
printf '%s  %s\n' "$digest" "$archive" | shasum -a 256 -c -
tar -xf "$archive"
for node in $nodes; do
  [[ "$node" == *-control-plane ]] && continue
  docker cp runsc "$node:/usr/local/bin/runsc"
  docker cp containerd-shim-runsc-v1 "$node:/usr/local/bin/containerd-shim-runsc-v1"
  docker cp gvisor-bin "$node:/usr/local/bin/gvisor-bin"
  docker exec "$node" sh -ec '
    if ! grep -Fq .runtimes.runsc /etc/containerd/config.toml; then
      case "$(sed -n "s/^version = \([23]\)$/\1/p" /etc/containerd/config.toml)" in
        2) config=io.containerd.grpc.v1.cri ;;
        3) config=io.containerd.cri.v1.runtime ;;
        *) exit 1 ;;
      esac
      printf "\n[plugins.\"%s\".containerd.runtimes.runsc]\n  runtime_type = \"io.containerd.runsc.v1\"\n" "$config" >> /etc/containerd/config.toml
    fi
    /usr/local/bin/runsc --version
    systemctl restart containerd
    systemctl is-active containerd
  '
  kubectl wait --for=condition=Ready --timeout=90s "node/$node"
  kubectl label "node/$node" codexrunner.io/sandbox=gvisor --overwrite
done
kubectl delete runtimeclass codex-gvisor --ignore-not-found
