//go:build windows

package sshclient

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"github.com/bombless/terminal-go/internal/winapi"
)

const (
	extendedStartupInfoPresent       = 0x00080000
	procThreadAttributePseudoConsole = 0x00020016
	waitObject0                      = 0
	waitTimeout                      = 258
	waitFailed                       = 0xFFFFFFFF
	infinite                         = 0xFFFFFFFF
	startfUseStdHandles              = 0x00000100
	createNoWindow                   = 0x08000000
	handleFlagInherit                = 0x00000001
)

// coord is the Win32 COORD structure (two signed 16-bit values).
type coord struct{ X, Y int16 }

type startupInfo struct {
	Cb                               uint32
	_                                uint32
	LpReserved                       *uint16
	LpDesktop                        *uint16
	LpTitle                          *uint16
	DwX, DwY, DwXSize, DwYSize       uint32
	DwXCountChars, DwYCountChars     uint32
	DwFillAttribute, DwFlags         uint32
	WShowWindow, CbReserved2         uint16
	_2                               uint32
	LpReserved2                      *byte
	HStdInput, HStdOutput, HStdError syscall.Handle
}

type startupInfoEx struct {
	StartupInfo     startupInfo
	LpAttributeList uintptr
}

type processInformation struct {
	Process, Thread     syscall.Handle
	ProcessID, ThreadID uint32
}

type securityAttributes struct {
	Length             uint32
	_                  uint32
	SecurityDescriptor uintptr
	InheritHandle      int32
	_2                 uint32
}

// Session owns one supported child process and its ConPTY transport. Output
// must be drained concurrently with input writes to avoid filling the
// synchronous pipe.
type Session struct {
	consoleMu sync.Mutex
	console   uintptr
	process   syscall.Handle
	thread    syscall.Handle
	Input     *os.File
	Output    *os.File
	ErrOutput *os.File
	rawPipes  bool

	waitOnce sync.Once
	waitDone chan struct{}
	waitCode uint32
	waitErr  error
}

// allowedExecutable reports whether path names a child process this terminal
// may run. Arguments are passed directly and never interpreted by a shell.
func allowedExecutable(path string) bool {
	switch strings.ToLower(filepath.Base(path)) {
	case "ssh.exe", "pwsh.exe":
		return true
	}
	return false
}

