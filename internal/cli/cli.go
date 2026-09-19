// Package cli implements the native Jev command-line interface.
package cli

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/term"
)

const Version = "0.1.0-beta.2"

//go:embed assets/*
var assets embed.FS

const help = `Jev: typed judgments for agents. JSON to stdout; diagnostics to stderr.
Usage: jev <command> [options]
Commands:
  auth      Save an API key locally (--stdin for noninteractive use)
  init      Create examples (--directory jev-demo)
  schema    Print the question JSON Schema
  validate  Validate --questions FILE [--input FILE|-]
  evaluate  Evaluate --questions FILE --input FILE|- [--output FILE|-]
            [--model jev-latest] [--workers 4] [--resume]
  mcp       Run the native MCP server over stdio
Use jev <command> --help for command options.
`

func emit(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

// Run returns an exit status and never prints raw errors or private inputs.
func Run(ctx context.Context, args []string, in io.Reader, out, diag io.Writer) int {
	code, err := run(ctx, args, in, out, diag)
	if ctx.Err() != nil {
		return 130
	}
	if err != nil {
		detail := object{"type": "ValueError", "code": "invalid_input", "message": "Invalid configuration or input"}
		var input *inputError
		if errors.As(err, &input) {
			detail["code"] = input.Code
			detail["message"] = input.Message
			if input.Field != "" {
				detail["field"] = input.Field
			}
			if input.Path != "" {
				detail["path"] = input.Path
			}
			if input.Line > 0 {
				detail["line"] = input.Line
			}
		}
		_ = emit(diag, object{"error": detail})
		return 2
	}
	return code
}
func run(ctx context.Context, args []string, in io.Reader, out, diag io.Writer) (int, error) {
	if len(args) == 0 {
		fmt.Fprint(diag, help)
		return 2, nil
	}
	if args[0] == "--version" && len(args) == 1 {
		_, e := fmt.Fprintln(out, "jev "+Version)
		return 0, e
	}
	if (args[0] == "--help" || args[0] == "-h") && len(args) == 1 {
		_, e := fmt.Fprint(out, help)
		return 0, e
	}
	fs := flag.NewFlagSet(args[0], flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var qpath, input, output, model, directory string
	var workers int
	var resume, stdin bool
	switch args[0] {
	case "auth":
		fs.BoolVar(&stdin, "stdin", false, "Read key from stdin")
	case "init":
		fs.StringVar(&directory, "directory", "jev-demo", "New directory to create")
	case "schema", "mcp":
	case "evaluate", "validate":
		fs.StringVar(&qpath, "questions", "", "Questions JSON file (required)")
		fs.StringVar(&input, "input", "", "JSONL file, or - for stdin")
		if args[0] == "evaluate" {
			fs.StringVar(&output, "output", "-", "JSONL output, or - for stdout")
			fs.StringVar(&model, "model", "jev-latest", "Model name")
			fs.IntVar(&workers, "workers", 4, "Concurrent requests (1–32)")
			fs.BoolVar(&resume, "resume", false, "Append and skip successful identical requests")
		}
	default:
		return 2, invalid("unknown_command", "Unknown command; run jev --help")
	}
	if e := fs.Parse(args[1:]); e == flag.ErrHelp {
		fmt.Fprintf(out, "Usage: jev %s [options]\n", args[0])
		fs.SetOutput(out)
		fs.PrintDefaults()
		return 0, nil
	} else if e != nil || fs.NArg() != 0 {
		return 2, invalid("invalid_arguments", "Invalid command arguments; run the command with --help")
	}
	switch args[0] {
	case "schema":
		data, _ := assets.ReadFile("assets/schema.json")
		_, e := out.Write(data)
		return 0, e
	case "init":
		parent := filepath.Dir(directory)
		if e := os.MkdirAll(parent, 0755); e != nil {
			return 2, e
		}
		if e := os.Mkdir(directory, 0755); e != nil {
			return 2, e
		}
		for _, name := range []string{"questions.json", "tickets.jsonl", "summarize.py"} {
			data, _ := assets.ReadFile("assets/" + name)
			if e := os.WriteFile(filepath.Join(directory, name), data, 0644); e != nil {
				return 2, e
			}
		}
		return 0, emit(out, object{"created": directory})
	case "auth":
		var data []byte
		var e error
		if stdin {
			data, e = io.ReadAll(in)
		} else {
			f, ok := in.(*os.File)
			if !ok || !term.IsTerminal(int(f.Fd())) {
				return 2, invalid("noninteractive_auth", "Use jev auth --stdin when stdin is not a terminal")
			}
			fmt.Fprint(diag, "TypeSafe API key: ")
			data, e = term.ReadPassword(int(f.Fd()))
			fmt.Fprintln(diag)
		}
		if e != nil {
			return 2, e
		}
		if e = saveKey(strings.TrimSpace(string(data))); e != nil {
			return 2, e
		}
		return 0, emit(out, object{"saved": true})
	case "mcp":
		return 0, serveMCP(ctx, in, out)
	}
	if qpath == "" {
		return 2, invalidField("missing_required_option", "The --questions option is required", "questions")
	}
	data, e := os.ReadFile(qpath)
	if e != nil {
		return 2, invalidPath("questions_file_error", "Could not read questions file", qpath)
	}
	qs, e := questions(data)
	if e != nil {
		return 2, e
	}
	rows := []object{}
	if input != "" {
		reader := in
		if input != "-" {
			f, e := os.Open(input)
			if e != nil {
				return 2, invalidPath("input_file_error", "Could not read input file", input)
			}
			defer f.Close()
			reader = f
		}
		rows, e = records(reader)
		if e != nil {
			return 2, e
		}
	}
	if args[0] == "validate" {
		return 0, emit(out, object{"valid": true, "questions": len(qs), "records": len(rows)})
	}
	if input == "" || workers < 1 || workers > 32 || (resume && output == "-") {
		if input == "" {
			return 2, invalidField("missing_required_option", "The --input option is required", "input")
		}
		if workers < 1 || workers > 32 {
			return 2, invalidField("invalid_workers", "Workers must be between 1 and 32", "workers")
		}
		return 2, invalidField("invalid_resume_output", "Resume requires a file output", "output")
	}
	if output != "-" {
		for _, p := range []string{qpath, input} {
			if p != "-" && sameFile(output, p) {
				return 2, invalidField("unsafe_output_path", "Output must not overwrite an input file", "output")
			}
		}
	}
	completed := map[string]bool{}
	if resume {
		f, e := os.Open(output)
		if e != nil && !os.IsNotExist(e) {
			return 2, e
		}
		if e == nil {
			e = readLines(f, func(row object) error {
				if _, ok := row["answers"]; ok {
					if _, bad := row["error"]; !bad {
						digest, ok := row["fingerprint"].(string)
						if !ok || digest == "" {
							return invalid("invalid_resume_record", "Successful resume records need a nonempty fingerprint")
						}
						completed[digest] = true
					}
				}
				return nil
			})
			f.Close()
			if e != nil {
				return 2, e
			}
		}
	}
	client, e := newAPIClient()
	if e != nil {
		return 2, e
	}
	stream := out
	if output != "-" {
		flags := os.O_CREATE | os.O_WRONLY | os.O_EXCL
		if resume {
			flags = os.O_CREATE | os.O_RDWR | os.O_APPEND
		}
		f, e := os.OpenFile(output, flags, 0600)
		if e != nil {
			return 2, invalidPath("output_file_error", "Could not create or open output file", output)
		}
		defer f.Close()
		stream = f
		// A valid final JSON object without a newline must not merge with the next row.
		if resume {
			info, e := f.Stat()
			if e != nil {
				return 2, e
			}
			if info.Size() > 0 {
				last := make([]byte, 1)
				if _, e = f.ReadAt(last, info.Size()-1); e != nil {
					return 2, e
				}
				if last[0] != '\n' {
					if _, e = f.WriteString("\n"); e != nil {
						return 2, e
					}
				}
			}
		}
	}
	return evaluate(ctx, client, rows, qs, model, workers, completed, stream, diag)
}
func sameFile(a, b string) bool {
	aa, _ := filepath.Abs(a)
	bb, _ := filepath.Abs(b)
	if aa == bb {
		return true
	}
	ai, ae := os.Stat(a)
	bi, be := os.Stat(b)
	return ae == nil && be == nil && os.SameFile(ai, bi)
}
func evaluate(ctx context.Context, c *apiClient, rows []object, qs object, model string, workers int, completed map[string]bool, out, diag io.Writer) (int, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	// A sliding window bounds both active requests and buffered out-of-order results.
	type job struct {
		row    object
		digest string
		result chan object
	}
	jobs := make(chan job)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				r := c.evaluate(ctx, j.row["state"], qs, model)
				r["id"] = j.row["id"]
				r["fingerprint"] = j.digest
				j.result <- r
			}
		}()
	}
	defer func() { close(jobs); wg.Wait() }()
	pending := []job{}
	index, processed, failed, skipped := 0, 0, 0, 0
	for index < len(rows) || len(pending) > 0 {
		for index < len(rows) && len(pending) < workers {
			row := rows[index]
			index++
			digest := fingerprint(row, qs, model)
			if completed[digest] {
				skipped++
				continue
			}
			j := job{row, digest, make(chan object, 1)}
			select {
			case jobs <- j:
				pending = append(pending, j)
			case <-ctx.Done():
				return 130, nil
			}
		}
		if len(pending) == 0 {
			continue
		}
		select {
		case r := <-pending[0].result:
			if e := emit(out, r); e != nil {
				cancel()
				return 2, e
			}
			processed++
			if _, ok := r["error"]; ok {
				failed++
			}
			pending = pending[1:]
		case <-ctx.Done():
			return 130, nil
		}
	}
	if e := emit(diag, object{"processed": processed, "failed": failed, "skipped": skipped}); e != nil {
		return 2, e
	}
	if failed > 0 {
		return 1, nil
	}
	return 0, nil
}
