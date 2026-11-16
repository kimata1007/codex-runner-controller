package kubernetes

import (
 "context"
 "testing"

 v1alpha1 "github.com/kimata1007/codex-runner-controller/api/v1alpha1"
 metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
 "k8s.io/apimachinery/pkg/runtime"
 "sigs.k8s.io/controller-runtime/pkg/client"
 "sigs.k8s.io/controller-runtime/pkg/client/fake"
 apierrors "k8s.io/apimachinery/pkg/api/errors"
)

func TestDeleteSessionOnlyDeletesMatchingConversation(t *testing.T) {
 scheme := runtime.NewScheme()
 if err:=AddToScheme(scheme);err!=nil { t.Fatal(err) }
 existing := &v1alpha1.CodexSession{ObjectMeta:metav1.ObjectMeta{Name:"conversation-1",Namespace:"codex-system"},Spec:v1alpha1.CodexSessionSpec{ConversationID:"conversation-1"}}
 kube := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existing).Build()
 activator := &SessionActivator{Client:kube}
 if err:=activator.DeleteSession(context.Background(),"conversation-1");err!=nil { t.Fatal(err) }
 err:=kube.Get(context.Background(),client.ObjectKeyFromObject(existing),&v1alpha1.CodexSession{})
 if !apierrors.IsNotFound(err) { t.Fatalf("session still exists: %v",err) }
 if err:=activator.DeleteSession(context.Background(),"conversation-1");err!=nil { t.Fatal(err) }
}

func TestDeleteSessionRejectsDifferentConversationIdentity(t *testing.T) {
 scheme:=runtime.NewScheme()
 if err:=AddToScheme(scheme);err!=nil { t.Fatal(err) }
 obj:=&v1alpha1.CodexSession{ObjectMeta:metav1.ObjectMeta{Name:"conversation-1",Namespace:"codex-system"},Spec:v1alpha1.CodexSessionSpec{ConversationID:"other-conversation"}}
 kube:=fake.NewClientBuilder().WithScheme(scheme).WithObjects(obj).Build()
 activator:=&SessionActivator{Client:kube}
 if err:=activator.DeleteSession(context.Background(),"conversation-1");err==nil { t.Fatal("deletion succeeded for mismatched identity") }
 if err:=kube.Get(context.Background(),client.ObjectKeyFromObject(obj),&v1alpha1.CodexSession{});err!=nil { t.Fatalf("incorrect session was deleted: %v",err) }
}

func TestDeleteSessionRespectsCancelledContext(t *testing.T) {
 scheme:=runtime.NewScheme()
 if err:=AddToScheme(scheme);err!=nil { t.Fatal(err) }
 kube:=fake.NewClientBuilder().WithScheme(scheme).Build()
 activator:=&SessionActivator{Client:kube}
 ctx,cancel:=context.WithCancel(context.Background())
 cancel()
 if err:=activator.DeleteSession(ctx,"conversation-1");err==nil { t.Fatal("expected cancellation error") }
}
