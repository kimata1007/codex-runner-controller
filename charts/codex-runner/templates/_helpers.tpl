{{- define "codex-runner.image" -}}
{{- printf "%s/codex-runner-%s:%s" .root.Values.images.registry .component .root.Values.images.tag -}}
{{- end -}}
{{- define "codex-runner.validate" -}}
{{- if not .Values.existingSecret -}}{{ fail "existingSecret must reference a pre-existing Kubernetes Secret" }}{{- end -}}
{{- if and (not .Values.libsql.enabled) (not .Values.libsql.url) -}}{{ fail "libsql.url must be set when libsql.enabled=false" }}{{- end -}}
{{- if ne (int .Values.api.replicas) 1 -}}{{ fail "api.replicas must be 1 (ChatGPT refresh-token rotation is not multi-replica safe)" }}{{- end -}}
{{- if ne (int .Values.authBroker.replicas) 1 -}}{{ fail "authBroker.replicas must be 1 (device login is stateful)" }}{{- end -}}
{{- if not (has .Values.api.approvalPolicy (list "never" "on-request" "untrusted")) -}}{{ fail "api.approvalPolicy must be never, on-request, or untrusted" }}{{- end -}}
{{- end -}}
