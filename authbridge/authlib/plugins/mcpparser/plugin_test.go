package mcpparser

import (
	"context"
	"testing"

	"github.com/kagenti/kagenti-extensions/authbridge/authlib/pipeline"
)

func TestMCPParser_Capabilities(t *testing.T) {
	p := NewMCPParser()

	if p.Name() != "mcp-parser" {
		t.Errorf("Name() = %q, want %q", p.Name(), "mcp-parser")
	}

	caps := p.Capabilities()
	if !caps.ReadsBody {
		t.Error("ReadsBody should be true")
	}
	if len(caps.Writes) != 1 || caps.Writes[0] != "mcp" {
		t.Errorf("Writes = %v, want [mcp]", caps.Writes)
	}
}

func TestMCPParser_ToolCall(t *testing.T) {
	p := NewMCPParser()
	pctx := &pipeline.Context{
		Body: []byte(`{"jsonrpc":"2.0","method":"tools/call","id":1,"params":{"name":"get_weather","arguments":{"city":"NYC"}}}`),
	}

	action := p.OnRequest(context.Background(), pctx)
	if action.Type != pipeline.Continue {
		t.Fatalf("expected Continue, got %v", action.Type)
	}
	if pctx.Extensions.MCP == nil {
		t.Fatal("Extensions.MCP is nil")
	}
	if pctx.Extensions.MCP.Method != "tools/call" {
		t.Errorf("Method = %q, want %q", pctx.Extensions.MCP.Method, "tools/call")
	}
	if pctx.Extensions.MCP.RPCID != float64(1) {
		t.Errorf("RPCID = %v, want 1", pctx.Extensions.MCP.RPCID)
	}
	if pctx.Extensions.MCP.Params["name"] != "get_weather" {
		t.Errorf("Params[name] = %v, want %q", pctx.Extensions.MCP.Params["name"], "get_weather")
	}
	args, ok := pctx.Extensions.MCP.Params["arguments"].(map[string]any)
	if !ok {
		t.Fatal("Params[arguments] is not a map")
	}
	if args["city"] != "NYC" {
		t.Errorf("Params[arguments][city] = %v, want %q", args["city"], "NYC")
	}
}

func TestMCPParser_ResourceRead(t *testing.T) {
	p := NewMCPParser()
	pctx := &pipeline.Context{
		Body: []byte(`{"jsonrpc":"2.0","method":"resources/read","id":2,"params":{"uri":"file:///tmp/data.csv"}}`),
	}

	action := p.OnRequest(context.Background(), pctx)
	if action.Type != pipeline.Continue {
		t.Fatalf("expected Continue, got %v", action.Type)
	}
	if pctx.Extensions.MCP == nil {
		t.Fatal("Extensions.MCP is nil")
	}
	if pctx.Extensions.MCP.Method != "resources/read" {
		t.Errorf("Method = %q, want %q", pctx.Extensions.MCP.Method, "resources/read")
	}
	if pctx.Extensions.MCP.Params["uri"] != "file:///tmp/data.csv" {
		t.Errorf("Params[uri] = %v, want %q", pctx.Extensions.MCP.Params["uri"], "file:///tmp/data.csv")
	}
}

func TestMCPParser_PromptGet(t *testing.T) {
	p := NewMCPParser()
	pctx := &pipeline.Context{
		Body: []byte(`{"jsonrpc":"2.0","method":"prompts/get","id":3,"params":{"name":"summarize","arguments":{"style":"brief"}}}`),
	}

	action := p.OnRequest(context.Background(), pctx)
	if action.Type != pipeline.Continue {
		t.Fatalf("expected Continue, got %v", action.Type)
	}
	if pctx.Extensions.MCP == nil {
		t.Fatal("Extensions.MCP is nil")
	}
	if pctx.Extensions.MCP.Method != "prompts/get" {
		t.Errorf("Method = %q, want %q", pctx.Extensions.MCP.Method, "prompts/get")
	}
	if pctx.Extensions.MCP.Params["name"] != "summarize" {
		t.Errorf("Params[name] = %v, want %q", pctx.Extensions.MCP.Params["name"], "summarize")
	}
	args, ok := pctx.Extensions.MCP.Params["arguments"].(map[string]any)
	if !ok {
		t.Fatal("Params[arguments] is not a map")
	}
	if args["style"] != "brief" {
		t.Errorf("Params[arguments][style] = %v, want %q", args["style"], "brief")
	}
}

