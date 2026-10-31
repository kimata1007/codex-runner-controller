package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var GroupVersion = schema.GroupVersion{Group: "codex.example.com", Version: "v1alpha1"}

type CodexSession struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   CodexSessionSpec   `json:"spec,omitempty"`
	Status CodexSessionStatus `json:"status,omitempty"`
}

type CodexSessionList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CodexSession `json:"items"`
}

func (in *CodexSession) DeepCopyInto(out *CodexSession) {
	*out = *in
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	if in.Spec.Repository != nil {
		out.Spec.Repository = &Repository{URL: in.Spec.Repository.URL, Revision: in.Spec.Repository.Revision}
	}
	if in.Spec.CredentialsRef != nil {
		out.Spec.CredentialsRef = &SecretReference{Name: in.Spec.CredentialsRef.Name}
	}
}

func (in *CodexSession) DeepCopy() *CodexSession {
	if in == nil {
		return nil
	}
	out := new(CodexSession)
	in.DeepCopyInto(out)
	return out
}

func (in *CodexSession) DeepCopyObject() runtime.Object {
	return in.DeepCopy()
}

func (in *CodexSessionList) DeepCopyInto(out *CodexSessionList) {
	*out = *in
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		out.Items = make([]CodexSession, len(in.Items))
		for i := range in.Items {
			in.Items[i].DeepCopyInto(&out.Items[i])
		}
	}
}

func (in *CodexSessionList) DeepCopy() *CodexSessionList {
	if in == nil {
		return nil
	}
	out := new(CodexSessionList)
	in.DeepCopyInto(out)
	return out
}

func (in *CodexSessionList) DeepCopyObject() runtime.Object {
	return in.DeepCopy()
}

func AddToScheme(scheme *runtime.Scheme) error {
	scheme.AddKnownTypes(GroupVersion, &CodexSession{}, &CodexSessionList{})
	metav1.AddToGroupVersion(scheme, GroupVersion)
	return nil
}
