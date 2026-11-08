# terminal-go

`terminal-go` is a native Windows SSH terminal frontend. Its only interactive child process is the Windows OpenSSH client (`ssh.exe`); it is not a local PowerShell, CMD, or general-purpose shell emulator.

## Current status

The Win32 GUI now has a host entry field, Connect/Disconnect controls, connection status, and a terminal drawing surface. Connect launches only `ssh.exe` inside ConPTY. A background reader feeds output chunks into the incremental VT parser; `WM_CHAR` and common navigation keys are forwarded to the ConPTY input pipe; `WM_SIZE` resizes both the screen grid and pseudoconsole. Drag to select terminal text, use `Ctrl+C` to copy selected text, `Ctrl+V` to paste Unicode clipboard text (line endings are converted for the terminal), and `Ctrl+A` to select the visible screen. Right-click opens a Copy/Paste/Select all menu. When no text is selected, `Ctrl+C` remains the remote interrupt key. Screen updates and painting are synchronized. Performance telemetry is written locally to `%APPDATA%\terminal-go\telemetry.log` (or the system temp directory if the config directory is unavailable). It records SSH output throughput, VT parser time, paint duration/cell and text-run counts, connection startup timing, and slow input writes/queue saturation. It never records terminal contents, clipboard text, or host names.

This is an early interactive slice, not yet a full terminal emulator. Current limitations include basic host-only connection arguments (no GUI controls yet for port, identity file, or arbitrary SSH options), a basic key map, no scrollback or alternate-screen buffer or full VT compatibility, and no end-to-end authentication/server test in this session. The incremental parser handles common CSI cursor/erase controls, OSC title sequences, and ANSI SGR colors (16-color, 256-color, and 24-bit RGB); unsupported terminal modes and control sequences remain outside the current implementation. Unicode input is UTF-8 encoded, but rendering still does not correctly handle surrogate pairs, combining marks, or East Asian display width. Use the standard OpenSSH host-key and authentication prompts; host-key checking is not disabled.

## Requirements

- Windows 10 version 1809 (build 17763) or later for ConPTY.
- The official Go toolchain (`gc`), Windows amd64.
- Windows OpenSSH Client, with `ssh.exe` available in `%WINDIR%\\System32\\OpenSSH` or on `PATH`.

Install Go from <https://go.dev/dl/> and enable/install the Windows OpenSSH Client optional feature from Windows Settings if it is not already present.

## Build and test

From the repository root in PowerShell:

```powershell
go test ./...
go vet ./...
go build ./...
go build -o .\\terminal.exe .\\cmd\\terminal
```

Run `.\\terminal.exe`, enter `user@host` or `host`, and select **Connect**. OpenSSH handles authentication and host-key verification. Select **Disconnect** to close the current session. `terminal.exe` is a local build artifact and should not be committed.

## Security and architecture

- Launch only `ssh.exe`, attached to a Windows ConPTY instance; never launch a local shell.
- Delegate SSH protocol, authentication, host-key verification, and known-hosts handling to the installed OpenSSH client.
- Preserve normal host-key and authentication prompts; never disable host-key checking or silently accept changed host keys.
- Do not invoke a shell to construct process commands. Pass a properly quoted Windows command line directly to `CreateProcessW`.
- Keep process output off the Win32 message loop and close native handles deterministically.

## Project layout

- `cmd/terminal`: Win32 GUI entry point and GUI/session integration
- `internal/winapi`: dynamically loaded Win32 and ConPTY procedures
- `internal/term`: terminal screen buffer and incremental VT parser
- `internal/sshclient`: OpenSSH discovery, Windows argument quoting, and ConPTY process/session lifecycle

See `TODO.md` for remaining implementation priorities and the manual Windows integration test checklist.