func TestMCPParser_AnyMethod(t *testing.T) {
	p := NewMCPParser()
	pctx := &pipeline.Context{
		Body: []byte(`{"jsonrpc":"2.0","method":"notifications/initialized","id":4}`),
	}

	action := p.OnRequest(context.Background(), pctx)
	if action.Type != pipeline.Continue {
		t.Fatalf("expected Continue, got %v", action.Type)
	}
	if pctx.Extensions.MCP == nil {
		t.Fatal("Extensions.MCP is nil")
	}
	if pctx.Extensions.MCP.Method != "notifications/initialized" {
		t.Errorf("Method = %q, want %q", pctx.Extensions.MCP.Method, "notifications/initialized")
	}
}

func TestMCPParser_NilBody(t *testing.T) {
	p := NewMCPParser()
	pctx := &pipeline.Context{Body: nil}

	action := p.OnRequest(context.Background(), pctx)
	if action.Type != pipeline.Continue {
		t.Fatalf("expected Continue, got %v", action.Type)
	}
	if pctx.Extensions.MCP != nil {
		t.Error("Extensions.MCP should be nil when body is nil")
	}
}

func TestMCPParser_EmptyBody(t *testing.T) {
	p := NewMCPParser()
	pctx := &pipeline.Context{Body: []byte{}}

	action := p.OnRequest(context.Background(), pctx)
	if action.Type != pipeline.Continue {
		t.Fatalf("expected Continue, got %v", action.Type)
	}
	if pctx.Extensions.MCP != nil {
		t.Error("Extensions.MCP should be nil when body is empty")
	}
}

// Regression: an OpenAI chat/completions body is valid JSON but not
// JSON-RPC. Before the fix, mcp-parser ran first in the outbound pipeline,
// Unmarshal'd the body into a zero-value jsonRPCRequest, and attached an
// empty MCPExtension{Method:""} to every inference request/response —
// polluting the session store with a phantom "mcp: {}" on each event.
func TestMCPParser_SkipsJSONThatIsNotJSONRPC(t *testing.T) {
	p := NewMCPParser()
	pctx := &pipeline.Context{
		Body: []byte(`{"model":"gpt-4","messages":[{"role":"user","content":"hi"}]}`),
	}
	action := p.OnRequest(context.Background(), pctx)
	if action.Type != pipeline.Continue {
		t.Fatalf("expected Continue, got %v", action.Type)
	}
	if pctx.Extensions.MCP != nil {
		t.Errorf("MCP should remain nil for non-JSON-RPC JSON, got %+v", pctx.Extensions.MCP)
	}
}

func TestMCPParser_InvalidJSON(t *testing.T) {
	p := NewMCPParser()
	pctx := &pipeline.Context{Body: []byte("not json")}

	action := p.OnRequest(context.Background(), pctx)
	if action.Type != pipeline.Continue {
		t.Fatalf("expected Continue, got %v", action.Type)
	}
	if pctx.Extensions.MCP != nil {
		t.Error("Extensions.MCP should be nil for invalid JSON")
	}
}

func TestMCPParser_MissingParams(t *testing.T) {
	p := NewMCPParser()
	pctx := &pipeline.Context{
		Body: []byte(`{"jsonrpc":"2.0","method":"tools/call","id":5}`),
	}

	action := p.OnRequest(context.Background(), pctx)
	if action.Type != pipeline.Continue {
		t.Fatalf("expected Continue, got %v", action.Type)
	}
	if pctx.Extensions.MCP == nil {
		t.Fatal("Extensions.MCP is nil")
	}
	if pctx.Extensions.MCP.Method != "tools/call" {
		t.Errorf("Method = %q, want %q", pctx.Extensions.MCP.Method, "tools/call")
	}
	if pctx.Extensions.MCP.Params != nil {
		t.Errorf("Params = %v, want nil when params not present", pctx.Extensions.MCP.Params)
	}
}

