package zerotrust

import (
	"context"
	"errors"

	appsv1 "k8s.io/api/apps/v1"
	authv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const NodeTokenAudience = "codexrunner.io/identity-issuer"

var ErrNodeDenied = errors.New("node authentication denied")

type TokenReviewer interface {
	Create(context.Context, *authv1.TokenReview, metav1.CreateOptions) (*authv1.TokenReview, error)
}

// NodeAuthenticator authenticates the dedicated DaemonSet's pod-bound token
// and checks the objects it names against the live API on every request.
type NodeAuthenticator struct {
	Tokens         TokenReviewer
	Reader         client.Reader
	Namespace      string
	ServiceAccount string
	DaemonSet      string
}

type authenticatedNode struct {
	name string
	uid  types.UID
}

func (a NodeAuthenticator) authenticate(ctx context.Context, token string) (authenticatedNode, error) {
	if token == "" || len(token) > 8192 || a.Reader == nil || a.Tokens == nil ||
		a.Namespace == "" || a.ServiceAccount == "" || a.DaemonSet == "" {
		return authenticatedNode{}, ErrNodeDenied
	}
	review, err := a.Tokens.Create(ctx, &authv1.TokenReview{
		Spec: authv1.TokenReviewSpec{Token: token, Audiences: []string{NodeTokenAudience}},
	}, metav1.CreateOptions{})
	if err != nil || review == nil || !review.Status.Authenticated || review.Status.Error != "" ||
		len(review.Status.Audiences) != 1 || review.Status.Audiences[0] != NodeTokenAudience {
		return authenticatedNode{}, ErrNodeDenied
	}
	user := review.Status.User
	if user.Username != "system:serviceaccount:"+a.Namespace+":"+a.ServiceAccount || user.UID == "" {
		return authenticatedNode{}, ErrNodeDenied
	}
	podName := boundClaim(user, "pod-name")
	podUID := types.UID(boundClaim(user, "pod-uid"))
	nodeName := boundClaim(user, "node-name")
	nodeUID := types.UID(boundClaim(user, "node-uid"))
	if len(validation.IsDNS1123Subdomain(podName)) != 0 || len(validation.IsDNS1123Subdomain(nodeName)) != 0 ||
		!component.MatchString(string(podUID)) || !component.MatchString(string(nodeUID)) {
		return authenticatedNode{}, ErrNodeDenied
	}
	key := client.ObjectKey{Namespace: a.Namespace, Name: podName}
	var pod corev1.Pod
	if err := a.Reader.Get(ctx, key, &pod); err != nil || pod.UID != podUID || !runningPod(&pod) ||
		pod.Spec.NodeName != nodeName || pod.Spec.ServiceAccountName != a.ServiceAccount {
		return authenticatedNode{}, ErrNodeDenied
	}
	owner := metav1.GetControllerOfNoCopy(&pod)
	if owner == nil || owner.APIVersion != appsv1.SchemeGroupVersion.String() || owner.Kind != "DaemonSet" ||
		owner.Name != a.DaemonSet || owner.UID == "" {
		return authenticatedNode{}, ErrNodeDenied
	}
	var daemon appsv1.DaemonSet
	key.Name = a.DaemonSet
	if err := a.Reader.Get(ctx, key, &daemon); err != nil || daemon.UID != owner.UID || daemon.DeletionTimestamp != nil ||
		daemon.Spec.Template.Spec.ServiceAccountName != a.ServiceAccount {
		return authenticatedNode{}, ErrNodeDenied
	}
	var account corev1.ServiceAccount
	key.Name = a.ServiceAccount
	if err := a.Reader.Get(ctx, key, &account); err != nil || string(account.UID) != user.UID || account.DeletionTimestamp != nil ||
		!liveNode(ctx, a.Reader, nodeName, nodeUID) {
		return authenticatedNode{}, ErrNodeDenied
	}
	return authenticatedNode{name: nodeName, uid: nodeUID}, nil
}

func boundClaim(user authv1.UserInfo, name string) string {
	values := user.Extra["authentication.kubernetes.io/"+name]
	if len(values) != 1 {
		return ""
	}
	return values[0]
}
