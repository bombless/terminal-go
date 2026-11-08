//go:build windows && amd64

package sshclient

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"unsafe"
)

func TestConPTYWin64Layouts(t *testing.T) {
	tests := []struct {
		name      string
		got, want uintptr
	}{
		{"COORD", unsafe.Sizeof(coord{}), 4},
		{"STARTUPINFOEX", unsafe.Sizeof(startupInfoEx{}), 112},
		{"PROCESS_INFORMATION", unsafe.Sizeof(processInformation{}), 24},
		{"SECURITY_ATTRIBUTES", unsafe.Sizeof(securityAttributes{}), 24},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("%s size = %d, want %d", tt.name, tt.got, tt.want)
		}
	}
}

func TestStartRefusesNonSSHChild(t *testing.T) {
	_, err := Start("cmd.exe", nil, 80, 24)
	if err == nil || !strings.Contains(err.Error(), "only permitted session executable is ssh.exe") {
		t.Fatalf("Start(cmd.exe) error = %v, want SSH-only policy error", err)
	}
}

// This exercises CreatePseudoConsole, STARTUPINFOEX, CreateProcessW, pipe I/O,
// and native-handle teardown using the installed OpenSSH client's harmless -V.
func TestConPTYStartsInstalledOpenSSH(t *testing.T) {
	executable, err := ResolveExecutable("")
	if err != nil {
		t.Skipf("Windows OpenSSH is not installed: %v", err)
	}
	session, err := Start(executable, []string{"-V"}, 80, 24)
	if err != nil {
		t.Fatalf("start ssh.exe in ConPTY: %v", err)
	}
	var output bytes.Buffer
	readDone := make(chan error, 1)
	go func() {
		_, readErr := io.Copy(&output, session.Output)
		readDone <- readErr
	}()
	exitCode, waitErr := session.Wait()
	closeErr := session.Close()
	if waitErr != nil {
		t.Fatalf("wait for ssh.exe: %v", waitErr)
	}
	if closeErr != nil {
		t.Fatalf("close ConPTY session: %v", closeErr)
	}
	if readErr := <-readDone; readErr != nil && !errors.Is(readErr, os.ErrClosed) {
		t.Fatalf("read ConPTY output: %v", readErr)
	}
	if exitCode != 0 {
		t.Fatalf("ssh.exe -V exit code = %d; output: %s", exitCode, output.String())
	}
	if output.Len() == 0 {
		t.Fatal("expected the ConPTY output pipe to receive terminal data")
	}
}
