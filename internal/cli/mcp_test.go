package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPProcess(t *testing.T) {
	if os.Getenv("JEV_MCP_TEST_PROCESS") == "1" {
		os.Exit(Run(context.Background(), []string{"mcp"}, os.Stdin, os.Stdout, os.Stderr))
	}
}
func connectMCP(t *testing.T, ctx context.Context) *mcp.ClientSession {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(executable, "-test.run=^TestMCPProcess$")
	command.Env = append(os.Environ(), "JEV_MCP_TEST_PROCESS=1", "PATH=", "JEV_PYTHON=/does/not/exist")
	session, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(ctx, &mcp.CommandTransport{Command: command}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { session.Close() })
	return session
}
func callTool(t *testing.T, ctx context.Context, s *mcp.ClientSession, name string, args any, wantError bool) object {
	t.Helper()
	result, err := s.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError != wantError {
		t.Fatalf("%s: IsError=%v, want %v: %+v", name, result.IsError, wantError, result.Content)
	}
	b, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	v, err := decode(b)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Content) != 1 || result.Content[0].(*mcp.TextContent).Text != string(b) {
		// Compare JSON semantically: transports may reorder map keys.
		var textValue any
		if err := json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &textValue); err != nil {
			t.Fatal(err)
		}
		canonicalText, _ := json.Marshal(textValue)
		if string(canonicalText) != string(b) {
			t.Fatal("text and structured output differ")
		}
	}
	return v.(object)
}
func TestMCPStdioTools(t *testing.T) {
	var calls atomic.Int32
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer test-key" || r.URL.Path != "/v1/systemone" {
			t.Error("request contract")
		}
		var body object
		json.NewDecoder(r.Body).Decode(&body)
		if body["state"] == "fail" {
			w.WriteHeader(400)
			fmt.Fprint(w, "secret-upstream")
			return
		}
		fmt.Fprint(w, `{"model":"actual","usage":{"input_tokens":1},"answers":{"urgent":{"type":"noul","noul":0.9}}}`)
	}))
	defer api.Close()
	t.Setenv("TYPESAFE_API_KEY", "test-key")
	t.Setenv("TYPESAFE_BASE_URL", api.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	session := connectMCP(t, ctx)
	tools, err := session.ListTools(ctx, nil)
	if err != nil || len(tools.Tools) != 3 {
		t.Fatal(tools, err)
	}
	names := map[string]bool{}
	for _, tool := range tools.Tools {
		names[tool.Name] = true
		data, _ := json.Marshal(tool.InputSchema)
		if !strings.Contains(string(data), `"type":"object"`) {
			t.Fatal(string(data))
		}
	}
	for _, name := range []string{"question_schema", "evaluate", "evaluate_batch"} {
		if !names[name] {
			t.Fatal(name)
		}
	}
	schema := callTool(t, ctx, session, "question_schema", object{}, false)
	if schema["$defs"] == nil {
		t.Fatal(schema)
	}
	qs, _ := questions([]byte(testQuestions))
	result := callTool(t, ctx, session, "evaluate", object{"state": object{"ticket": "登录失败"}, "questions": qs}, false)
	if result["model"] != "actual" || result["answers"] == nil {
		t.Fatal(result)
	}
	result = callTool(t, ctx, session, "evaluate", object{"state": "fail", "questions": qs, "model": "custom"}, true)
	b, _ := json.Marshal(result)
	if strings.Contains(string(b), "secret-upstream") {
		t.Fatal("leaked body")
	}
	result = callTool(t, ctx, session, "evaluate_batch", object{"records": []any{object{"id": "a", "state": "ok"}, object{"id": "b", "state": "fail"}}, "questions": qs}, false)
	rows := result["results"].([]any)
	if result["failed"] != json.Number("1") || result["status"] != "partial_failure" || len(rows) != 2 || rows[0].(object)["id"] != "a" || rows[1].(object)["error"] == nil {
		t.Fatal(result)
	}
	if rows[0].(object)["fingerprint"] != fingerprint(object{"id": "a", "state": "ok"}, qs, "jev-latest") {
		t.Fatal("fingerprint changed")
	}
	result = callTool(t, ctx, session, "evaluate_batch", object{"records": []any{object{"id": "failed", "state": "fail"}}, "questions": qs}, true)
	if result["status"] != "failed" || result["failed"] != json.Number("1") {
		t.Fatal(result)
	}
	before := calls.Load()
	for _, args := range []object{{"state": nil, "questions": qs}, {"state": "secret-input", "questions": object{}}, {"state": "x", "questions": qs, "model": 1}, {"state": "x", "questions": qs, "api_key": "secret-input"}} {
		result = callTool(t, ctx, session, "evaluate", args, true)
		b, _ := json.Marshal(result)
		if strings.Contains(string(b), "secret-input") {
			t.Fatal("leaked input")
		}
	}
	for _, rows := range []any{[]any{}, make([]any, 51), []any{object{"id": "a", "state": "x"}, object{"id": "a", "state": "y"}}, []any{object{"id": "a", "state": false}}} {
		callTool(t, ctx, session, "evaluate_batch", object{"records": rows, "questions": qs}, true)
	}
	if calls.Load() != before {
		t.Fatal("invalid inputs sent upstream")
	}
	if _, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "unknown", Arguments: object{}}); err == nil {
		t.Fatal("unknown tool accepted")
	}
}
func TestMCPSchemaWithoutCredentials(t *testing.T) {
	// An invalid env key avoids any access to the user's credential store.
	t.Setenv("TYPESAFE_API_KEY", "invalid key")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session := connectMCP(t, ctx)
	callTool(t, ctx, session, "question_schema", nil, false)
	qs, _ := questions([]byte(testQuestions))
	callTool(t, ctx, session, "evaluate", object{"state": "x", "questions": qs}, true)
}
func TestMCPCancellation(t *testing.T) {
	started := make(chan struct{})
	stopped := make(chan struct{})
	release := make(chan struct{})
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		close(started)
		select {
		case <-r.Context().Done():
			close(stopped)
		case <-release:
		}
	}))
	defer api.Close()
	defer close(release)
	t.Setenv("TYPESAFE_API_KEY", "test-key")
	t.Setenv("TYPESAFE_BASE_URL", api.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session := connectMCP(t, ctx)
	qs, _ := questions([]byte(testQuestions))
	requestCtx, stop := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() {
		_, err := session.CallTool(requestCtx, &mcp.CallToolParams{Name: "evaluate", Arguments: object{"state": "x", "questions": qs}})
		done <- err
	}()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("request did not start")
	}
	stop()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancel succeeded unexpectedly")
		}
	case <-ctx.Done():
		t.Fatal("call did not cancel")
	}
	select {
	case <-stopped:
	case <-ctx.Done():
		t.Fatal("HTTP request did not cancel")
	}
}
