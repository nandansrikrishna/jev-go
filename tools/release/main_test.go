package main

import (
	"archive/zip"
	"bytes"
	"io"
	"testing"
)

func TestWindowsArchivePortableTimestamp(t *testing.T) {
	data, err := pack(map[string][]byte{"jev.exe": []byte("test executable"), "LICENSE": []byte("license")}, "jev.exe", true)
	if err != nil {
		t.Fatal(err)
	}
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	if len(archive.File) != 2 {
		t.Fatal("missing files")
	}
	for _, file := range archive.File {
		// ZIP's DOS timestamp cannot represent Unix epoch 1970 without wrapping.
		if file.ModTime().Year() != 1980 || file.Modified.Year() != 1980 {
			t.Fatal("invalid ZIP timestamp", file.Name)
		}
		reader, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		content, err := io.ReadAll(reader)
		reader.Close()
		if err != nil || len(content) == 0 {
			t.Fatal("corrupt archive", err)
		}
	}
}
