package kubernetes

import "testing"

func TestResourceBuilderForwardsOTLPEndpointToAgent(t *testing.T) {
	const endpoint = "http://otel-collector.observability.svc.cluster.local:4318"
	pod, err := (ResourceBuilder{
		RunnerImage:  "runner:v1.0.0",
		OTLPEndpoint: endpoint,
	}).BuildPod("codex-system", "session-1", testSessionSpec())
	if err != nil {
		t.Fatalf("build pod: %v", err)
	}
	for _, env := range pod.Spec.Containers[0].Env {
		if env.Name == "OTEL_EXPORTER_OTLP_ENDPOINT" {
			if env.Value != endpoint {
				t.Fatalf("OTLP endpoint = %q", env.Value)
			}
			return
		}
	}
	t.Fatal("OTLP endpoint not propagated to runner-agent")
}

func TestResourceBuilderOmitsOTLPWhenDisabled(t *testing.T) {
	pod, err := (ResourceBuilder{RunnerImage: "runner:v1.0.0"}).BuildPod("codex-system", "session-1", testSessionSpec())
	if err != nil {
		t.Fatalf("build pod: %v", err)
	}
	for _, env := range pod.Spec.Containers[0].Env {
		if env.Name == "OTEL_EXPORTER_OTLP_ENDPOINT" {
			t.Fatal("disabled OTLP endpoint must not be added to agent")
		}
	}
}
