package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestArchiveTimestampAndReproducibility(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"bin", "sql", "admin"} {
		if err := os.Mkdir(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "admin", "index.html"), []byte("test"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	a, b := filepath.Join(out, "a.tar.gz"), filepath.Join(out, "b.tar.gz")
	for _, path := range []string{a, b} {
		if err := writeArchive(root, path); err != nil {
			t.Fatal(err)
		}
	}
	first, err := os.ReadFile(a)
	if err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(b)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("archive bytes differ for identical inputs")
	}
	gz, err := gzip.NewReader(bytes.NewReader(first))
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	want := time.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC)
	count := 0
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if !h.ModTime.Equal(want) {
			t.Fatalf("%s: timestamp = %s, want %s", h.Name, h.ModTime, want)
		}
		count++
	}
	if count != 4 {
		t.Fatalf("got %d archive entries, want 4", count)
	}
}