// Start creates a ConPTY session and starts one of the supported executables.
// executable should normally come from ResolveExecutable or ResolvePowerShell;
// args are passed directly through Windows command-line quoting and never
// through a shell.
func Start(executable string, args []string, columns, rows int) (*Session, error) {
	if !allowedExecutable(executable) {
		return nil, fmt.Errorf("refusing to launch %q: supported executables are ssh.exe and pwsh.exe", executable)
	}
	if columns < 1 || rows < 1 || columns > 32767 || rows > 32767 {
		return nil, fmt.Errorf("invalid terminal size %dx%d", columns, rows)
	}
	exe16, err := syscall.UTF16PtrFromString(executable)
	if err != nil {
		return nil, fmt.Errorf("encode executable path: %w", err)
	}
	cmd16, err := syscall.UTF16FromString(CommandLine(executable, args))
	if err != nil {
		return nil, fmt.Errorf("encode command line: %w", err)
	}

	var inputRead, inputWrite, outputRead, outputWrite syscall.Handle
	if err = createPipe(&inputRead, &inputWrite); err != nil {
		return nil, fmt.Errorf("create ConPTY input pipe: %w", err)
	}
	defer closeHandle(&inputRead)
	defer closeHandle(&inputWrite)
	if err = createPipe(&outputRead, &outputWrite); err != nil {
		return nil, fmt.Errorf("create ConPTY output pipe: %w", err)
	}
	defer closeHandle(&outputRead)
	defer closeHandle(&outputWrite)

	var console uintptr
	hr, _, callErr := winapi.CreatePseudoConsole.Call(
		uintptr(uint32(columns))|uintptr(uint32(rows))<<16,
		uintptr(inputRead), uintptr(outputWrite), 0, uintptr(unsafe.Pointer(&console)))
	if int32(hr) < 0 {
		return nil, fmt.Errorf("CreatePseudoConsole failed (HRESULT 0x%08X): %w", uint32(hr), callError(callErr))
	}
	consoleOpen := true
	defer func() {
		if consoleOpen {
			winapi.ClosePseudoConsole.Call(console)
		}
	}()

	var required uintptr
	winapi.InitializeProcThreadAttributeList.Call(0, 1, 0, uintptr(unsafe.Pointer(&required)))
	if required == 0 {
		return nil, fmt.Errorf("InitializeProcThreadAttributeList size query failed: %w", syscall.GetLastError())
	}
	attributeMemory := make([]byte, required)
	attributeList := uintptr(unsafe.Pointer(&attributeMemory[0]))
	ok, _, callErr := winapi.InitializeProcThreadAttributeList.Call(attributeList, 1, 0, uintptr(unsafe.Pointer(&required)))
	if ok == 0 {
		runtime.KeepAlive(attributeMemory)
		return nil, fmt.Errorf("InitializeProcThreadAttributeList failed: %w", callError(callErr))
	}
	attributeInitialized := true
	defer func() {
		if attributeInitialized {
			winapi.DeleteProcThreadAttributeList.Call(attributeList)
		}
		runtime.KeepAlive(attributeMemory)
	}()

	ok, _, callErr = winapi.UpdateProcThreadAttribute.Call(attributeList, 0, procThreadAttributePseudoConsole, console, unsafe.Sizeof(console), 0, 0)
	if ok == 0 {
		return nil, fmt.Errorf("UpdateProcThreadAttribute(PSEUDOCONSOLE) failed: %w", callError(callErr))
	}

	si := startupInfoEx{LpAttributeList: attributeList}
	si.StartupInfo.Cb = uint32(unsafe.Sizeof(si))
	// Tell CreateProcess to initialize the child's standard handles for the
	// pseudoconsole. Without this, console applications such as Windows OpenSSH
	// may retain the parent's redirected handles instead of the ConPTY streams.
	si.StartupInfo.DwFlags = startfUseStdHandles
	var pi processInformation
	ok, _, callErr = winapi.CreateProcessW.Call(
		uintptr(unsafe.Pointer(exe16)), uintptr(unsafe.Pointer(&cmd16[0])),
		0, 0, 0, extendedStartupInfoPresent, 0, 0,
		uintptr(unsafe.Pointer(&si)), uintptr(unsafe.Pointer(&pi)))
	runtime.KeepAlive(cmd16)
	runtime.KeepAlive(exe16)
	if ok == 0 {
		return nil, fmt.Errorf("CreateProcessW for %s failed: %w", filepath.Base(executable), callError(callErr))
	}
	// The process has duplicated the pseudoconsole's channel handles.
	closeHandle(&inputRead)
	closeHandle(&outputWrite)
	input := os.NewFile(uintptr(inputWrite), "conpty-input")
	inputWrite = 0 // ownership transferred to os.File; prevent deferred double-close
	output := os.NewFile(uintptr(outputRead), "conpty-output")
	outputRead = 0 // ownership transferred to os.File; prevent deferred double-close
	if input == nil || output == nil {
		if input != nil {
			input.Close()
		} else {
			closeHandle(&inputWrite)
		}
		if output != nil {
			output.Close()
		} else {
			closeHandle(&outputRead)
		}
		winapi.CloseHandle.Call(uintptr(pi.Thread))
		winapi.CloseHandle.Call(uintptr(pi.Process))
		return nil, fmt.Errorf("wrap ConPTY pipe handles")
	}
	consoleOpen = false
	return &Session{console: console, process: pi.Process, thread: pi.Thread, Input: input, Output: output, waitDone: make(chan struct{})}, nil
}

