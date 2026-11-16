package runnerapi

import (
 "context"
 "net/http"
 "testing"

 "github.com/kimata1007/codex-runner-controller/internal/conversation"
 "github.com/kimata1007/codex-runner-controller/internal/memory"
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
