package libsql

import (
 "context"
 "strings"
 "testing"
)

func TestDeleteConversationDeletesDependentsInForeignKeyOrder(t *testing.T) {
 server, queries, args := newRepositoryTestServer(t)
 defer server.Close()
 repo := NewRepository(NewClient(server.URL, "").WithHTTPClient(server.Client()), "user-1")
 if err := repo.DeleteConversation(context.Background(), "conversation-1"); err != nil { t.Fatal(err) }
 if len(*queries) != 7 { t.Fatalf("queries=%d want 7",len(*queries)) }
 expected := []string{"approvals","events","runs","messages","conversation_repositories","conversations"}
 for i, table := range expected {
  query := (*queries)[i+1]
  if !strings.HasPrefix(query, "DELETE FROM "+table) { t.Errorf("query %d=%q expected %s", i+1,query,table) }
  if !strings.Contains(query,"user_id = ?") { t.Errorf("unscoped delete: %s",query) }
  if !containsTextValue((*args)[i+1],"user-1") { t.Errorf("missing user guard for %s",table) }
 }
}
