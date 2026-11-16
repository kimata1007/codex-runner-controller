package libsql

import (
 "context"
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "strings"
 "testing"
)

func batchSuccessResponse(size int, failAt int) pipelineResponse {
 results := make([]json.RawMessage,size)
 errors := make([]json.RawMessage,size)
 for i:=0;i<size;i++ {
  results[i]=json.RawMessage(`null`)
  errors[i]=json.RawMessage(`null`)
 }
 // BEGIN succeeds; each dependent statement succeeds up to any injected failure.
 for i:=0;i<size-2;i++ {
  if failAt>=0 && i>=failAt { break }
  results[i]=json.RawMessage(`{"cols":[],"rows":[],"affected_row_count":1}`)
 }
 if failAt>=0 {
  errors[failAt]=json.RawMessage(`{"message":"injected constraint violation"}`)
  // The final step must execute ROLLBACK.
  results[size-1]=json.RawMessage(`{"cols":[],"rows":[],"affected_row_count":0}`)
 }else{
  results[size-2]=json.RawMessage(`{"cols":[],"rows":[],"affected_row_count":0}`)
 }
 inner,_:=json.Marshal(map[string]any{"type":"batch","result":map[string]any{"step_results":results,"step_errors":errors}})
 return pipelineResponse{Results:[]streamResult{{Type:"ok",Response:inner}}}
}

func TestDeleteConversationUsesOneAtomicBatch(t *testing.T) {
 count:=0
 server:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  count++
  var req pipelineRequest
  if err:=json.NewDecoder(r.Body).Decode(&req);err!=nil { t.Errorf("decode: %v",err);return }
  if count==1 {
   if len(req.Requests)!=1 || req.Requests[0].Stmt==nil || !strings.Contains(req.Requests[0].Stmt.SQL,"user_id = ?") { t.Errorf("missing ownership check: %+v",req.Requests) }
   result:=json.RawMessage(`{"type":"execute","result":{"cols":[],"rows":[[{"type":"text","value":"conversation-1"}]],"affected_row_count":0}}`)
   _=json.NewEncoder(w).Encode(pipelineResponse{Results:[]streamResult{{Type:"ok",Response:result}}})
   return
  }
  if count!=2 { t.Errorf("expected one atomic batch, got request %d",count);return }
  if len(req.Requests)!=1 || req.Requests[0].Type!="batch" || req.Requests[0].Batch==nil { t.Errorf("missing batch request: %+v",req.Requests);return }
  steps:=req.Requests[0].Batch.Steps
  if len(steps)!=9 { t.Errorf("step count = %d want 9",len(steps));return }
  if steps[0].Stmt.SQL!="BEGIN IMMEDIATE" || steps[7].Stmt.SQL!="COMMIT" || steps[8].Stmt.SQL!="ROLLBACK" { t.Errorf("transaction boundaries not found") }
  expected:=[]string{"approvals","events","runs","messages","conversation_repositories","conversations"}
  for i,table:=range expected {
   step:=steps[i+1]
   if !strings.HasPrefix(step.Stmt.SQL,"DELETE FROM "+table) { t.Errorf("step %d = %q",i,step.Stmt.SQL) }
   if !strings.Contains(step.Stmt.SQL,"user_id = ?") { t.Errorf("unscoped delete: %s",step.Stmt.SQL) }
   if len(step.Stmt.Args)!=3 || step.Stmt.Args[2].Value!="user-1" { t.Errorf("incorrect ownership args: %+v",step.Stmt.Args) }
   if step.Condition==nil || step.Condition.Type!="ok" || step.Condition.Step!=i { t.Errorf("step %d not conditioned on previous success",i+1) }
  }
  if steps[7].Condition==nil || steps[7].Condition.Type!="ok" || steps[7].Condition.Step!=6 { t.Error("COMMIT not gated on last DELETE") }
  if steps[8].Condition==nil || steps[8].Condition.Type!="and" || len(steps[8].Condition.Conds)!=2 { t.Error("ROLLBACK not gated on failed commit") }
  _=json.NewEncoder(w).Encode(batchSuccessResponse(len(steps),-1))
 }))
 defer server.Close()
 repo:=NewRepository(NewClient(server.URL,"").WithHTTPClient(server.Client()),"user-1")
 if err:=repo.DeleteConversation(context.Background(),"conversation-1");err!=nil { t.Fatal(err) }
 if count!=2 { t.Fatalf("request count=%d want 2",count) }
}

func TestDeleteConversationStatementFailureReturnsErrorAndRollback(t *testing.T) {
 calls:=0
 server:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  calls++
  var req pipelineRequest
  if err:=json.NewDecoder(r.Body).Decode(&req);err!=nil { t.Error(err);return }
  if calls==1 {
   data:=json.RawMessage(`{"type":"execute","result":{"cols":[],"rows":[[{"type":"text","value":"conversation-1"}]],"affected_row_count":0}}`)
   _=json.NewEncoder(w).Encode(pipelineResponse{Results:[]streamResult{{Type:"ok",Response:data}}})
   return
  }
  steps:=req.Requests[0].Batch.Steps
  _=json.NewEncoder(w).Encode(batchSuccessResponse(len(steps),4))
 }))
 defer server.Close()
 repo:=NewRepository(NewClient(server.URL,"").WithHTTPClient(server.Client()),"user-1")
 err:=repo.DeleteConversation(context.Background(),"conversation-1")
 if err==nil { t.Fatal("delete unexpectedly succeeded") }
 if !strings.Contains(err.Error(),"transaction step 4 failed") { t.Fatalf("failure not surfaced: %v",err) }
 if calls!=2 { t.Fatalf("request count=%d want 2",calls) }
}

func TestDeleteConversationUnknownOwnerDoesNotRunBatch(t *testing.T) {
 calls:=0
 server:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  calls++
  result:=json.RawMessage(`{"type":"execute","result":{"cols":[],"rows":[],"affected_row_count":0}}`)
  _=json.NewEncoder(w).Encode(pipelineResponse{Results:[]streamResult{{Type:"ok",Response:result}}})
 }))
 defer server.Close()
 repo:=NewRepository(NewClient(server.URL,"").WithHTTPClient(server.Client()),"another-user")
 if err:=repo.DeleteConversation(context.Background(),"conversation-1");err==nil { t.Fatal("missing conversation unexpectedly deleted") }
 if calls!=1 { t.Fatalf("unexpected delete batch: %d calls",calls) }
}