// StartRaw starts ssh.exe with ordinary inherited stdin/stdout/stderr pipes.
// It avoids ConPTY's terminal-output processing so Kitty APC bytes reach the
// VT parser unchanged. Callers should pass -tt to request a remote PTY.
func StartRaw(executable string, args []string) (*Session, error) {
	if !strings.EqualFold(filepath.Base(executable), "ssh.exe") {
		return nil, fmt.Errorf("raw-pipe transport is restricted to ssh.exe")
	}
	exe16, err := syscall.UTF16PtrFromString(executable)
	if err != nil {
		return nil, fmt.Errorf("encode executable path: %w", err)
	}
	cmd16, err := syscall.UTF16FromString(CommandLine(executable, args))
	if err != nil {
		return nil, fmt.Errorf("encode command line: %w", err)
	}

	var childStdin, parentStdin, parentStdout, childStdout, parentStderr, childStderr syscall.Handle
	closeAll := func() {
		for _, h := range []*syscall.Handle{&childStdin, &parentStdin, &parentStdout, &childStdout, &parentStderr, &childStderr} {
			closeHandle(h)
		}
	}
	defer closeAll()
	if err = createInheritablePipe(&childStdin, &parentStdin); err != nil {
		return nil, fmt.Errorf("create SSH stdin pipe: %w", err)
	}
	if err = createInheritablePipe(&parentStdout, &childStdout); err != nil {
		return nil, fmt.Errorf("create SSH stdout pipe: %w", err)
	}
	if err = createInheritablePipe(&parentStderr, &childStderr); err != nil {
		return nil, fmt.Errorf("create SSH stderr pipe: %w", err)
	}
	for _, h := range []syscall.Handle{parentStdin, parentStdout, parentStderr} {
		if err = setHandleNonInheritable(h); err != nil {
			return nil, fmt.Errorf("protect parent pipe handle: %w", err)
		}
	}
	si := startupInfo{Cb: uint32(unsafe.Sizeof(startupInfo{})), DwFlags: startfUseStdHandles, HStdInput: childStdin, HStdOutput: childStdout, HStdError: childStderr}
	var pi processInformation
	ok, _, callErr := winapi.CreateProcessW.Call(
		uintptr(unsafe.Pointer(exe16)), uintptr(unsafe.Pointer(&cmd16[0])),
		0, 0, 1, createNoWindow, 0, 0,
		uintptr(unsafe.Pointer(&si)), uintptr(unsafe.Pointer(&pi)))
	runtime.KeepAlive(cmd16)
	runtime.KeepAlive(exe16)
	if ok == 0 {
		return nil, fmt.Errorf("CreateProcessW for ssh.exe (raw pipes) failed: %w", callError(callErr))
	}
	closeHandle(&childStdin)
	closeHandle(&childStdout)
	closeHandle(&childStderr)
	input := os.NewFile(uintptr(parentStdin), "ssh-stdin")
	parentStdin = 0
	output := os.NewFile(uintptr(parentStdout), "ssh-stdout")
	parentStdout = 0
	errOutput := os.NewFile(uintptr(parentStderr), "ssh-stderr")
	parentStderr = 0
	if input == nil || output == nil || errOutput == nil {
		if input != nil {
			_ = input.Close()
		}
		if output != nil {
			_ = output.Close()
		}
		if errOutput != nil {
			_ = errOutput.Close()
		}
		winapi.TerminateProcess.Call(uintptr(pi.Process), 1)
		winapi.WaitForSingleObject.Call(uintptr(pi.Process), infinite)
		winapi.CloseHandle.Call(uintptr(pi.Thread))
		winapi.CloseHandle.Call(uintptr(pi.Process))
		return nil, fmt.Errorf("wrap SSH raw-pipe handles")
	}
	return &Session{process: pi.Process, thread: pi.Thread, Input: input, Output: output, ErrOutput: errOutput, rawPipes: true, waitDone: make(chan struct{})}, nil
}

func createInheritablePipe(read, write *syscall.Handle) error {
	sa := securityAttributes{Length: uint32(unsafe.Sizeof(securityAttributes{})), InheritHandle: 1}
	ok, _, callErr := winapi.CreatePipe.Call(uintptr(unsafe.Pointer(read)), uintptr(unsafe.Pointer(write)), uintptr(unsafe.Pointer(&sa)), 0)
	if ok == 0 {
		return callError(callErr)
	}
	return nil
}

func setHandleNonInheritable(handle syscall.Handle) error {
	ok, _, callErr := winapi.SetHandleInformation.Call(uintptr(handle), handleFlagInherit, 0)
	if ok == 0 {
		return callError(callErr)
	}
	return nil
}

func createPipe(read, write *syscall.Handle) error {
	ok, _, callErr := winapi.CreatePipe.Call(uintptr(unsafe.Pointer(read)), uintptr(unsafe.Pointer(write)), 0, 0)
	if ok == 0 {
		return callError(callErr)
	}
	return nil
}

func closeHandle(h *syscall.Handle) {
	if *h != 0 {
		winapi.CloseHandle.Call(uintptr(*h))
		*h = 0
	}
}

func callError(err error) error {
	if err != nil && err != syscall.Errno(0) {
		return err
	}
	return syscall.GetLastError()
}

