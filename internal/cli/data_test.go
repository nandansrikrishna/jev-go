package cli

import (
	"os"
	"testing"
)

func TestPythonFingerprints(t *testing.T) {
	b, e := os.ReadFile("testdata/fingerprints.json")
	if e != nil {
		t.Fatal(e)
	}
	v, e := decode(b)
	if e != nil {
		t.Fatal(e)
	}
	for _, raw := range v.([]any) {
		c := raw.(object)
		got := fingerprint(c["record"].(object), c["questions"].(object), c["model"].(string))
		if got != c["expected"] {
			t.Fatalf("fingerprint mismatch: %s != %s", got, c["expected"])
		}
	}
}
