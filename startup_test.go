package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestStartupLogRetention(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 25; i++ {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("startup-%03d.log", i)), []byte("trace"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	untouched := filepath.Join(dir, "other.log")
	if err := os.WriteFile(untouched, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	trimStartupLogs(dir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 20 {
		t.Fatalf("want 19 traces plus other.log, got %d", len(entries))
	}
	if data, err := os.ReadFile(untouched); err != nil || string(data) != "keep" {
		t.Fatal("unrelated file changed")
	}
	if _, err := os.Stat(filepath.Join(dir, "startup-024.log")); err != nil {
		t.Fatal("newest trace removed")
	}
	if _, err := os.Stat(filepath.Join(dir, "startup-005.log")); !os.IsNotExist(err) {
		t.Fatal("oldest traces not removed")
	}
}
