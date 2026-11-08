package sshclient

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveExecutableOverride(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "ssh.exe")
	if err := os.WriteFile(file, []byte("test"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := ResolveExecutable(file)
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.Abs(file)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("ResolveExecutable() = %q, want %q", got, want)
	}
}

func TestResolveExecutableRejectsDirectory(t *testing.T) {
	if _, err := ResolveExecutable(t.TempDir()); err == nil {
		t.Fatal("ResolveExecutable() accepted a directory")
	}
}
