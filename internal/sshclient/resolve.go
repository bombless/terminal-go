// Package sshclient locates and runs the supported Windows terminal executables
// without a shell.
package sshclient

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// ResolveExecutable returns an absolute path to ssh.exe. An explicit override
// takes precedence, followed by the standard Windows OpenSSH location and PATH.
func ResolveExecutable(override string) (string, error) {
	if override != "" {
		return executableFile(override)
	}
	if windir := os.Getenv("WINDIR"); windir != "" {
		candidate := filepath.Join(windir, "System32", "OpenSSH", "ssh.exe")
		if path, err := executableFile(candidate); err == nil {
			return path, nil
		}
	}
	path, err := exec.LookPath("ssh.exe")
	if err != nil {
		return "", fmt.Errorf("Windows OpenSSH client (ssh.exe) was not found; install the Windows OpenSSH Client optional feature or add ssh.exe to PATH: %w", err)
	}
	return filepath.Abs(path)
}

// ResolvePowerShell returns an absolute path to pwsh.exe. An explicit override
// takes precedence, followed by the standard PowerShell 7 install location and PATH.
func ResolvePowerShell(override string) (string, error) {
	if override != "" {
		return executableFile(override)
	}
	if programFiles := os.Getenv("ProgramFiles"); programFiles != "" {
		candidate := filepath.Join(programFiles, "PowerShell", "7", "pwsh.exe")
		if path, err := executableFile(candidate); err == nil {
			return path, nil
		}
	}
	path, err := exec.LookPath("pwsh.exe")
	if err != nil {
		return "", fmt.Errorf("PowerShell 7 (pwsh.exe) was not found; install PowerShell 7 or add pwsh.exe to PATH: %w", err)
	}
	return filepath.Abs(path)
}

func executableFile(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("executable path is a directory: %s", path)
	}
	return filepath.Abs(path)
}
