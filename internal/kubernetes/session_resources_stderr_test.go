package kubernetes

import "testing"

func TestResourceBuilderOptInAppServerStderr(t *testing.T) {
	for _, tc := range []struct {
		name    string
		enabled bool
		want    bool
	}{
		{name: "default disabled", enabled: false, want: false},
		{name: "enabled", enabled: true, want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pod, err := (ResourceBuilder{
				RunnerImage:            "example.com/runner-agent:v1",
				ForwardAppServerStderr: tc.enabled,
			}).BuildPod("codex-system", "session-1", testSessionSpec())
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, env := range pod.Spec.Containers[0].Env {
				if env.Name == "CODEX_APP_SERVER_STDERR_TO_POD_LOG" {
					found = true
					if env.Value != "true" {
						t.Fatalf("unexpected env value %q", env.Value)
					}
				}
			}
			if found != tc.want {
				t.Fatalf("forward stderr env present = %t, want %t", found, tc.want)
			}
		})
	}
}
