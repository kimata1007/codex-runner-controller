package kubernetes

import (
	"context"
	"errors"
	"strconv"
	"time"

	v1alpha1 "github.com/kimata1007/codex-runner-controller/api/v1alpha1"
	"github.com/kimata1007/codex-runner-controller/internal/agent"
	"github.com/kimata1007/codex-runner-controller/internal/agentclient"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (a *SessionActivator) ResolveControl(ctx context.Context, conversationID string) (agentclient.ControlTarget, error) {
	if a.Namespace != SandboxNamespace {
		return agentclient.ControlTarget{}, errors.New("authenticated control requires the sandbox namespace")
	}
	name, err := SessionName(conversationID)
	if err != nil {
		return agentclient.ControlTarget{}, err
	}
	var session v1alpha1.CodexSession
	key := client.ObjectKey{Namespace: a.Namespace, Name: name}
	if err := a.Client.Get(ctx, key, &session); err != nil {
		return agentclient.ControlTarget{}, err
	}
	route := meta.FindStatusCondition(session.Status.Conditions, "RouteReady")
	if session.Spec.ConversationID != conversationID || session.Spec.DesiredState != v1alpha1.DesiredStateActive ||
		session.DeletionTimestamp != nil || session.Status.PodName != name ||
		route == nil || route.Status != metav1.ConditionTrue || route.ObservedGeneration != session.Generation {
		return agentclient.ControlTarget{}, errors.New("session control route is unavailable")
	}
	var pod corev1.Pod
	if err := a.Client.Get(ctx, key, &pod); err != nil {
		return agentclient.ControlTarget{}, err
	}
	if pod.UID == "" || !metav1.IsControlledBy(&pod, &session) || pod.Status.Phase != corev1.PodRunning || pod.DeletionTimestamp != nil {
		return agentclient.ControlTarget{}, errors.New("session Pod binding is unavailable")
	}
	fresh, ready := false, false
	now := time.Now()
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
			ready = true
		}
		if condition.Type == v1alpha1.SandboxRouteReady && condition.Status == corev1.ConditionTrue &&
			condition.Message == strconv.FormatInt(session.Generation, 10) &&
			!condition.LastProbeTime.After(now.Add(2*time.Second)) && now.Sub(condition.LastProbeTime.Time) < 20*time.Second {
			fresh = true
		}
	}
	if !fresh || !ready {
		return agentclient.ControlTarget{}, errors.New("session route probe is stale")
	}
	return agentclient.ControlTarget{PodUID: pod.UID, Params: agent.ThreadStartParams{
		Model: session.Spec.Execution.Model, CWD: DefaultMountPath + "/workspace", ApprovalPolicy: session.Spec.Execution.ApprovalPolicy,
	}}, nil
}
