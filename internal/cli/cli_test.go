package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const testQuestions = `{"urgent":{"type":"noul","instructions":"Is this urgent?"}}`

func invoke(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, diag bytes.Buffer
	code := Run(context.Background(), args, strings.NewReader(""), &out, &diag)
	return code, out.String(), diag.String()
}
func write(t *testing.T, path, text string) {
	t.Helper()
	if e := os.WriteFile(path, []byte(text), 0600); e != nil {
		t.Fatal(e)
	}
}
func fixture(t *testing.T) (string, string, string) {
	t.Helper()
	dir := t.TempDir()
	q, in, out := filepath.Join(dir, "questions.json"), filepath.Join(dir, "input.jsonl"), filepath.Join(dir, "results.jsonl")
	write(t, q, testQuestions)
	write(t, in, "{\"id\":\"a\",\"state\":\"ok\"}\n{\"id\":\"b\",\"state\":\"fail\"}\n")
	return q, in, out
}
func TestValidation(t *testing.T) {
	for _, raw := range []string{`{}`, `[]`, `null`, `{"":{"type":"noul","instructions":"?"}}`, `{"a":{"type":"noul","instructions":1}}`, `{"a":{"type":"noul","instructions":"?","extra":true}}`, `{"a":{"type":"score","instructions":"?","criteria":["one"]}}`, `{"a":{"type":"choice","instructions":"?","criteria":["a","b"]}}`, `{"a":{"type":"noul","instructions":"?","criteria":{"wrong":"x"}}}`} {
		if _, e := questions([]byte(raw)); e == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	for _, raw := range []string{testQuestions, `{"a":{"type":"choice","instructions":{"task":"pick"},"criteria":{"a":null,"b":[]}}}`, `{"a":{"type":"score","instructions":["rate"],"criteria":["a",{}]}}`} {
		if _, e := questions([]byte(raw)); e != nil {
			t.Fatalf("rejected %s", raw)
		}
	}
	for _, raw := range []string{`{"id":"a","state":null}`, `{"id":1,"state":"x"}`, "{\"id\":\"a\",\"state\":\"x\"}\n{\"id\":\"a\",\"state\":\"y\"}", `{"id":"a","state":"x"} {}`} {
		if _, e := records(strings.NewReader(raw)); e == nil {
			t.Fatal("invalid records accepted")
		}
	}
	raw := `{"id":"café","state":"登录失败` + strings.Repeat("x", 100000) + `"}`
	if rows, e := records(strings.NewReader(raw)); e != nil || rows[0]["id"] != "café" {
		t.Fatal("long Unicode input failed", e)
	}
}
func TestInitSchemaAndFlags(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "demo")
	if c, _, _ := invoke(t, "init", "--directory", dir); c != 0 {
		t.Fatal(c)
	}
	if c, _, _ := invoke(t, "init", "--directory", dir); c != 2 {
		t.Fatal(c)
	}
	if c, out, _ := invoke(t, "validate", "--questions", filepath.Join(dir, "questions.json"), "--input", filepath.Join(dir, "tickets.jsonl")); c != 0 || !strings.Contains(out, `"records":3`) {
		t.Fatal(c, out)
	}
	if c, out, _ := invoke(t, "schema"); c != 0 || !json.Valid([]byte(out)) {
		t.Fatal(c, out)
	}
	for _, args := range [][]string{{}, {"validate"}, {"unknown"}, {"schema", "unexpected"}, {"auth", "--private-secret"}} {
		c, out, err := invoke(t, args...)
		if c != 2 || strings.Contains(out+err, "private-secret") {
			t.Fatal(c, out, err)
		}
	}
	if c, out, _ := invoke(t, "evaluate", "--help"); c != 0 || !strings.Contains(out, "workers") {
		t.Fatal(c, out)
	}
}
func TestEvaluateResumeAndProtection(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/v1/systemone" || r.Header.Get("Authorization") != "Bearer test-key" || r.Method != "POST" {
			t.Error("request contract")
		}
		var body object
		if e := json.NewDecoder(r.Body).Decode(&body); e != nil {
			t.Error(e)
		}
		if body["state"] == "fail" {
			w.WriteHeader(400)
			fmt.Fprint(w, "private upstream body")
			return
		}
		time.Sleep(10 * time.Millisecond)
		fmt.Fprint(w, `{"model":"jev-latest","answers":{"urgent":{"type":"noul","noul":0.9}},"usage":{"input_tokens":10,"output_tokens":2},"secret":"private"}`)
	}))
	defer server.Close()
	t.Setenv("TYPESAFE_API_KEY", "test-key")
	t.Setenv("TYPESAFE_BASE_URL", server.URL)
	q, in, out := fixture(t)
	args := []string{"evaluate", "--questions", q, "--input", in, "--output", out, "--workers", "2"}
	c, _, diag := invoke(t, args...)
	if c != 1 || !strings.Contains(diag, `"failed":1`) {
		t.Fatal(c, diag)
	}
	data, _ := os.ReadFile(out)
	rows, e := readResults(data)
	if e != nil || len(rows) != 2 || rows[0]["id"] != "a" || rows[1]["id"] != "b" || strings.Contains(string(data), "private") {
		t.Fatal(string(data), e)
	}
	if c, _, _ := invoke(t, args...); c != 2 || calls.Load() != 2 {
		t.Fatal("overwrite protection", c, calls.Load())
	}
	// Also exercise appending after a valid unterminated final line.
	write(t, out, strings.TrimSpace(string(data)))
	c, _, diag = invoke(t, append(args, "--resume")...)
	if c != 1 || calls.Load() != 3 || !strings.Contains(diag, `"skipped":1`) {
		t.Fatal(c, diag, calls.Load())
	}
	data, _ = os.ReadFile(out)
	rows, e = readResults(data)
	if e != nil || len(rows) != 3 {
		t.Fatal("append", e, string(data))
	}
	write(t, out, "{broken")
	if c, _, _ := invoke(t, append(args, "--resume")...); c != 2 || calls.Load() != 3 {
		t.Fatal("malformed resume")
	}
	for _, target := range []string{q, in} {
		if c, _, _ := invoke(t, "evaluate", "--questions", q, "--input", in, "--output", target, "--resume"); c != 2 {
			t.Fatal("input overwrite")
		}
	}
	for _, count := range []string{"0", "33", "bad"} {
		if c, _, _ := invoke(t, "evaluate", "--questions", q, "--input", in, "--workers", count); c != 2 {
			t.Fatal("workers")
		}
	}
}
func readResults(b []byte) ([]object, error) {
	rows := []object{}
	e := readLines(bytes.NewReader(b), func(r object) error { rows = append(rows, r); return nil })
	return rows, e
}
func TestRetriesAndInvalidResponses(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) < 3 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(429)
			return
		}
		fmt.Fprint(w, `{"model":"x","usage":{},"answers":{"urgent":{"type":"noul","noul":0.5}}}`)
	}))
	defer server.Close()
	qs, _ := questions([]byte(testQuestions))
	c := &apiClient{key: "test", base: server.URL, http: server.Client()}
	if r := c.evaluate(context.Background(), "x", qs, "x"); r["error"] != nil || calls.Load() != 3 {
		t.Fatal(r, calls.Load())
	}
	for _, raw := range []string{`{}`, `{"model":"x","usage":{},"answers":{"a":{"type":"noul","noul":"secret"}}}`, `{"model":"x","usage":{},"answers":null}`, `{"model":"x","usage":{"input_tokens":1.5}}`} {
		if _, ok := response([]byte(raw)); ok {
			t.Fatal("accepted invalid response", raw)
		}
	}
}
func TestStdinAndMissingCredentials(t *testing.T) {
	q, in, out := fixture(t)
	t.Setenv("TYPESAFE_API_KEY", "")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	// Invalid key stops evaluation before creating a result file on all platforms.
	t.Setenv("TYPESAFE_API_KEY", "bad key")
	if c, _, _ := invoke(t, "evaluate", "--questions", q, "--input", in, "--output", out); c != 2 {
		t.Fatal(c)
	}
	if _, e := os.Stat(out); !os.IsNotExist(e) {
		t.Fatal("output created without credentials")
	}
	var stdout, stderr bytes.Buffer
	c := Run(context.Background(), []string{"validate", "--questions", q, "--input", "-"}, strings.NewReader(`{"id":"a","state":{}}`), &stdout, &stderr)
	if c != 0 || !strings.Contains(stdout.String(), `"records":1`) {
		t.Fatal(c, stderr.String())
	}
}
func TestCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	qs, _ := questions([]byte(testQuestions))
	c := &apiClient{base: "http://127.0.0.1:1", http: &http.Client{Timeout: time.Second}}
	start := time.Now()
	code, e := evaluate(ctx, c, []object{{"id": "a", "state": "x"}}, qs, "x", 1, map[string]bool{}, &bytes.Buffer{}, &bytes.Buffer{})
	if e != nil || code != 130 || time.Since(start) > time.Second {
		t.Fatal(code, e)
	}
}

