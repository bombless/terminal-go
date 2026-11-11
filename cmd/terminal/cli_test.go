package main

import "testing"

func TestParseStartupOptions(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		want    startupOptions
		wantErr bool
	}{
		{name: "no startup session", want: startupOptions{}},
		{name: "ssh", args: []string{"--ssh", "admin@example.com:2222"}, want: startupOptions{sshAddress: "admin@example.com:2222"}},
		{name: "ssh command", args: []string{"--ssh=admin@example.com", "--ssh-command", "uname -a && whoami"}, want: startupOptions{sshAddress: "admin@example.com", sshCommand: "uname -a && whoami"}},
		{name: "powershell", args: []string{"--powershell"}, want: startupOptions{powerShell: true}},
		{name: "powershell command", args: []string{"--powershell", "--powershell-command", "Get-Date; Get-Location"}, want: startupOptions{powerShell: true, powerShellCommand: "Get-Date; Get-Location"}},
		{name: "ssh command without address", args: []string{"--ssh-command", "whoami"}, wantErr: true},
		{name: "powershell command without mode", args: []string{"--powershell-command", "Get-Date"}, wantErr: true},
		{name: "conflicting modes", args: []string{"--ssh", "host", "--powershell"}, wantErr: true},
		{name: "unknown argument", args: []string{"--unknown"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseStartupOptions(tt.args)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseStartupOptions() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && got != tt.want {
				t.Fatalf("parseStartupOptions() = %#v, want %#v", got, tt.want)
			}
		})
	}
}
