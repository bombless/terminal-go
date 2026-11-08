package sshclient

import "testing"

func TestCommandLineQuotesWindowsArguments(t *testing.T) {
	tests := []struct{ name, input, want string }{
		{"plain", "user@example.com", "user@example.com"},
		{"spaces", "C:\\Program Files\\OpenSSH\\ssh.exe", `"C:\Program Files\OpenSSH\ssh.exe"`},
		{"embedded quote", `a"b`, `"a\"b"`},
		{"trailing slash", `C:\path with space\`, `"C:\path with space\\"`},
		{"empty", "", `""`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := quoteWindowsArg(tt.input); got != tt.want {
				t.Errorf("quoteWindowsArg(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestCommandLineIncludesExecutableAndArguments(t *testing.T) {
	got := CommandLine(`C:\Program Files\OpenSSH\ssh.exe`, []string{"-p", "2222", "user@example.com"})
	want := `"C:\Program Files\OpenSSH\ssh.exe" -p 2222 user@example.com`
	if got != want {
		t.Fatalf("CommandLine() = %q, want %q", got, want)
	}
}
