//go:build windows && amd64

package sshclient

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"
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

func TestStartRefusesUnsupportedChild(t *testing.T) {
	_, err := Start("cmd.exe", nil, 80, 24)
	if err == nil || !strings.Contains(err.Error(), "supported executables are ssh.exe and pwsh.exe") {
		t.Fatalf("Start(cmd.exe) error = %v, want unsupported-executable policy error", err)
	}
}

func TestAllowedExecutable(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{`C:\Windows\System32\OpenSSH\ssh.exe`, true},
		{`C:\Program Files\PowerShell\7\pwsh.exe`, true},
		{`C:\Program Files\PowerShell\7\PWSH.EXE`, true},
		{`C:\Windows\System32\cmd.exe`, false},
		{`C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`, false},
	}
	for _, tt := range tests {
		if got := allowedExecutable(tt.path); got != tt.want {
			t.Errorf("allowedExecutable(%q) = %v, want %v", tt.path, got, tt.want)
		}
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
	if waitErr != nil {
		t.Fatalf("wait for ssh.exe: %v", waitErr)
	}
	var readErr error
	select {
	case readErr = <-readDone:
	case <-time.After(5 * time.Second):
		t.Fatal("ConPTY output reader did not reach EOF after Wait")
	}
	closeErr := session.Close()
	if closeErr != nil {
		t.Fatalf("close ConPTY session: %v", closeErr)
	}
	if readErr != nil && !errors.Is(readErr, os.ErrClosed) {
		t.Fatalf("read ConPTY output: %v", readErr)
	}
	if exitCode != 0 {
		t.Fatalf("ssh.exe -V exit code = %d; output: %s", exitCode, output.String())
	}
	if !strings.Contains(output.String(), "OpenSSH") {
		t.Fatalf("expected OpenSSH version text in ConPTY output, got %q", output.String())
	}
}

// Verifies pwsh.exe starts, writes to ConPTY, and exits cleanly.
func TestConPTYStartsInstalledPowerShell(t *testing.T) {
	executable, err := ResolvePowerShell("")
	if err != nil {
		t.Skipf("PowerShell 7 is not installed: %v", err)
	}
	session, err := Start(executable, []string{"-NoLogo", "-NoProfile", "-NonInteractive", "-Command", "exit 0"}, 80, 24)
	if err != nil {
		t.Fatalf("start pwsh.exe in ConPTY: %v", err)
	}
	var output bytes.Buffer
	readDone := make(chan error, 1)
	go func() {
		_, readErr := io.Copy(&output, session.Output)
		readDone <- readErr
	}()
	exitCode, waitErr := session.Wait()
	if waitErr != nil {
		t.Fatalf("wait for pwsh.exe: %v", waitErr)
	}
	var readErr error
	select {
	case readErr = <-readDone:
	case <-time.After(5 * time.Second):
		t.Fatal("PowerShell ConPTY output reader did not reach EOF after Wait")
	}
	closeErr := session.Close()
	if closeErr != nil {
		t.Fatalf("close ConPTY session: %v", closeErr)
	}
	if readErr != nil && !errors.Is(readErr, os.ErrClosed) {
		t.Fatalf("read ConPTY output: %v", readErr)
	}
	if exitCode != 0 {
		t.Fatalf("pwsh.exe exit code = %d; output: %s", exitCode, output.String())
	}
	if output.Len() == 0 {
		t.Fatal("expected the ConPTY output pipe to receive terminal data")
	}
}
