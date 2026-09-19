//go:build !windows

package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCredentials(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("TYPESAFE_API_KEY", "")
	if _, e := apiKey(); e == nil {
		t.Fatal("missing credentials accepted")
	}
	if e := saveKey("test-key"); e != nil {
		t.Fatal(e)
	}
	p, _ := keyPath()
	info, e := os.Stat(p)
	if e != nil || info.Mode().Perm() != 0600 {
		t.Fatal("permissions", e)
	}
	if k, e := apiKey(); e != nil || k != "test-key" {
		t.Fatal("load", e)
	}
	t.Setenv("TYPESAFE_API_KEY", "override")
	if k, _ := apiKey(); k != "override" {
		t.Fatal("precedence")
	}
	target := filepath.Join(t.TempDir(), "target")
	write(t, target, "untouched")
	os.Remove(p)
	if e := os.Symlink(target, p); e != nil {
		t.Fatal(e)
	}
	if e := saveKey("replacement"); e != nil {
		t.Fatal(e)
	}
	b, _ := os.ReadFile(target)
	if string(b) != "untouched" {
		t.Fatal("followed symlink")
	}
	if e := saveKey("bad key"); e == nil {
		t.Fatal("accepted whitespace")
	}
}
