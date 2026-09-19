package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Use the SDK's raw handler API to keep JSON numbers intact for resume hashes
// and apply shared validation without echoing private inputs in schema errors.
func newMCPServer() *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "Jev", Version: Version}, &mcp.ServerOptions{Capabilities: &mcp.ServerCapabilities{}})
	stateSchema := object{"anyOf": []any{object{"type": "string"}, object{"type": "object"}, object{"type": "array"}}}
	common := object{"questions": object{"type": "object", "minProperties": 1, "description": "Question IDs mapped to choice, score, or noul questions. Use question_schema for the format."}, "model": object{"type": "string", "default": "jev-latest"}}
	single := object{"state": stateSchema, "questions": common["questions"], "model": common["model"]}
	batch := object{"records": object{"type": "array", "minItems": 1, "maxItems": 50, "items": object{"type": "object", "required": []string{"id", "state"}, "properties": object{"id": object{"type": "string", "minLength": 1}, "state": stateSchema}}}, "questions": common["questions"], "model": common["model"]}
	for _, tool := range []struct {
		name, description string
		props             object
		required          []string
	}{
		{"question_schema", "Get the schema for dynamically authored choice, score, and noul questions.", object{}, nil},
		{"evaluate", "Evaluate context against typed questions. Bundle independent questions in one call. Predictions are not proof.", single, []string{"state", "questions"}},
		{"evaluate_batch", "Evaluate 1–50 records with unique string IDs and state. Returns ordered results and a failure count. Use the CLI for larger, resumable jobs.", batch, []string{"records", "questions"}},
	} {
		schema := object{"type": "object", "properties": tool.props, "additionalProperties": false}
		if len(tool.required) > 0 {
			schema["required"] = tool.required
		}
		name := tool.name
		s.AddTool(&mcp.Tool{Name: name, Description: tool.description, InputSchema: schema}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return callMCPTool(ctx, name, req.Params.Arguments), nil
		})
	}
	return s
}

func mcpResult(value object, failed bool) *mcp.CallToolResult {
	data, _ := json.Marshal(value)
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(data)}}, StructuredContent: value, IsError: failed}
}
func mcpInputError() *mcp.CallToolResult {
	return mcpResult(object{"error": object{"type": "ValueError", "message": "Invalid configuration or input; check question schema, unique IDs, batch size (1–50), and credentials"}}, true)
}
func callMCPTool(ctx context.Context, name string, raw json.RawMessage) *mcp.CallToolResult {
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	value, err := decode(raw)
	if err != nil {
		return mcpInputError()
	}
	args, ok := value.(object)
	if !ok {
		return mcpInputError()
	}
	for key := range args {
		if name == "question_schema" || (key != "questions" && key != "model" && !(name == "evaluate" && key == "state") && !(name == "evaluate_batch" && key == "records")) {
			return mcpInputError()
		}
	}
	if name == "question_schema" {
		data, _ := assets.ReadFile("assets/schema.json")
		value, _ := decode(data)
		return mcpResult(value.(object), false)
	}
	data, err := json.Marshal(args["questions"])
	if err != nil {
		return mcpInputError()
	}
	qs, err := questions(data)
	if err != nil {
		return mcpInputError()
	}
	model := "jev-latest"
	if v, exists := args["model"]; exists {
		model, ok = v.(string)
		if !ok {
			return mcpInputError()
		}
	}
	var rows []object
	if name == "evaluate" {
		if !content(args["state"]) {
			return mcpInputError()
		}
	} else {
		values, ok := args["records"].([]any)
		if !ok || len(values) < 1 || len(values) > 50 {
			return mcpInputError()
		}
		var input bytes.Buffer
		for _, row := range values {
			if err := emit(&input, row); err != nil {
				return mcpInputError()
			}
		}
		rows, err = records(&input)
		if err != nil {
			return mcpInputError()
		}
	}
	client, err := newAPIClient()
	if err != nil {
		return mcpInputError()
	}
	if name == "evaluate" {
		result := client.evaluate(ctx, args["state"], qs, model)
		if ctx.Err() != nil {
			return mcpResult(object{"error": safeError("CancelledError", 0)}, true)
		}
		return mcpResult(result, result["error"] != nil)
	}
	var output bytes.Buffer
	code, err := evaluate(ctx, client, rows, qs, model, 4, nil, &output, io.Discard)
	if err != nil || code == 130 {
		return mcpResult(object{"error": safeError("CancelledError", 0)}, true)
	}
	results := []object{}
	failed := 0
	err = readLines(&output, func(row object) error {
		results = append(results, row)
		if row["error"] != nil {
			failed++
		}
		return nil
	})
	if err != nil {
		return mcpInputError()
	}
	return mcpResult(object{"results": results, "failed": failed}, false)
}

type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }
func serveMCP(ctx context.Context, in io.Reader, out io.Writer) error {
	reader, ok := in.(io.ReadCloser)
	if !ok {
		reader = io.NopCloser(in)
	}
	writer, ok := out.(io.WriteCloser)
	if !ok {
		writer = nopWriteCloser{out}
	}
	return newMCPServer().Run(ctx, &mcp.IOTransport{Reader: reader, Writer: writer})
}
