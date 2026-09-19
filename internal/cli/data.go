package cli

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

type object = map[string]any

var errInput = errors.New("invalid configuration or input")

// inputError carries only safe, machine-actionable context. It must never
// contain record contents, question instructions, credentials, or upstream
// response bodies.
type inputError struct {
	Code    string
	Message string
	Field   string
	Path    string
	Line    int
}

func (e *inputError) Error() string { return e.Code }

func invalid(code, message string) error {
	return &inputError{Code: code, Message: message}
}

func invalidField(code, message, field string) error {
	return &inputError{Code: code, Message: message, Field: field}
}

func invalidPath(code, message, path string) error {
	return &inputError{Code: code, Message: message, Path: path}
}

func invalidLine(code, message string, line int) error {
	return &inputError{Code: code, Message: message, Line: line}
}

func decode(data []byte) (any, error) {
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	var v any
	if err := d.Decode(&v); err != nil {
		return nil, errInput
	}
	if d.Decode(new(any)) != io.EOF {
		return nil, errInput
	}
	return v, nil
}
func content(v any) bool {
	switch v.(type) {
	case string, map[string]any, []any:
		return true
	}
	return false
}
func nonempty(v any) bool {
	switch x := v.(type) {
	case string:
		return x != ""
	case map[string]any:
		return len(x) > 0
	case []any:
		return len(x) > 0
	}
	return false
}
func questions(data []byte) (object, error) {
	v, err := decode(data)
	if err != nil {
		return nil, invalid("invalid_questions_json", "Questions file is not one JSON object")
	}
	qs, ok := v.(object)
	if !ok || len(qs) == 0 {
		return nil, invalid("invalid_questions", "Questions must be a nonempty object")
	}
	for id, raw := range qs {
		q, ok := raw.(object)
		if !ok || id == "" || !nonempty(q["instructions"]) {
			return nil, invalidField("invalid_question", "Each question needs a nonempty ID and instructions", "questions")
		}
		for key := range q {
			if key != "type" && key != "instructions" && key != "criteria" {
				return nil, invalidField("invalid_question", "Question contains an unsupported field", "questions")
			}
		}
		switch q["type"] {
		case "choice":
			c, ok := q["criteria"].(object)
			if !ok || len(c) < 2 {
				return nil, invalidField("invalid_question_criteria", "Choice questions need at least two named criteria", "criteria")
			}
			for _, v := range c {
				if v != nil && !content(v) {
					return nil, invalidField("invalid_question_criteria", "Choice criteria descriptions must be text, objects, arrays, or null", "criteria")
				}
			}
		case "score":
			c, ok := q["criteria"].([]any)
			if !ok || len(c) < 2 {
				return nil, invalidField("invalid_question_criteria", "Score questions need at least two ordered criteria", "criteria")
			}
			for _, v := range c {
				if !content(v) {
					return nil, invalidField("invalid_question_criteria", "Score criteria must be text, objects, or arrays", "criteria")
				}
			}
		case "noul":
			if q["criteria"] == nil {
				delete(q, "criteria")
				continue
			}
			c, ok := q["criteria"].(object)
			if !ok {
				return nil, invalidField("invalid_question_criteria", "Noul criteria must be an object", "criteria")
			}
			for k, v := range c {
				if (k != "true" && k != "false") || (v != nil && !content(v)) {
					return nil, invalidField("invalid_question_criteria", "Noul criteria may contain only true and false", "criteria")
				}
			}
		default:
			return nil, invalidField("invalid_question_type", "Question type must be choice, score, or noul", "type")
		}
	}
	return qs, nil
}
func readLines(r io.Reader, visit func(object) error) error {
	// Reader has no Scanner token limit, allowing long document records.
	b := bufio.NewReader(r)
	lineNumber := 0
	for {
		line, err := b.ReadBytes('\n')
		lineNumber++
		if len(bytes.TrimSpace(line)) > 0 {
			v, e := decode(line)
			if e != nil {
				return invalidLine("invalid_jsonl", "JSONL line is not one JSON object", lineNumber)
			}
			row, ok := v.(object)
			if !ok {
				return invalidLine("invalid_jsonl_record", "JSONL record must be an object", lineNumber)
			}
			if e = visit(row); e != nil {
				var input *inputError
				if errors.As(e, &input) && input.Line == 0 {
					input.Line = lineNumber
				}
				return e
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}
func records(r io.Reader) ([]object, error) {
	rows := []object{}
	seen := map[string]bool{}
	err := readLines(r, func(row object) error {
		id, ok := row["id"].(string)
		if !ok || id == "" {
			return invalid("invalid_record_id", "Record ID must be a nonempty string")
		}
		if seen[id] {
			return invalid("duplicate_record_id", "Record IDs must be unique")
		}
		if !content(row["state"]) {
			return invalid("invalid_record_state", "Record state must be a string, object, or array")
		}
		seen[id] = true
		rows = append(rows, row)
		return nil
	})
	return rows, err
}

// Python json.dumps(sort_keys=True, ensure_ascii=False) compatibility keeps
// successful Python CLI results resumable, including float spelling and spacing.
func canonical(v any) string {
	switch x := v.(type) {
	case object:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(x))
		for _, k := range keys {
			parts = append(parts, canonical(k)+": "+canonical(x[k]))
		}
		return "{" + strings.Join(parts, ", ") + "}"
	case []any:
		parts := make([]string, len(x))
		for i, v := range x {
			parts[i] = canonical(v)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case string:
		var b strings.Builder
		b.WriteByte('"')
		for _, r := range x {
			switch r {
			case '"':
				b.WriteString(`\"`)
			case '\\':
				b.WriteString(`\\`)
			case '\b':
				b.WriteString(`\b`)
			case '\f':
				b.WriteString(`\f`)
			case '\n':
				b.WriteString(`\n`)
			case '\r':
				b.WriteString(`\r`)
			case '\t':
				b.WriteString(`\t`)
			default:
				if r < 32 {
					fmt.Fprintf(&b, `\u%04x`, r)
				} else {
					b.WriteRune(r)
				}
			}
		}
		b.WriteByte('"')
		return b.String()
	case json.Number:
		s := string(x)
		if !strings.ContainsAny(s, ".eE") {
			if s == "-0" {
				return "0"
			}
			return s
		}
		f, _ := strconv.ParseFloat(s, 64)
		s = strconv.FormatFloat(f, 'g', -1, 64)
		// Python uses fixed notation for exponents -4 through 15.
		if f == 0 || (abs(f) >= 1e-4 && abs(f) < 1e16) {
			s = strconv.FormatFloat(f, 'f', -1, 64)
			if !strings.Contains(s, ".") {
				s += ".0"
			}
		}
		return s
	default:
		b, _ := json.Marshal(v)
		return string(b)
	}
}
func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}
func fingerprint(row, qs object, model string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(canonical(object{"record": row, "questions": qs, "model": model}))))
}