func TestMCPParser_OnResponse_NoRequestContext(t *testing.T) {
	// Request phase didn't run (no MCP extension populated): OnResponse
	// stays silent — no Invocation recorded — so the response event
	// doesn't appear as an MCP row at all.
	p := NewMCPParser()
	pctx := &pipeline.Context{
		Direction:    pipeline.Outbound,
		ResponseBody: []byte(`{"jsonrpc":"2.0","id":1,"result":{"tools":[]}}`),
	}
	action := p.OnResponse(context.Background(), pctx)
	if action.Type != pipeline.Continue {
		t.Fatalf("expected Continue, got %v", action.Type)
	}
	if pctx.Extensions.MCP != nil {
		t.Error("MCP extension should remain nil when request was not parsed")
	}
	if pctx.Extensions.Invocations != nil &&
		(len(pctx.Extensions.Invocations.Inbound)+len(pctx.Extensions.Invocations.Outbound)) > 0 {
		t.Errorf("non-MCP response should not record any Invocation; got %+v",
			pctx.Extensions.Invocations)
	}
}

// TestMCPParser_OnResponse_EmptyBody is the regression test for the
// notifications/initialized pairing bug: when the request side parsed
// the message (Extensions.MCP populated) but the response body is empty
// (HTTP 202 ack), the parser must record a Skip so abctl can pair the
// response row with the request row in the events timeline.
func TestMCPParser_OnResponse_EmptyBody(t *testing.T) {
	p := NewMCPParser()
	pctx := &pipeline.Context{
		Direction:  pipeline.Outbound,
		Extensions: pipeline.Extensions{MCP: &pipeline.MCPExtension{Method: "notifications/initialized"}},
	}
	action := p.OnResponse(context.Background(), pctx)
	if action.Type != pipeline.Continue {
		t.Fatalf("expected Continue, got %v", action.Type)
	}
	if pctx.Extensions.MCP.Result != nil {
		t.Error("Result should remain nil when response body is empty")
	}
	if pctx.Extensions.Invocations == nil {
		t.Fatal("expected a Skip Invocation, got none")
	}
	invs := pctx.Extensions.Invocations.Outbound
	if len(invs) != 1 {
		t.Fatalf("expected 1 Invocation, got %d", len(invs))
	}
	if invs[0].Action != pipeline.ActionSkip {
		t.Errorf("Action = %q, want skip", invs[0].Action)
	}
	if invs[0].Reason != "no_response_body" {
		t.Errorf("Reason = %q, want no_response_body", invs[0].Reason)
	}
}

func TestMCPParser_OnResponse_ToolsList(t *testing.T) {
	p := NewMCPParser()
	pctx := &pipeline.Context{
		Extensions:   pipeline.Extensions{MCP: &pipeline.MCPExtension{Method: "tools/list"}},
		ResponseBody: []byte(`{"jsonrpc":"2.0","id":1,"result":{"tools":[{"name":"get_weather"},{"name":"get_news"}]}}`),
	}

	action := p.OnResponse(context.Background(), pctx)
	if action.Type != pipeline.Continue {
		t.Fatalf("expected Continue, got %v", action.Type)
	}
	if pctx.Extensions.MCP.Result == nil {
		t.Fatal("Result should be populated")
	}
	tools, ok := pctx.Extensions.MCP.Result["tools"].([]any)
	if !ok {
		t.Fatalf("Result[tools] should be []any, got %T", pctx.Extensions.MCP.Result["tools"])
	}
	if len(tools) != 2 {
		t.Errorf("expected 2 tools, got %d", len(tools))
	}
}

func TestMCPParser_OnResponse_ToolsCall(t *testing.T) {
	p := NewMCPParser()
	pctx := &pipeline.Context{
		Extensions:   pipeline.Extensions{MCP: &pipeline.MCPExtension{Method: "tools/call"}},
		ResponseBody: []byte(`{"jsonrpc":"2.0","id":2,"result":{"content":[{"type":"text","text":"sunny, 72F"}]}}`),
	}

	action := p.OnResponse(context.Background(), pctx)
	if action.Type != pipeline.Continue {
		t.Fatalf("expected Continue, got %v", action.Type)
	}
	if pctx.Extensions.MCP.Result == nil {
		t.Fatal("Result should be populated")
	}
	if _, ok := pctx.Extensions.MCP.Result["content"]; !ok {
		t.Error("Result should contain content key")
	}
}

