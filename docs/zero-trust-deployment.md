# Deployment gates

The opt-in config/zero-trust overlay creates a quarantined execution namespace.
It does not enable the existing controller to schedule sessions there. NetworkPolicy
must be enforced by the installed CNI; host and node traffic require additional
enforcement. Do not migrate live sessions into this namespace yet.

The node-proxy binary supports a trusted static route and per-connection certificate
reload. A route listener is not a trusted interceptor; the Unix socket must never
be mounted into an execution pod. A real CNI integration must bind the original
socket to a verified Pod UID, handle same-node paths and address reuse, and close
existing streams on workload deletion.

The node proxy image can be built with Dockerfile.node-proxy. No cluster-wide
privileged DaemonSet is installed until source attestation and interception are
implemented and tested. Existing token-based deployments are unchanged.
