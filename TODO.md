# TODO — terminal-go Windows SSH Terminal

> Handoff note for the next ChatGPT session. Work on branch `feature/windows-terminal` (PR #1).
> The intended product is a native Windows terminal window whose only interactive backend is the local `ssh.exe` client.

## Product goal

Build a lightweight native Windows GUI terminal that launches the Windows OpenSSH client (`ssh.exe`) and displays its interactive remote terminal session. Do **not** implement or launch a local PowerShell, CMD, or other local shell. The application is an SSH terminal frontend, not a general local shell emulator.

Use the **official Go toolchain (gc)**. GCCGO is no longer a project requirement. Do not spend time installing GCCGO or validating GCCGO ABI compatibility. Do not use CGO unless a concrete, documented requirement makes it unavoidable.

## Current repository and verified environment

- Branch: `feature/windows-terminal`.
- Module: `github.com/bombless/terminal-go`.
- Existing PR: https://github.com/bombless/terminal-go/pull/1
- `internal/term/screen.go`: fixed-size cell buffer and screen operations.
- `internal/term/vt.go`: incremental ANSI/VT parser, UTF-8 decoding, CSI cursor operations, erase operations, and basic 16-color SGR.
- `internal/term/screen_test.go`, `internal/term/vt_test.go`: screen and parser tests.
- `internal/winapi/procs_windows.go`: dynamically loaded Win32 procedures.
- `cmd/terminal/main.go`: Win32 host-entry/Connect/Disconnect toolbar, connection status, background ConPTY output reader feeding the VT parser, basic keyboard forwarding, synchronized paint, and screen/ConPTY resize. Live authentication against a remote SSH server has not yet been exercised.
- Build environment used: Windows amd64, official `go1.27.0 windows/amd64`, `CGO_ENABLED=1`.
- Verified successfully on `desktop-01`: `go test ./...`, `go vet ./...`, `go build ./...`, and `go build -o .\terminal.exe .\cmd\terminal`.
- The generated `terminal.exe` is a local untracked build artifact; do not commit it.
- `internal/sshclient/conpty_windows.go` now provides a Windows amd64 ConPTY process/session wrapper and a runtime smoke test using the installed `ssh.exe -V`; the GUI is not yet wired to this wrapper and still lacks interactive input/output integration.

## Architecture and scope decisions

1. The only child program to launch is `ssh.exe` (normally Windows OpenSSH at `%WINDIR%\System32\OpenSSH\ssh.exe`, but resolve it robustly and allow configuration/override).
2. Do not launch `powershell.exe`, `pwsh.exe`, `cmd.exe`, WSL shells, or a local shell as a session backend.
3. Use Windows ConPTY as the local pseudo-terminal between the GUI and `ssh.exe`. ConPTY is still required: interactive OpenSSH needs a terminal for input, terminal dimensions, and VT behavior. ConPTY hosts `ssh.exe` only; it is not being used to host a local shell.
4. `ssh.exe` owns SSH protocol, authentication, host-key verification, remote PTY negotiation, and remote process/session behavior. Do not implement SSH protocol or authentication in Go.
5. Launch `ssh.exe` with `CreateProcessW` and `STARTUPINFOEXW`, attaching the ConPTY through `PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE`. Prefer argument-safe Windows command-line construction; never concatenate untrusted input into a shell command.
6. Preserve normal OpenSSH behavior: host-key prompts, password/passphrase prompts, keyboard-interactive auth, key/agent use, known_hosts checks, and remote interactive TUI applications. Do not bypass host-key verification.
7. Keep UI/message handling responsive. Process I/O must not block the Win32 message loop. Manage process, pseudoconsole, pipes, and worker goroutines/threads with deterministic shutdown and error handling.

## TODO priorities

### P0 — official Go baseline and project cleanup

- [x] Build and test with official Go on Windows amd64 (`go test ./...`, `go vet ./...`, `go build ./...`).
- [x] Produce a Windows GUI executable with `go build -o .\terminal.exe .\cmd\terminal`.
- [x] Remove stale GCCGO-only claims from README and GUI labels; remaining TODO mentions explicitly mark GCCGO out of scope.
- [x] Document official Go installation/build/test commands and the Windows OpenSSH prerequisite in README.
- [x] Add `internal/sshclient.ResolveExecutable` for explicit override, standard Windows OpenSSH path, and PATH lookup with actionable errors.

### P1 — launch ssh.exe in ConPTY

- [x] Add the required Win32 declarations/procedure bindings for ConPTY and process creation: `CreatePseudoConsole`, `ResizePseudoConsole`, `ClosePseudoConsole`, `CreatePipe`, `InitializeProcThreadAttributeList`, `UpdateProcThreadAttribute`, `DeleteProcThreadAttributeList`, `CreateProcessW`, `GetExitCodeProcess`, `WaitForSingleObject`, and required handle-management APIs.
- [x] Define and verify the Go layouts/constants for `COORD`, `STARTUPINFOEXW`, `PROCESS_INFORMATION`, `SECURITY_ATTRIBUTES`, and `PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE` on Windows amd64. Keep pointer sizes/alignment correct.
- [x] Create ConPTY input/output pipes and a pseudoconsole with the initial requested dimensions.
- [x] Resolve `ssh.exe` without invoking a shell. Support the standard Windows OpenSSH location and PATH lookup; allow an explicit executable override.
- [ ] Add a higher-level argument builder for common usage (`user@host`, `-p`, `-i`, and additional SSH options); the current `Start` accepts an argument slice and quotes it safely for `CreateProcessW`. Do not log secrets or expose private-key contents.
- [x] Launch only `ssh.exe` attached to the pseudoconsole via an extended startup attribute list; reject any other executable basename.
- [x] Implement asynchronous output reading from the ConPTY pipe and feed byte chunks into the existing incremental VT parser.
- [x] Implement UTF-8/control-key input forwarding through a dedicated FIFO writer goroutine; handle partial writes, errors, and broken pipes without blocking the Win32 message loop.
- [ ] Preserve interactive SSH prompts and remote TUI behavior; ensure the application does not swallow or reinterpret SSH authentication/host-key prompts.
- [x] Handle child exit, startup failure, pipe closure, and cleanup on the implemented process/session wrapper paths. Close process/thread handles, pipes, attribute lists, and pseudoconsole exactly once.
- [x] Add an explicit Connect/Disconnect lifecycle and useful connection error/status display in the GUI.

### P2 — terminal interaction and resizing

- [ ] Handle `WM_SIZE`, calculate terminal rows/columns from client size and cell metrics, and call `ResizePseudoConsole`.
- [x] Resize the screen buffer consistently with ConPTY; screen resize, parser updates, and painting share a mutex.
- [ ] Implement keyboard input mapping for printable text, Enter, Backspace, Tab, Escape, arrows, Home/End, Insert/Delete, PageUp/PageDown, and Ctrl combinations commonly used by remote shells/TUIs.
- [ ] Ensure key events and text input handle Unicode correctly; document any Windows input limitations.
- [ ] Improve rendering: font selection, cell metrics, foreground/background colors, cursor visibility/position, scrolling, and invalidation.
- [ ] Correctly handle UTF-8 split across reads, non-BMP characters, combining marks, and East Asian wide-character cell width.
- [ ] Implement scrollback and selection/copy/paste if required for a usable SSH terminal.
- [ ] Handle DPI changes and minimum window size.

### P3 — robustness and usability

- [x] Add tests for SSH argument quoting and executable discovery.
- [ ] Add tests for process lifecycle state, input mapping, and parser integration.
- [ ] Add a manual Windows test checklist covering password auth, key auth, host-key first-use/change prompts, keyboard-interactive auth, disconnects, network loss, resize, and remote TUI programs (for example `vim`, `top`, and `htop` where installed remotely).
- [ ] Verify remote UTF-8 and color behavior against common SSH servers.
- [ ] Add configurable default font, font size, colors, initial window size, and SSH executable path.
- [ ] Add structured diagnostic logging that avoids credentials and sensitive command-line material.
- [ ] Add Windows CI using the official Go toolchain.
- [ ] Update README with setup, build, connection examples, supported behavior, and known limitations.

## Important implementation constraints

- Official Go compiler only; GCCGO compatibility is explicitly out of scope.
- No local PowerShell/CMD/local shell integration. `ssh.exe` is the only session child process.
- No CGO by default. Prefer Win32 syscall/dynamic procedure bindings already used by the repository.
- ConPTY is the local terminal transport for `ssh.exe`, not a local shell host.
- Do not implement SSH protocol, cryptography, host-key storage, or authentication in Go; delegate these to the installed OpenSSH client.
- Never disable host-key checking or silently accept changed host keys.
- Keep the Win32 UI thread responsive; perform blocking process I/O away from the message loop.
- Ensure deterministic cleanup of all native handles and synchronization between I/O, parser, screen, resize, and paint paths.
- Test on the actual Windows target. A successful cross-platform compile is not a substitute for Windows runtime validation.

## Suggested next steps

1. Wire `internal/sshclient.Session` into the Win32 GUI with an explicit Connect/Disconnect lifecycle and visible connection status.
2. Drain `Session.Output` on a dedicated goroutine, feed byte chunks into the incremental VT parser under screen synchronization, and invalidate the window without blocking the message loop.
3. Add a dedicated input writer and map `WM_CHAR`/`WM_KEYDOWN` to UTF-8 and terminal control sequences; preserve Unicode and SSH authentication prompts.
4. Handle `WM_SIZE`, resize both the screen grid and ConPTY, and synchronize resize, parser, and paint operations.
5. Add tests for argument construction, process lifecycle, input mapping, and parser integration; then manually verify password/key auth, host-key prompts, network loss, resize, and remote TUI programs.
6. Keep `terminal.exe` untracked, run `CGO_ENABLED=0 go test ./...`, `go vet ./...`, and `go build ./...`, and continue focused work on PR #1.

## Current PR

Continue work on PR #1 unless there is a clear reason to split the work. Keep commits focused and update this TODO as each verified item is completed.