// closePseudoConsole is safe to call from the process waiter and Close.
func (s *Session) closePseudoConsole() {
	if s == nil {
		return
	}
	s.consoleMu.Lock()
	if s.console != 0 {
		winapi.ClosePseudoConsole.Call(s.console)
		s.console = 0
	}
	s.consoleMu.Unlock()
}

// Resize updates the pseudoconsole dimensions in character cells.
func (s *Session) Resize(columns, rows int) error {
	if s == nil {
		return fmt.Errorf("session is closed")
	}
	if columns < 1 || rows < 1 || columns > 32767 || rows > 32767 {
		return fmt.Errorf("invalid terminal size %dx%d", columns, rows)
	}
	if s.rawPipes {
		return fmt.Errorf("SSH raw-pipe session cannot yet send remote PTY window-size updates")
	}
	s.consoleMu.Lock()
	defer s.consoleMu.Unlock()
	if s.console == 0 {
		return fmt.Errorf("session is closed")
	}
	hr, _, callErr := winapi.ResizePseudoConsole.Call(s.console, uintptr(uint32(columns))|uintptr(uint32(rows))<<16)
	if int32(hr) < 0 {
		return fmt.Errorf("ResizePseudoConsole failed (HRESULT 0x%08X): %w", uint32(hr), callError(callErr))
	}
	return nil
}

// Wait blocks until the child process exits, closes the pseudoconsole to
// release its output stream, and returns the child's exit code. The shared
// waiter lets the output reader and explicit shutdown path wait concurrently.
func (s *Session) Wait() (uint32, error) {
	if s == nil || s.process == 0 || s.waitDone == nil {
		return 0, fmt.Errorf("session is closed")
	}
	s.waitOnce.Do(func() {
		go func() {
			r, _, callErr := winapi.WaitForSingleObject.Call(uintptr(s.process), infinite)
			if uint32(r) == waitFailed {
				s.waitErr = fmt.Errorf("wait for child process: %w", callError(callErr))
			} else if uint32(r) != waitObject0 {
				s.waitErr = fmt.Errorf("unexpected wait result 0x%X", uint32(r))
			} else {
				ok, _, exitErr := winapi.GetExitCodeProcess.Call(uintptr(s.process), uintptr(unsafe.Pointer(&s.waitCode)))
				if ok == 0 {
					s.waitErr = fmt.Errorf("GetExitCodeProcess: %w", callError(exitErr))
				}
			}
			if !s.rawPipes {
				// ConPTY keeps its output channel alive until HPCON is closed.
				// Close it only after the child exits so final VT output can drain.
				s.closePseudoConsole()
			}
			close(s.waitDone)
		}()
	})
	<-s.waitDone
	return s.waitCode, s.waitErr
}

// Close shuts down the session, waits for the process monitor, then releases
// the output handles. Keep the output reader active until this method returns.
func (s *Session) Close() error {
	if s == nil {
		return nil
	}
	var first error
	if s.Input != nil {
		if err := s.Input.Close(); err != nil {
			first = err
		}
		s.Input = nil
	}
	if !s.rawPipes {
		// Closing HPCON terminates an attached child and allows its output pipe
		// to reach EOF. The process waiter may already have done this.
		s.closePseudoConsole()
	}
	if s.process != 0 {
		r, _, callErr := winapi.WaitForSingleObject.Call(uintptr(s.process), 5000)
		if uint32(r) == waitTimeout {
			ok, _, termErr := winapi.TerminateProcess.Call(uintptr(s.process), 1)
			if ok == 0 && first == nil {
				first = fmt.Errorf("terminate child process: %w", callError(termErr))
			}
			winapi.WaitForSingleObject.Call(uintptr(s.process), infinite)
		} else if uint32(r) == waitFailed && first == nil {
			first = fmt.Errorf("wait for child process shutdown: %w", callError(callErr))
		}
		if uint32(r) != waitFailed {
			if _, err := s.Wait(); err != nil && first == nil {
				first = err
			}
		}
	}
	if s.Output != nil {
		if err := s.Output.Close(); err != nil && first == nil {
			first = err
		}
		s.Output = nil
	}
	if s.ErrOutput != nil {
		if err := s.ErrOutput.Close(); err != nil && first == nil {
			first = err
		}
		s.ErrOutput = nil
	}
	if s.thread != 0 {
		winapi.CloseHandle.Call(uintptr(s.thread))
		s.thread = 0
	}
	if s.process != 0 {
		winapi.CloseHandle.Call(uintptr(s.process))
		s.process = 0
	}
	return first
}
