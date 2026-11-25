package v1alpha1

import (
	"slices"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
)

// SessionPolicy is the controller's authorization for one scheduled Pod UID.
// Its name is the Pod UID. The policy lives in the trusted control namespace.
type SessionPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              SessionPolicySpec `json:"spec"`
}

type SessionPolicySpec struct {
	Workload  SessionWorkload `json:"workload"`
	Version   int64           `json:"version"`
	ExpiresAt metav1.Time     `json:"expiresAt"`
	Revoked   bool            `json:"revoked"`
	Grants    []NetworkGrant  `json:"grants"`
	Model     *ModelPolicy    `json:"model,omitempty"`
}

type ModelPolicy struct {
	BudgetMicros      int64        `json:"budgetMicros"`
	RequestsPerMinute int64        `json:"requestsPerMinute"`
	Models            []ModelGrant `json:"models"`
}

type ModelGrant struct {
	Name                   string `json:"name"`
	MaxInputBytes          int64  `json:"maxInputBytes"`
	MaxOutputTokens        int64  `json:"maxOutputTokens"`
	InputMicrosPerMillion  int64  `json:"inputMicrosPerMillion"`
	OutputMicrosPerMillion int64  `json:"outputMicrosPerMillion"`
}

// SessionWorkload contains immutable references to the enrolled resources.
type SessionWorkload struct {
	SessionName        string    `json:"sessionName"`
	SessionUID         types.UID `json:"sessionUID"`
	PodName            string    `json:"podName"`
	PodUID             types.UID `json:"podUID"`
	NodeName           string    `json:"nodeName"`
	NodeUID            types.UID `json:"nodeUID"`
	ServiceAccountName string    `json:"serviceAccountName"`
	ServiceAccountUID  types.UID `json:"serviceAccountUID"`
}

type NetworkGrant struct {
	Source      string `json:"source"`
	Destination string `json:"destination"`
	Protocol    string `json:"protocol"`
	Port        uint16 `json:"port"`
}

type SessionPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SessionPolicy `json:"items"`
}

func (in *SessionPolicy) DeepCopyObject() runtime.Object {
	if in == nil {
		return nil
	}
	out := new(SessionPolicy)
	*out = *in
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	out.Spec.Grants = slices.Clone(in.Spec.Grants)
	if in.Spec.Model != nil {
		model := *in.Spec.Model
		model.Models = slices.Clone(model.Models)
		out.Spec.Model = &model
	}
	return out
}

func (in *SessionPolicyList) DeepCopyObject() runtime.Object {
	if in == nil {
		return nil
	}
	out := new(SessionPolicyList)
	*out = *in
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		out.Items = make([]SessionPolicy, len(in.Items))
		for i := range in.Items {
			out.Items[i] = *in.Items[i].DeepCopyObject().(*SessionPolicy)
		}
	}
	return out
}
