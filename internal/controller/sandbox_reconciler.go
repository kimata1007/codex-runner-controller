package controller

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	v1alpha1 "github.com/kimata1007/codex-runner-controller/api/v1alpha1"
	"github.com/kimata1007/codex-runner-controller/internal/kubernetes"
	"github.com/kimata1007/codex-runner-controller/internal/zerotrust"
	corev1 "k8s.io/api/core/v1"
	nodev1 "k8s.io/api/node/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	policyFinalizer = "codexrunner.io/session-policies"
	policyOwner     = "codexrunner.io/session-uid"
)

type SandboxReconciler struct {
	Client          client.Client
	Builder         kubernetes.ResourceBuilder
	PolicyNamespace string
	Domain          string
}

func (r *SandboxReconciler) Reconcile(ctx context.Context, key client.ObjectKey) (reconcile.Result, error) {
	var s v1alpha1.CodexSession
	if err := r.Client.Get(ctx, key, &s); err != nil {
		return reconcile.Result{}, client.IgnoreNotFound(err)
	}
	if s.DeletionTimestamp != nil {
		if !controllerutil.ContainsFinalizer(&s, policyFinalizer) {
			return reconcile.Result{}, nil
		}
		if err := r.removePolicies(ctx, s.UID, ""); err != nil {
			return reconcile.Result{}, err
		}
		controllerutil.RemoveFinalizer(&s, policyFinalizer)
		return reconcile.Result{}, r.Client.Update(ctx, &s)
	}
	if controllerutil.AddFinalizer(&s, policyFinalizer) {
		if err := r.Client.Update(ctx, &s); err != nil {
			return reconcile.Result{}, err
		}
	}
	phase, pod, reason, err := r.sync(ctx, &s)
	before := s.DeepCopy().Status
	s.Status.Phase, s.Status.PodName, s.Status.WorkspaceClaim = phase, pod, s.Name
	s.Status.ObservedGeneration = s.Generation
	routeStatus := metav1.ConditionFalse
	if reason == "AuthenticatedRoute" {
		routeStatus = metav1.ConditionTrue
	}
	meta.SetStatusCondition(&s.Status.Conditions, metav1.Condition{Type: "RouteReady", Status: routeStatus,
		Reason: reason, Message: reason, ObservedGeneration: s.Generation})
	if !apiequality.Semantic.DeepEqual(before, s.Status) {
		if statusErr := r.Client.Status().Update(ctx, &s); statusErr != nil {
			err = errors.Join(err, statusErr)
		} else if before.Phase != phase {
			slog.InfoContext(ctx, "sandbox session phase changed", "session_uid", s.UID, "phase", phase, "reason", reason)
		}
	}
	return reconcile.Result{RequeueAfter: 5 * time.Second}, err
}