func TestMCPParser_OnResponse_Error(t *testing.T) {
	p := NewMCPParser()
	pctx := &pipeline.Context{
		Extensions:   pipeline.Extensions{MCP: &pipeline.MCPExtension{Method: "tools/call"}},
		ResponseBody: []byte(`{"jsonrpc":"2.0","id":3,"error":{"code":-32601,"message":"Method not found"}}`),
	}

	action := p.OnResponse(context.Background(), pctx)
	if action.Type != pipeline.Continue {
		t.Fatalf("expected Continue, got %v", action.Type)
	}
	if pctx.Extensions.MCP.Result != nil {
		t.Errorf("Result should be nil on error response, got %v", pctx.Extensions.MCP.Result)
	}
	if pctx.Extensions.MCP.Err == nil {
		t.Fatal("Err should be populated")
	}
	if pctx.Extensions.MCP.Err.Code != -32601 {
		t.Errorf("Err.Code = %d, want -32601", pctx.Extensions.MCP.Err.Code)
	}
	if pctx.Extensions.MCP.Err.Message != "Method not found" {
		t.Errorf("Err.Message = %q, want %q", pctx.Extensions.MCP.Err.Message, "Method not found")
	}
}

func TestMCPParser_OnResponse_InvalidJSON(t *testing.T) {
	p := NewMCPParser()
	pctx := &pipeline.Context{
		Extensions:   pipeline.Extensions{MCP: &pipeline.MCPExtension{Method: "tools/list"}},
		ResponseBody: []byte("not json"),
	}

	action := p.OnResponse(context.Background(), pctx)
	if action.Type != pipeline.Continue {
		t.Fatalf("expected Continue, got %v", action.Type)
	}
	if pctx.Extensions.MCP.Result != nil {
		t.Error("Result should remain nil for invalid JSON")
	}
}

func TestMCPParser_OnResponse_SSE(t *testing.T) {
	// MCP's Streamable HTTP transport returns SSE (event: / data: lines)
	// instead of plain JSON-RPC when the client sends Accept: text/event-stream.
	p := NewMCPParser()
	body := "event: message\r\n" +
		"data: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"tools\":[{\"name\":\"get_weather\"}]}}\r\n\r\n"
	pctx := &pipeline.Context{
		Extensions:   pipeline.Extensions{MCP: &pipeline.MCPExtension{Method: "tools/list"}},
		ResponseBody: []byte(body),
	}

	action := p.OnResponse(context.Background(), pctx)
	if action.Type != pipeline.Continue {
		t.Fatalf("expected Continue, got %v", action.Type)
	}
	if pctx.Extensions.MCP.Result == nil {
		t.Fatal("Result should be populated from SSE data frame")
	}
	if _, ok := pctx.Extensions.MCP.Result["tools"]; !ok {
		t.Error("Result should contain tools from SSE data")
	}
}

func TestMCPParser_OnResponse_SSE_Error(t *testing.T) {
	p := NewMCPParser()
	body := "event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":1,\"error\":{\"code\":-32601,\"message\":\"not found\"}}\n\n"
	pctx := &pipeline.Context{
		Extensions:   pipeline.Extensions{MCP: &pipeline.MCPExtension{Method: "tools/call"}},
		ResponseBody: []byte(body),
	}

	_ = p.OnResponse(context.Background(), pctx)
	if pctx.Extensions.MCP.Err == nil {
		t.Fatal("expected Err populated from SSE error frame")
	}
	if pctx.Extensions.MCP.Err.Message != "not found" {
		t.Errorf("Err.Message = %q, want %q", pctx.Extensions.MCP.Err.Message, "not found")
	}
}

func TestMCPParser_OnResponse_SSE_SkipsMalformedFramesUntilGoodOne(t *testing.T) {
	// A broken upstream emits a garbage data: line before the valid one.
	// Malformed frames should be logged at DEBUG, not abort parsing.
	p := NewMCPParser()
	body := "data: not-json\n\n" +
		"data: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"ok\":true}}\n\n"
	pctx := &pipeline.Context{
		Extensions:   pipeline.Extensions{MCP: &pipeline.MCPExtension{Method: "tools/list"}},
		ResponseBody: []byte(body),
	}
	_ = p.OnResponse(context.Background(), pctx)
	if pctx.Extensions.MCP.Result == nil || pctx.Extensions.MCP.Result["ok"] != true {
		t.Errorf("expected result from second SSE frame, got %v", pctx.Extensions.MCP.Result)
	}
}
