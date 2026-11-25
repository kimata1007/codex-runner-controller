#!/usr/bin/env bash
set -euo pipefail

: "${TAGPR_NEXT_VERSION:?tagpr must provide TAGPR_NEXT_VERSION}"
if [[ ! "$TAGPR_NEXT_VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "unexpected release version: $TAGPR_NEXT_VERSION" >&2
  exit 1
fi

perl -0pi -e 's{(ghcr\.io/kimata1007/codex-runner-(?:api|controller|agent|pwa):)v[0-9]+\.[0-9]+\.[0-9]+}{$1$ENV{TAGPR_NEXT_VERSION}}g' \
  config/api/deployment.yaml \
  config/manager/deployment.yaml \
  config/web/deployment.yaml

perl -0pi -e 's{(ghcr\.io/kimata1007/codex-(?:identity-issuer|node-proxy|model-proxy|web-proxy|github-proxy):)v[0-9]+\.[0-9]+\.[0-9]+}{$1$ENV{TAGPR_NEXT_VERSION}}g' \
  config/identity-issuer/deployment.yaml \
  config/node-proxy/daemonset.yaml \
  config/model-proxy/deployment.yaml \
  config/web-proxy/deployment.yaml \
  config/github-proxy/deployment.yaml
