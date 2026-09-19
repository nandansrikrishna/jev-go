//go:build !windows

package cli

import (
	"os"
	"path/filepath"
)

func keyPath() (string, error) {
	home, e := os.UserHomeDir()
	return filepath.Join(home, ".config", "jev", "api-key"), e
}
func loadKey() (string, error) {
	path, e := keyPath()
	if e != nil {
		return "", e
	}
	data, e := os.ReadFile(path)
	return string(data), e
}
func saveKey(key string) error {
	if !validKey(key) {
		return errInput
	}
	path, e := keyPath()
	if e != nil {
		return e
	}
	if e = os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".api-key-")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	if _, e = f.WriteString(key); e != nil {
		f.Close()
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	return os.Rename(f.Name(), path)
}
