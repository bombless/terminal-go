//go:build windows && amd64

package sshclient

import (
	"bytes"
	"errors"
	"io"
	"os"
	"testing"
)

// TestRawPipesStartInstalledOpenSSH verifies raw stdin/stdout/stderr pipe
// creation, independent stderr draining, process wait, and handle teardown.
func TestRawPipesStartInstalledOpenSSH(t *testing.T) {
	executable, err := ResolveExecutable("")
	if err != nil {
		t.Skipf("Windows OpenSSH is not installed: %v", err)
	}
	session, err := StartRaw(executable, []string{"-V"})
	if err != nil {
		t.Fatalf("start ssh.exe with raw pipes: %v", err)
	}
	var stdout, stderr bytes.Buffer
	readDone := make(chan error, 2)
	go func() { _, readErr := io.Copy(&stdout, session.Output); readDone <- readErr }()
	go func() { _, readErr := io.Copy(&stderr, session.ErrOutput); readDone <- readErr }()
	exitCode, waitErr := session.Wait()
	if waitErr != nil {
		t.Fatalf("wait for raw-pipe ssh.exe: %v", waitErr)
	}
	for i := 0; i < 2; i++ {
		if readErr := <-readDone; readErr != nil && !errors.Is(readErr, os.ErrClosed) {
			t.Fatalf("read raw SSH pipe: %v", readErr)
		}
	}
	if closeErr := session.Close(); closeErr != nil {
		t.Fatalf("close raw SSH session: %v", closeErr)
	}
	if exitCode != 0 {
		t.Fatalf("ssh.exe -V exit code = %d; stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
	if stdout.Len()+stderr.Len() == 0 {
		t.Fatal("expected ssh.exe -V to write version data to a raw output pipe")
	}
	if stderr.Len() == 0 {
		t.Fatal("expected ssh.exe -V diagnostics on the separately captured stderr pipe")
	}
}