func TestWorkerLimitAndChangedResume(t *testing.T) {
	var active, peak, calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := active.Add(1)
		defer active.Add(-1)
		calls.Add(1)
		for old := peak.Load(); n > old && !peak.CompareAndSwap(old, n); old = peak.Load() {
		}
		time.Sleep(5 * time.Millisecond)
		fmt.Fprint(w, `{"model":"x","answers":{},"usage":{}}`)
	}))
	defer server.Close()
	qs, _ := questions([]byte(testQuestions))
	c := &apiClient{key: "test", base: server.URL, http: server.Client()}
	rows := make([]object, 20)
	for i := range rows {
		rows[i] = object{"id": fmt.Sprint(i), "state": "x"}
	}
	var out, diag bytes.Buffer
	code, e := evaluate(context.Background(), c, rows, qs, "x", 3, map[string]bool{}, &out, &diag)
	if code != 0 || e != nil || peak.Load() > 3 || peak.Load() < 2 || calls.Load() != 20 {
		t.Fatal(code, e, peak.Load(), calls.Load())
	}
	results, e := readResults(out.Bytes())
	if e != nil {
		t.Fatal(e)
	}
	for i, r := range results {
		if r["id"] != fmt.Sprint(i) {
			t.Fatal("unordered")
		}
	}
	digest := fingerprint(rows[0], qs, "x")
	completed := map[string]bool{digest: true}
	for _, model := range []string{"x", "changed"} {
		before := calls.Load()
		code, e = evaluate(context.Background(), c, rows[:1], qs, model, 1, completed, &bytes.Buffer{}, &bytes.Buffer{})
		expected := int32(0)
		if model == "changed" {
			expected = 1
		}
		if code != 0 || e != nil || calls.Load()-before != expected {
			t.Fatal("resume model", code, e)
		}
	}
	rows[0]["state"] = "changed"
	if fingerprint(rows[0], qs, "x") == digest {
		t.Fatal("state not hashed")
	}
	qs["urgent"].(object)["instructions"] = "different"
	if fingerprint(rows[0], qs, "x") == digest {
		t.Fatal("questions not hashed")
	}
}

func TestRetryHeaders(t *testing.T) {
	for _, tc := range []struct {
		key, value string
		want       time.Duration
	}{{"Retry-After", "2", 2 * time.Second}, {"retry-after-ms", "10", 10 * time.Millisecond}} {
		h := http.Header{}
		h.Set(tc.key, tc.value)
		d, ok := retryDelay(h)
		if !ok || d != tc.want {
			t.Fatal(d, ok)
		}
	}
	for _, v := range []string{"-1", "NaN", "Inf", "1e99", "bad"} {
		h := http.Header{}
		h.Set("Retry-After", v)
		if _, ok := retryDelay(h); ok {
			t.Fatal(v)
		}
	}
}
