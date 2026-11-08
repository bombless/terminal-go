//go:build windows

package sshclient

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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

// Session owns one ssh.exe process and its ConPTY transport. Output must be
// drained concurrently with input writes to avoid filling the synchronous pipe.
type Session struct {
	console uintptr
	process syscall.Handle
	thread  syscall.Handle
	Input   *os.File
	Output  *os.File
}

// Start creates a ConPTY session and starts only the supplied executable.
// executable should normally come from ResolveExecutable; args are passed
// directly through Windows command-line quoting and never through a shell.
func Start(executable string, args []string, columns, rows int) (*Session, error) {
	if !strings.EqualFold(filepath.Base(executable), "ssh.exe") {
		return nil, fmt.Errorf("refusing to launch %q: the only permitted session executable is ssh.exe", executable)
	}
	if columns < 1 || rows < 1 || columns > 32767 || rows > 32767 {
		return nil, fmt.Errorf("invalid terminal size %dx%d", columns, rows)
	}
	exe16, err := syscall.UTF16PtrFromString(executable)
	if err != nil {
		return nil, fmt.Errorf("encode ssh executable path: %w", err)
	}
	cmd16, err := syscall.UTF16FromString(CommandLine(executable, args))
	if err != nil {
		return nil, fmt.Errorf("encode ssh command line: %w", err)
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
	var pi processInformation
	ok, _, callErr = winapi.CreateProcessW.Call(
		uintptr(unsafe.Pointer(exe16)), uintptr(unsafe.Pointer(&cmd16[0])),
		0, 0, 0, extendedStartupInfoPresent, 0, 0,
		uintptr(unsafe.Pointer(&si)), uintptr(unsafe.Pointer(&pi)))
	runtime.KeepAlive(cmd16)
	runtime.KeepAlive(exe16)
	if ok == 0 {
		return nil, fmt.Errorf("CreateProcessW for ssh.exe failed: %w", callError(callErr))
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
	return &Session{console: console, process: pi.Process, thread: pi.Thread, Input: input, Output: output}, nil
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

// Resize updates the pseudoconsole dimensions in character cells.
func (s *Session) Resize(columns, rows int) error {
	if s == nil || s.console == 0 {
		return fmt.Errorf("SSH session is closed")
	}
	if columns < 1 || rows < 1 || columns > 32767 || rows > 32767 {
		return fmt.Errorf("invalid terminal size %dx%d", columns, rows)
	}
	hr, _, callErr := winapi.ResizePseudoConsole.Call(s.console, uintptr(uint32(columns))|uintptr(uint32(rows))<<16)
	if int32(hr) < 0 {
		return fmt.Errorf("ResizePseudoConsole failed (HRESULT 0x%08X): %w", uint32(hr), callError(callErr))
	}
	return nil
}

// Wait blocks until ssh.exe exits and returns its process exit code.
func (s *Session) Wait() (uint32, error) {
	if s == nil || s.process == 0 {
		return 0, fmt.Errorf("SSH session is closed")
	}
	r, _, callErr := winapi.WaitForSingleObject.Call(uintptr(s.process), infinite)
	if uint32(r) == waitFailed {
		return 0, fmt.Errorf("wait for ssh.exe: %w", callError(callErr))
	}
	if uint32(r) != waitObject0 {
		return 0, fmt.Errorf("unexpected wait result 0x%X", uint32(r))
	}
	var code uint32
	ok, _, callErr := winapi.GetExitCodeProcess.Call(uintptr(s.process), uintptr(unsafe.Pointer(&code)))
	if ok == 0 {
		return 0, fmt.Errorf("GetExitCodeProcess: %w", callError(callErr))
	}
	return code, nil
}

// Close shuts down the ConPTY session and waits briefly for ssh.exe to exit.
// Callers should stop writing and keep the output reader active during shutdown.
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
	if s.console != 0 {
		winapi.ClosePseudoConsole.Call(s.console)
		s.console = 0
	}
	if s.process != 0 {
		r, _, callErr := winapi.WaitForSingleObject.Call(uintptr(s.process), 5000)
		if uint32(r) == waitTimeout {
			ok, _, termErr := winapi.TerminateProcess.Call(uintptr(s.process), 1)
			if ok == 0 && first == nil {
				first = fmt.Errorf("terminate ssh.exe: %w", callError(termErr))
			}
			winapi.WaitForSingleObject.Call(uintptr(s.process), infinite)
		} else if uint32(r) == waitFailed && first == nil {
			first = fmt.Errorf("wait for ssh.exe shutdown: %w", callError(callErr))
		}
	}
	if s.Output != nil {
		if err := s.Output.Close(); err != nil && first == nil {
			first = err
		}
		s.Output = nil
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
