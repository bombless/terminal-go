# terminal-go

`terminal-go` is a native Windows terminal frontend for the Windows OpenSSH client (`ssh.exe`) and Windows PowerShell 7 (`pwsh.exe`). Each session runs one supported executable inside ConPTY; it is not a general-purpose shell emulator and never invokes CMD or arbitrary commands.

## Current status

`terminal.exe` is a GUI-subsystem program with an embedded multi-size icon, so launching it does not open a console window and the icon appears in Explorer, the taskbar, and the title bar. The Win32 GUI now has a host entry field, Connect/Disconnect controls, a PowerShell button for local sessions, connection status, and a terminal drawing surface. Connect launches `ssh.exe` and PowerShell launches a local `pwsh.exe`, both inside ConPTY. A background reader feeds output chunks into the incremental VT parser; `WM_CHAR` and common navigation keys are forwarded to the ConPTY input pipe; `WM_SIZE` resizes both the screen grid and pseudoconsole. Drag to select terminal text, use `Ctrl+C` to copy selected text, `Ctrl+V` to paste Unicode clipboard text (line endings are converted for the terminal), and `Ctrl+A` to select the visible screen. Right-click opens a Copy/Paste/Select all menu. When no text is selected, `Ctrl+C` remains the remote interrupt key. Screen updates and painting are synchronized. Performance telemetry is written locally to `%APPDATA%\terminal-go\telemetry.log` (or the system temp directory if the config directory is unavailable). It records session output throughput, VT parser time, paint duration/cell and text-run counts, connection startup timing, and slow input writes/queue saturation. It never records terminal contents, clipboard text, or host names.

This is an early interactive slice, not yet a full terminal emulator. Current limitations include basic host-only connection arguments (no GUI controls yet for port, identity file, or arbitrary SSH options), a basic key map, no scrollback or alternate-screen buffer or full VT compatibility, and no end-to-end authentication/server test in this session. The incremental parser handles common CSI cursor/erase controls, OSC title sequences, and ANSI SGR colors (16-color, 256-color, and 24-bit RGB); unsupported terminal modes and control sequences remain outside the current implementation. Unicode input is UTF-8 encoded, and East Asian wide characters occupy two cells, but rendering still does not correctly handle surrogate pairs or combining marks. Use the standard OpenSSH host-key and authentication prompts; host-key checking is not disabled.

## Requirements

- Windows 10 version 1809 (build 17763) or later for ConPTY.
- The official Go toolchain (`gc`), Windows amd64.
- Windows OpenSSH Client, with `ssh.exe` available in `%WINDIR%\\System32\\OpenSSH` or on `PATH`.
- PowerShell 7, with `pwsh.exe` available in `%ProgramFiles%\\PowerShell\\7` or on `PATH`, for local sessions (optional).

Install Go from <https://go.dev/dl/>, enable/install the Windows OpenSSH Client optional feature from Windows Settings if it is not already present, and install PowerShell 7 from <https://aka.ms/powershell> if you want local PowerShell sessions.

## Build and test

From the repository root in PowerShell:

```powershell
go test ./...
go vet ./...
go build ./...
go build -ldflags "-H=windowsgui" -o .\\terminal.exe .\\cmd\\terminal
```

The `-H=windowsgui` flag is required: without it the linker produces a console-subsystem executable, which opens an extra console window. The app icon (`terminal.ico`) and the common-controls v6 manifest (`terminal.manifest`, needed for the host field's placeholder text) are compiled into `cmd/terminal/rsrc_windows_amd64.syso`, which is kept in the source tree so regular builds need no resource tool. Regenerate it only after changing the icon or manifest, with LLVM-MinGW or any Win32 `windres`:

```powershell
cd .\\cmd\\terminal
windres -O coff --target=pe-x86-64 -i terminal.rc -o rsrc_windows_amd64.syso
```

Run `.\\terminal.exe`, enter `user@host` or `host`, and select **Connect**. OpenSSH handles authentication and host-key verification. Select **PowerShell** to open a local PowerShell 7 session instead. Select **Disconnect** to close the current session. `terminal.exe` is a local build artifact and should not be committed.

## Security and architecture

- Launch only the resolved `ssh.exe` or `pwsh.exe`, attached to a Windows ConPTY instance; never launch `cmd.exe` or arbitrary commands.
- Delegate SSH protocol, authentication, host-key verification, and known-hosts handling to the installed OpenSSH client.
- Preserve normal host-key and authentication prompts; never disable host-key checking or silently accept changed host keys.
- Do not invoke a shell to construct process commands. Pass a properly quoted Windows command line directly to `CreateProcessW`.
- Keep process output off the Win32 message loop and close native handles deterministically.

## Project layout

- `cmd/terminal`: Win32 GUI entry point, GUI/session integration, and the app icon/window resource (`terminal.png`, `terminal.ico`, `terminal.rc`, `rsrc_windows_amd64.syso`)
- `internal/winapi`: dynamically loaded Win32 and ConPTY procedures
- `internal/term`: terminal screen buffer and incremental VT parser
- `internal/sshclient`: executable discovery (OpenSSH and PowerShell), Windows argument quoting, and ConPTY process/session lifecycle

See `TODO.md` for remaining implementation priorities and the manual Windows integration test checklist.