func (r *SandboxReconciler) sync(ctx context.Context, s *v1alpha1.CodexSession) (phase, podName, reason string, err error) {
	if err := s.Spec.Validate(); err != nil || s.Spec.CredentialsRef != nil {
		return v1alpha1.PhaseFailed, "", "InvalidSandboxConfiguration", err
	}
	if s.Spec.Access != nil && s.Spec.Access.Model != nil {
		allowed := false
		for _, model := range s.Spec.Access.Model.Models {
			if model.Name == s.Spec.Execution.Model {
				allowed = true
				break
			}
		}
		if !allowed || s.Spec.Execution.Model == "" {
			return v1alpha1.PhaseFailed, "", "ModelPolicyMismatch", nil
		}
	}
	key := client.ObjectKeyFromObject(s)
	var pod corev1.Pod
	getPod := r.Client.Get(ctx, key, &pod)
	if getPod != nil && !apierrors.IsNotFound(getPod) {
		return v1alpha1.PhaseStarting, "", "PodLookupFailed", getPod
	}
	if getPod == nil && !metav1.IsControlledBy(&pod, s) {
		return v1alpha1.PhaseFailed, "", "PodOwnershipMismatch", nil
	}
	idle, _, err := idleState(s.Spec, s.Status, time.Now())
	if err != nil {
		return v1alpha1.PhaseFailed, "", "InvalidSessionActivity", err
	}
	if s.Spec.DesiredState == v1alpha1.DesiredStateSuspended || idle ||
		pod.Status.Phase == corev1.PodFailed || pod.Status.Phase == corev1.PodSucceeded {
		// Revocation precedes Pod deletion. Keep the tombstone for this UID
		// until the workload has disappeared.
		if getPod == nil {
			if err := r.revokePolicy(ctx, pod.UID); err != nil {
				return v1alpha1.PhaseStarting, pod.Name, "PolicyRevocationFailed", err
			}
			if err := r.Client.Delete(ctx, &pod, client.Preconditions{UID: &pod.UID}); client.IgnoreNotFound(err) != nil {
				return v1alpha1.PhaseStarting, pod.Name, "PodDeletionFailed", err
			}
		} else if err := r.removePolicies(ctx, s.UID, ""); err != nil {
			return v1alpha1.PhaseStarting, "", "PolicyCleanupFailed", err
		}
		if s.Spec.DesiredState == v1alpha1.DesiredStateSuspended || idle {
			return v1alpha1.PhaseSuspended, "", "SessionSuspended", nil
		}
		return v1alpha1.PhaseStarting, "", "PodReplacementPending", nil
	}

	var runtime nodev1.RuntimeClass
	if err := r.Client.Get(ctx, client.ObjectKey{Name: kubernetes.SandboxRuntimeClass}, &runtime); err != nil ||
		runtime.Handler != "runsc" || runtime.Scheduling == nil || runtime.Scheduling.NodeSelector["codexrunner.io/sandbox"] != "gvisor" {
		return v1alpha1.PhaseFailed, "", "GVisorUnavailable", nil
	}
	var namespace corev1.Namespace
	if err := r.Client.Get(ctx, client.ObjectKey{Name: s.Namespace}, &namespace); err != nil ||
		namespace.Labels["pod-security.kubernetes.io/enforce"] != "restricted" {
		return v1alpha1.PhaseFailed, "", "RestrictedNamespaceRequired", nil
	}

	var account corev1.ServiceAccount
	if err := r.Client.Get(ctx, key, &account); apierrors.IsNotFound(err) {
		fresh := kubernetes.BuildSandboxServiceAccount(s.Name)
		if err := r.create(ctx, s, fresh); err != nil {
			return v1alpha1.PhaseStarting, "", "ServiceAccountCreationFailed", err
		}
		return v1alpha1.PhaseStarting, "", "ServiceAccountPending", nil
	} else if err != nil || !metav1.IsControlledBy(&account, s) ||
		account.AutomountServiceAccountToken == nil || *account.AutomountServiceAccountToken {
		return v1alpha1.PhaseFailed, "", "ServiceAccountUnverified", err
	}
	var pvc corev1.PersistentVolumeClaim
	if err := r.Client.Get(ctx, key, &pvc); apierrors.IsNotFound(err) {
		fresh, err := r.Builder.BuildPVC(s.Namespace, s.Name, s.Spec)
		if err == nil {
			err = r.create(ctx, s, fresh)
		}
		if err != nil {
			return v1alpha1.PhaseFailed, "", "WorkspaceCreationFailed", err
		}
		return v1alpha1.PhaseStarting, "", "WorkspacePending", nil
	} else if err != nil || (!metav1.IsControlledBy(&pvc, s) && pvc.Labels[policyOwner] != string(s.UID)) {
		return v1alpha1.PhaseFailed, "", "WorkspaceUnverified", err
	}
	if getPod != nil {
		fresh, err := r.Builder.BuildSandboxPod(s.Name, s.Spec)
		if err == nil {
			err = r.create(ctx, s, fresh)
		}
		if err != nil {
			return v1alpha1.PhaseFailed, "", "SandboxPodCreationFailed", err
		}
		return v1alpha1.PhaseStarting, s.Name, "SandboxPodPending", nil
	}
	if pod.DeletionTimestamp != nil || pod.Status.Phase != corev1.PodRunning || pod.Spec.NodeName == "" {
		return v1alpha1.PhaseStarting, pod.Name, "SandboxPodPending", nil
	}
	if err := r.removePolicies(ctx, s.UID, pod.UID); err != nil {
		return v1alpha1.PhaseStarting, pod.Name, "PolicyCleanupFailed", err
	}
	if err := r.ensurePolicy(ctx, s, &pod, &account); err != nil {
		return v1alpha1.PhaseStarting, pod.Name, "SessionPolicyUnavailable", err
	}
	now := time.Now()
	for _, condition := range pod.Status.Conditions {
		if condition.Type == v1alpha1.SandboxRouteReady && condition.Status == corev1.ConditionTrue &&
			condition.Message == strconv.FormatInt(s.Generation, 10) &&
			!condition.LastProbeTime.After(now.Add(2*time.Second)) && now.Sub(condition.LastProbeTime.Time) < 20*time.Second && podReady(&pod) {
			return readyPhase(s.Status.Phase), pod.Name, "AuthenticatedRoute", nil
		}
	}
	return v1alpha1.PhaseStarting, pod.Name, "AuthenticatedRoutePending", nil
}

func (r *SandboxReconciler) create(ctx context.Context, owner *v1alpha1.CodexSession, object client.Object) error {
	if claim, ok := object.(*corev1.PersistentVolumeClaim); ok && owner.Spec.SessionData.Retain {
		claim.Labels[policyOwner] = string(owner.UID)
		return r.Client.Create(ctx, object)
	}
	if err := controllerutil.SetControllerReference(owner, object, r.Client.Scheme(), controllerutil.WithBlockOwnerDeletion(false)); err != nil {
		return err
	}
	return r.Client.Create(ctx, object)
}

