# Codex Runner Helm chart

This chart installs Runner API, Controller, Auth Broker, PWA, optionally libSQL, CRD and namespaced RBAC. It preserves fixed service names from the Kustomize manifests; **install only once per namespace** and do not deploy Kustomize and Helm resources together.

## Secret prerequisites

Create the `codex-runner-tokens` Secret in the target namespace **outside Helm**. Required keys: `api-token`, `agent-token` and `runner-auth-encryption-key` (32 random bytes, Base64-encoded in the Secret data). Do not commit real credentials or pass them as Helm values. `libsql-auth-token` is optional. If using private Git checkouts, create a separate credential Secret with a `token` key and set `api.gitCredentialsSecretName`.

## Installation

```sh
helm lint charts/codex-runner
helm template codex-runner charts/codex-runner --namespace codex-system
helm install codex-runner charts/codex-runner --namespace codex-system --create-namespace
```

CRD is installed from `crds/` automatically on initial install, but Helm does not automatically upgrade or delete CRDs. Apply future CRD schema upgrades separately and review conversions. With `libsql.enabled=false`, supply `libsql.url` and manage database availability separately.

The default approval policy is `never`; **this disables approval prompts, not sandbox restrictions**. Change `api.approvalPolicy` to `on-request` if you prefer interactive approvals. Existing CodexSession CRs and Pods may retain execution settings, even after a Helm upgrade.

## Migration

The Chart preserves existing object names. **Do not run Helm install directly against objects already managed by Kustomize**: Helm will reject resources missing ownership labels/annotations. Back up libSQL PVCs and ChatGPT auth Secret, then migrate ownership in a controlled maintenance window, or deploy a clean namespace. Never delete the existing libSQL StatefulSet/PVC blindly; data loss is possible.

Set `images.tag` to a published release image version. Auth Broker and session agent share the same version to avoid protocol skew. External ingress/Tailnet access is intentionally not installed.
