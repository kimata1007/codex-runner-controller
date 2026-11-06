package v1alpha1

import (
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
)

func TestAddToSchemeRegistersCodexSession(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := AddToScheme(scheme); err != nil {
		t.Fatalf("add to scheme: %v", err)
	}
	object, err := scheme.New(GroupVersion.WithKind("CodexSession"))
	if err != nil {
		t.Fatalf("new codex session: %v", err)
	}
	if _, ok := object.(*CodexSession); !ok {
		t.Fatalf("scheme returned %T", object)
	}
}