func (r *SandboxReconciler) ensurePolicy(ctx context.Context, s *v1alpha1.CodexSession, pod *corev1.Pod, account *corev1.ServiceAccount) error {
	var node corev1.Node
	if err := r.Client.Get(ctx, client.ObjectKey{Name: pod.Spec.NodeName}, &node); err != nil {
		return err
	}
	if node.UID == "" || node.Labels["codexrunner.io/sandbox"] != "gvisor" || pod.Spec.ServiceAccountName != account.Name ||
		pod.Spec.RuntimeClassName == nil || *pod.Spec.RuntimeClassName != kubernetes.SandboxRuntimeClass {
		return errors.New("sandbox binding is inconsistent")
	}
	workload := v1alpha1.SessionWorkload{SessionName: s.Name, SessionUID: s.UID, PodName: pod.Name, PodUID: pod.UID,
		NodeName: node.Name, NodeUID: node.UID, ServiceAccountName: account.Name, ServiceAccountUID: account.UID}
	prefix := "spiffe://" + r.Domain
	identity := prefix + "/session/" + s.Name + "/pod/" + string(pod.UID)
	if _, err := zerotrust.ParseIdentity(identity, r.Domain); err != nil || s.UID == "" || account.UID == "" {
		return errors.New("sandbox identity is incomplete")
	}
	desired := v1alpha1.SessionPolicySpec{Workload: workload, Version: 1, SessionGeneration: s.Generation, ExpiresAt: metav1.NewTime(time.Now().Add(6 * time.Minute)),
		Grants: []v1alpha1.NetworkGrant{{Source: prefix + "/system/runner-api", Destination: identity, Protocol: "tcp", Port: zerotrust.ControlPort}}}
	if s.Spec.Access != nil {
		desired.Model, desired.Web, desired.GitHub = s.Spec.Access.Model, s.Spec.Access.Web, s.Spec.Access.GitHub
		for _, access := range []struct {
			role    string
			allowed bool
		}{{"model", desired.Model != nil}, {"web", desired.Web != nil}, {"github", desired.GitHub != nil}} {
			if access.allowed {
				desired.Grants = append(desired.Grants, v1alpha1.NetworkGrant{Source: identity, Destination: prefix + "/proxy/" + access.role, Protocol: "tcp", Port: zerotrust.ProxyPort})
			}
		}
	}
	key := client.ObjectKey{Namespace: r.PolicyNamespace, Name: string(pod.UID)}
	var policy v1alpha1.SessionPolicy
	if err := r.Client.Get(ctx, key, &policy); apierrors.IsNotFound(err) {
		return r.Client.Create(ctx, &v1alpha1.SessionPolicy{ObjectMeta: metav1.ObjectMeta{Namespace: key.Namespace, Name: key.Name,
			Labels: map[string]string{policyOwner: string(s.UID)}}, Spec: desired})
	} else if err != nil {
		return err
	}
	if policy.Spec.Revoked || policy.Spec.Workload != workload || policy.Labels[policyOwner] != string(s.UID) {
		return fmt.Errorf("session policy binding cannot be reused")
	}
	prior := desired
	prior.Version, prior.ExpiresAt = policy.Spec.Version, policy.Spec.ExpiresAt
	if time.Until(prior.ExpiresAt.Time) >= 3*time.Minute && apiequality.Semantic.DeepEqual(prior, policy.Spec) {
		return nil
	}
	desired.Version = policy.Spec.Version + 1
	policy.Spec = desired
	return r.Client.Update(ctx, &policy)
}

func (r *SandboxReconciler) revokePolicy(ctx context.Context, uid types.UID) error {
	var policy v1alpha1.SessionPolicy
	if err := r.Client.Get(ctx, client.ObjectKey{Namespace: r.PolicyNamespace, Name: string(uid)}, &policy); err != nil {
		return client.IgnoreNotFound(err)
	}
	if policy.Spec.Revoked {
		return nil
	}
	policy.Spec.Revoked = true
	policy.Spec.Version++
	return r.Client.Update(ctx, &policy)
}

func (r *SandboxReconciler) removePolicies(ctx context.Context, session, keep types.UID) error {
	var policies v1alpha1.SessionPolicyList
	if err := r.Client.List(ctx, &policies, client.InNamespace(r.PolicyNamespace), client.MatchingLabels{policyOwner: string(session)}); err != nil {
		return err
	}
	for i := range policies.Items {
		policy := &policies.Items[i]
		if policy.Spec.Workload.PodUID == keep {
			continue
		}
		if err := r.Client.Delete(ctx, policy, client.Preconditions{ResourceVersion: &policy.ResourceVersion}); client.IgnoreNotFound(err) != nil {
			return err
		}
	}
	return nil
}
