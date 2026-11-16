package runnerapi

import (
 "context"
 "net/http"
 "errors"
 "fmt"
 "testing"

 "github.com/kimata1007/codex-runner-controller/internal/conversation"
 "github.com/kimata1007/codex-runner-controller/internal/memory"
 "github.com/kimata1007/codex-runner-controller/internal/session"
)

func TestDeleteConversationRemovesHistory(t *testing.T) {
 repo := memory.NewRepository()
 value, err := repo.CreateConversation(context.Background())
 if err != nil { t.Fatal(err) }
 if _, _, err := repo.CreateMessage(context.Background(), value.ID, conversation.NewMessage{Content:"hello"}); err != nil { t.Fatal(err) }
 router := NewRouter(NewHandler(HandlerDependencies{Repository:repo}))
 response := serveJSON(t, router, http.MethodDelete, "/v1/conversations/"+value.ID, "")
 if response.Code != http.StatusNoContent { t.Fatalf("status=%d body=%s",response.Code,response.Body.String()) }
 if response := serveJSON(t,router,http.MethodGet,"/v1/conversations/"+value.ID,""); response.Code != http.StatusNotFound {
  t.Fatalf("deleted conversation still exists: %d", response.Code)
 }
 if response := serveJSON(t,router,http.MethodDelete,"/v1/conversations/"+value.ID,""); response.Code != http.StatusNotFound {
  t.Fatalf("second delete returned %d",response.Code)
 }
}

func TestDeleteConversationMissingReturnsNotFound(t *testing.T) {
 response := serveJSON(t,newTestRouter(),http.MethodDelete,"/v1/conversations/unknown","")
 if response.Code != http.StatusNotFound { t.Fatalf("status=%d",response.Code) }
}

type failingSessionCleaner struct {
 err error
 deleteCalls int
}

func (s *failingSessionCleaner) Activate(context.Context, session.Activation) error { return nil }
func (s *failingSessionCleaner) DeleteSession(context.Context,string) error {
 s.deleteCalls++
 return s.err
}

type failedDeleteRepository struct {
 *memory.Repository
 err error
 calls int
}

func (r *failedDeleteRepository) DeleteConversation(_ context.Context, _ string) error {
 r.calls++
 return r.err
}

func TestDeleteConversationSessionFailurePreservesHistory(t *testing.T) {
 repo:=memory.NewRepository()
 conversationValue,err:=repo.CreateConversation(context.Background())
 if err!=nil { t.Fatal(err) }
 cleaner:=&failingSessionCleaner{err:errors.New("injected Kubernetes failure")}
 router:=NewRouter(NewHandler(HandlerDependencies{Repository:repo,Activator:cleaner}))
 response:=serveJSON(t,router,http.MethodDelete,"/v1/conversations/"+conversationValue.ID,"")
 if response.Code!=http.StatusServiceUnavailable { t.Fatalf("status=%d",response.Code) }
 if cleaner.deleteCalls!=1 { t.Fatalf("Kubernetes delete calls=%d",cleaner.deleteCalls) }
 if _,err:=repo.GetConversation(context.Background(),conversationValue.ID);err!=nil { t.Fatalf("history was lost: %v",err) }
}

func TestDeleteConversationDBFailureReturnsErrorAndKeepsHistory(t *testing.T) {
 base:=memory.NewRepository()
 value,err:=base.CreateConversation(context.Background())
 if err!=nil { t.Fatal(err) }
 repo:=&failedDeleteRepository{Repository:base,err:fmt.Errorf("injected transaction failure")}
 router:=NewRouter(NewHandler(HandlerDependencies{Repository:repo}))
 response:=serveJSON(t,router,http.MethodDelete,"/v1/conversations/"+value.ID,"")
 if response.Code!=http.StatusInternalServerError { t.Fatalf("status=%d",response.Code) }
 if repo.calls!=1 { t.Fatalf("delete calls=%d",repo.calls) }
 if _,err:=base.GetConversation(context.Background(),value.ID);err!=nil { t.Fatalf("history was lost: %v",err) }
}

func TestDeleteConversationDoesNotDeleteUnknownSession(t *testing.T) {
 repo:=memory.NewRepository()
 cleaner:=&failingSessionCleaner{}
 router:=NewRouter(NewHandler(HandlerDependencies{Repository:repo,Activator:cleaner}))
 response:=serveJSON(t,router,http.MethodDelete,"/v1/conversations/nonexistent","")
 if response.Code!=http.StatusNotFound { t.Fatalf("status=%d",response.Code) }
 if cleaner.deleteCalls!=0 { t.Fatalf("unexpected session delete calls=%d",cleaner.deleteCalls) }
}
