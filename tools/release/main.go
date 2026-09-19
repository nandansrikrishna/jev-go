// Command release builds portable release archives and their checksums.
package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/nandansrikrishna/jev-go/internal/cli"
)

func main() {
	if err := build(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func build() error {
	if err := os.MkdirAll("dist", 0755); err != nil {
		return err
	}
	staging, err := os.MkdirTemp("", "jev-release-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	var sums []string
	for _, target := range []struct{ os, arch string }{{"darwin", "amd64"}, {"darwin", "arm64"}, {"linux", "amd64"}, {"linux", "arm64"}, {"windows", "amd64"}, {"windows", "arm64"}} {
		binary := "jev"
		suffix := ".tar.gz"
		if target.os == "windows" {
			binary += ".exe"
			suffix = ".zip"
		}
		output := filepath.Join(staging, binary)
		command := exec.Command("go", "build", "-trimpath", "-ldflags=-s -w", "-o", output, "./cmd/jev")
		command.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS="+target.os, "GOARCH="+target.arch)
		command.Stdout = os.Stdout
		command.Stderr = os.Stderr
		if err := command.Run(); err != nil {
			return err
		}
		files := map[string][]byte{}
		for _, name := range []string{"LICENSE", "INSTALL.md", "README.md", "THIRD_PARTY_NOTICES.md"} {
			data, err := os.ReadFile(name)
			if err != nil {
				return err
			}
			files[name] = data
		}
		files[binary], err = os.ReadFile(output)
		if err != nil {
			return err
		}
		archive, err := pack(files, binary, target.os == "windows")
		if err != nil {
			return err
		}
		name := fmt.Sprintf("jev_%s_%s_%s%s", cli.Version, target.os, target.arch, suffix)
		if err := os.WriteFile(filepath.Join("dist", name), archive, 0644); err != nil {
			return err
		}
		sums = append(sums, fmt.Sprintf("%x  %s", sha256.Sum256(archive), name))
		fmt.Println(name)
	}
	sort.Strings(sums)
	return os.WriteFile("dist/SHA256SUMS", []byte(strings.Join(sums, "\n")+"\n"), 0644)
}
func pack(files map[string][]byte, binary string, windows bool) ([]byte, error) {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	var out bytes.Buffer
	if windows {
		writer := zip.NewWriter(&out)
		for _, name := range names {
			header := &zip.FileHeader{Name: name, Method: zip.Deflate}
			header.SetMode(0644)
			if name == binary {
				header.SetMode(0755)
			}
			header.SetModTime(time.Unix(0, 0).UTC())
			entry, err := writer.CreateHeader(header)
			if err != nil {
				return nil, err
			}
			if _, err = entry.Write(files[name]); err != nil {
				return nil, err
			}
		}
		if err := writer.Close(); err != nil {
			return nil, err
		}
	} else {
		compressed := gzip.NewWriter(&out)
		writer := tar.NewWriter(compressed)
		for _, name := range names {
			mode := int64(0644)
			if name == binary {
				mode = 0755
			}
			if err := writer.WriteHeader(&tar.Header{Name: name, Mode: mode, Size: int64(len(files[name])), ModTime: time.Unix(0, 0).UTC()}); err != nil {
				return nil, err
			}
			if _, err := writer.Write(files[name]); err != nil {
				return nil, err
			}
		}
		if err := writer.Close(); err != nil {
			return nil, err
		}
		if err := compressed.Close(); err != nil {
			return nil, err
		}
	}
	return out.Bytes(), nil
}
