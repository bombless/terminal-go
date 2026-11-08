//go:build windows

package main

import (
	"fmt"
	"strings"
	"sync"
	"syscall"
	"unicode/utf8"
	"unsafe"

	"github.com/bombless/terminal-go/internal/sshclient"
	"github.com/bombless/terminal-go/internal/term"
	"github.com/bombless/terminal-go/internal/winapi"
)

const (
	csVRedraw          = 0x0001
	csHRedraw          = 0x0002
	wsOverlappedWindow = 0x00CF0000
	wsVisible          = 0x10000000
	wsChild            = 0x40000000
	wsBorder           = 0x00800000
	wsTabStop          = 0x00010000
	esAutoHScroll      = 0x0080
	bsPushButton       = 0
	ssLeft             = 0
	wmCreate           = 0x0001
	wmDestroy          = 0x0002
	wmSize             = 0x0005
	wmSetFocus         = 0x0007
	wmChar             = 0x0102
	wmKeyDown          = 0x0100
	wmCommand          = 0x0111
	wmPaint            = 0x000F
	colorWindow        = 5
	swShow             = 5

	idHost       = 1001
	idConnect    = 1002
	idDisconnect = 1003
	idStatus     = 1004

	cellWidth     = 10
	cellHeight    = 20
	toolbarHeight = 48
)

type point struct{ X, Y int32 }
type msg struct {
	Hwnd           uintptr
	Message        uint32
	WParam, LParam uintptr
	Time           uint32
	Pt             point
}
type rect struct{ Left, Top, Right, Bottom int32 }
type paintStruct struct {
	Hdc                uintptr
	Erase              int32
	Paint              rect
	Restore, IncUpdate int32
	Reserved           [32]byte
}
type wndClassEx struct {
	Size, Style                        uint32
	WndProc                            uintptr
	ClsExtra, WndExtra                 int32
	Instance, Icon, Cursor, Background uintptr
	MenuName, ClassName                *uint16
	IconSm                             uintptr
}

type pendingInput struct {
	session *sshclient.Session
	data    []byte
}

var (
	className                                              = syscall.StringToUTF16Ptr("TerminalGoWindow")
	windowTitle                                            = syscall.StringToUTF16Ptr("terminal-go - SSH Terminal")
	hostEdit, connectButton, disconnectButton, statusLabel uintptr
	screenMu                                               sync.Mutex
	screen                                                 = term.NewScreen(96, 30)
	parser                                                 = term.NewParser(screen)
	sessionMu                                              sync.Mutex
	inputWriteMu                                           sync.Mutex
	session                                                *sshclient.Session
	inputQueue                                             = make(chan pendingInput, 256)
)

func windowProc(hwnd uintptr, message uint32, wParam, lParam uintptr) uintptr {
	switch message {
	case wmCreate:
		createToolbar(hwnd)
		return 0
	case wmSize:
		layoutToolbar(hwnd, int32(uint16(lParam)), int32(uint16(lParam>>16)))
		resizeTerminal(hwnd)
		return 0
	case wmCommand:
		switch uint16(wParam) {
		case idConnect:
			connectSSH(hwnd)
		case idDisconnect:
			disconnectSSH("Disconnected")
		}
		return 0
	case wmChar:
		if wParam == 3 { // Ctrl+C is sent to the remote terminal, not handled locally.
			writeInput([]byte{3})
		} else if r := rune(wParam); r >= 0x20 || r == '\r' || r == '\n' || r == '\t' || r == 0x08 || r == 0x1b {
			buf := make([]byte, utf8.RuneLen(r))
			if r == '\r' || r == '\n' {
				buf = []byte{'\r'}
			} else if r == '\t' {
				buf = []byte{'\t'}
			} else if r == 0x08 {
				buf = []byte{0x7f}
			} else if r == 0x1b {
				buf = []byte{0x1b}
			} else {
				utf8.EncodeRune(buf, r)
			}
			writeInput(buf)
		}
		return 0
	case wmKeyDown:
		if seq := keySequence(wParam); len(seq) != 0 {
			writeInput(seq)
			return 0
		}
	case wmPaint:
		paint(hwnd)
		return 0
	case wmDestroy:
		disconnectSSH("Window closed")
		winapi.PostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := winapi.DefWindowProcW.Call(hwnd, uintptr(message), wParam, lParam)
	return r
}

func createToolbar(hwnd uintptr) {
	create := winapi.CreateWindowExW
	hostClass := syscall.StringToUTF16Ptr("EDIT")
	buttonClass := syscall.StringToUTF16Ptr("BUTTON")
	staticClass := syscall.StringToUTF16Ptr("STATIC")
	hostText := syscall.StringToUTF16Ptr("")
	connectText := syscall.StringToUTF16Ptr("Connect")
	disconnectText := syscall.StringToUTF16Ptr("Disconnect")
	statusText := syscall.StringToUTF16Ptr("Not connected")
	hostEdit, _, _ = create.Call(0, uintptr(unsafe.Pointer(hostClass)), uintptr(unsafe.Pointer(hostText)), wsChild|wsVisible|wsBorder|wsTabStop|esAutoHScroll, 12, 10, 300, 26, hwnd, idHost, 0, 0)
	connectButton, _, _ = create.Call(0, uintptr(unsafe.Pointer(buttonClass)), uintptr(unsafe.Pointer(connectText)), wsChild|wsVisible|wsTabStop|bsPushButton, 322, 10, 88, 26, hwnd, idConnect, 0, 0)
	disconnectButton, _, _ = create.Call(0, uintptr(unsafe.Pointer(buttonClass)), uintptr(unsafe.Pointer(disconnectText)), wsChild|wsVisible|wsTabStop|bsPushButton, 418, 10, 96, 26, hwnd, idDisconnect, 0, 0)
	statusLabel, _, _ = create.Call(0, uintptr(unsafe.Pointer(staticClass)), uintptr(unsafe.Pointer(statusText)), wsChild|wsVisible|ssLeft, 526, 14, 430, 22, hwnd, idStatus, 0, 0)
}

func layoutToolbar(hwnd uintptr, width, height int32) {
	if width < 1 {
		return
	}
	move := winapi.User32.NewProc("MoveWindow")
	statusWidth := width - 538
	if statusWidth < 80 {
		statusWidth = 80
	}
	move.Call(hostEdit, 12, 10, 300, 26, 1)
	move.Call(connectButton, 322, 10, 88, 26, 1)
	move.Call(disconnectButton, 418, 10, 96, 26, 1)
	move.Call(statusLabel, 526, 14, uintptr(statusWidth), 22, 1)
	_ = hwnd
	_ = height
}

func setStatus(value string) {
	if statusLabel != 0 {
		p, err := syscall.UTF16PtrFromString(value)
		if err == nil {
			winapi.User32.NewProc("SetWindowTextW").Call(statusLabel, uintptr(unsafe.Pointer(p)))
		}
	}
}

func connectSSH(hwnd uintptr) {
	sessionMu.Lock()
	if session != nil {
		sessionMu.Unlock()
		setStatus("Already connected")
		return
	}
	sessionMu.Unlock()
	length, _, _ := winapi.User32.NewProc("GetWindowTextLengthW").Call(hostEdit)
	buffer := make([]uint16, int(length)+1)
	winapi.User32.NewProc("GetWindowTextW").Call(hostEdit, uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)))
	host := strings.TrimSpace(syscall.UTF16ToString(buffer))
	if host == "" {
		setStatus("Enter user@host or host")
		return
	}
	setStatus("Starting SSH...")
	executable, err := sshclient.ResolveExecutable("")
	if err != nil {
		setStatus("SSH unavailable: " + err.Error())
		return
	}
	s, err := sshclient.Start(executable, []string{host}, screen.Width, screen.Height)
	if err != nil {
		setStatus("Connect failed: " + err.Error())
		return
	}
	sessionMu.Lock()
	if session != nil {
		sessionMu.Unlock()
		s.Close()
		return
	}
	session = s
	sessionMu.Unlock()
	setStatus("Connected: " + host)
	winapi.User32.NewProc("SetFocus").Call(hwnd)
	go readSessionOutput(s, hwnd)
}

func readSessionOutput(s *sshclient.Session, hwnd uintptr) {
	buf := make([]byte, 8192)
	for {
		n, err := s.Output.Read(buf)
		if n > 0 {
			screenMu.Lock()
			parser.Feed(buf[:n])
			screenMu.Unlock()
			winapi.User32.NewProc("InvalidateRect").Call(hwnd, 0, 0)
		}
		if err != nil {
			break
		}
	}
	sessionMu.Lock()
	current := session == s
	if current {
		session = nil
	}
	sessionMu.Unlock()
	if !current {
		return
	} // A manual disconnect already owns shutdown/status.
	code, waitErr := s.Wait()
	inputWriteMu.Lock()
	_ = s.Close()
	inputWriteMu.Unlock()
	if waitErr != nil {
		setStatus("SSH ended: " + waitErr.Error())
	} else {
		setStatus(fmt.Sprintf("SSH exited (%d)", code))
	}
}

func writeInput(data []byte) {
	sessionMu.Lock()
	s := session
	sessionMu.Unlock()
	if s == nil {
		return
	}
	item := pendingInput{session: s, data: append([]byte(nil), data...)}
	select {
	case inputQueue <- item:
	default:
		setStatus("SSH input queue full")
	}
}

// inputWriter serializes pipe writes away from the Win32 message loop.
func inputWriter() {
	for item := range inputQueue {
		inputWriteMu.Lock()
		sessionMu.Lock()
		if session != item.session || item.session.Input == nil {
			sessionMu.Unlock()
			inputWriteMu.Unlock()
			continue
		}
		input := item.session.Input
		sessionMu.Unlock()
		data := item.data
		for len(data) > 0 {
			n, err := input.Write(data)
			if err != nil {
				setStatus("SSH input failed: " + err.Error())
				break
			}
			if n == 0 {
				setStatus("SSH input pipe closed")
				break
			}
			data = data[n:]
		}
		inputWriteMu.Unlock()
	}
}

func disconnectSSH(status string) {
	sessionMu.Lock()
	s := session
	session = nil
	sessionMu.Unlock()
	if s != nil {
		go func() { inputWriteMu.Lock(); _ = s.Close(); inputWriteMu.Unlock() }()
	}
	setStatus(status)
}

func keySequence(key uintptr) []byte {
	switch key {
	case 0x25:
		return []byte("\x1b[D") // Left
	case 0x26:
		return []byte("\x1b[A") // Up
	case 0x27:
		return []byte("\x1b[C") // Right
	case 0x28:
		return []byte("\x1b[B") // Down
	case 0x24:
		return []byte("\x1b[H") // Home
	case 0x23:
		return []byte("\x1b[F") // End
	case 0x2d:
		return []byte("\x1b[2~") // Insert
	case 0x2e:
		return []byte("\x1b[3~") // Delete
	case 0x21:
		return []byte("\x1b[5~") // PageUp
	case 0x22:
		return []byte("\x1b[6~") // PageDown
	case 0x1b:
		return []byte{0x1b}
	}
	return nil
}

func resizeTerminal(hwnd uintptr) {
	var rc rect
	winapi.GetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&rc)))
	width := int(rc.Right-rc.Left-24) / cellWidth
	height := int(rc.Bottom-rc.Top-toolbarHeight-12) / cellHeight
	if width < 20 {
		width = 20
	}
	if height < 5 {
		height = 5
	}
	screenMu.Lock()
	if width != screen.Width || height != screen.Height {
		screen.Resize(width, height)
	}
	cols, rows := screen.Width, screen.Height
	screenMu.Unlock()
	sessionMu.Lock()
	if session != nil {
		if err := session.Resize(cols, rows); err != nil {
			setStatus("Resize failed: " + err.Error())
		}
	}
	sessionMu.Unlock()
}

func paint(hwnd uintptr) {
	var ps paintStruct
	hdc, _, _ := winapi.BeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	var rc rect
	winapi.GetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&rc)))
	brush, _, _ := winapi.CreateSolidBrush.Call(0x001E1E1E)
	winapi.FillRect.Call(hdc, uintptr(unsafe.Pointer(&rc)), brush)
	winapi.DeleteObject.Call(brush)
	winapi.SetBkMode.Call(hdc, 1)
	screenMu.Lock()
	for y := 0; y < screen.Height; y++ {
		for x := 0; x < screen.Width; {
			cell := screen.Cells[y][x]
			end := x + 1
			for end < screen.Width && screen.Cells[y][end].FG == cell.FG && screen.Cells[y][end].BG == cell.BG {
				end++
			}
			left := int32(12 + x*cellWidth)
			top := int32(toolbarHeight + 6 + y*cellHeight)
			background := rect{Left: left, Top: top, Right: left + int32((end-x)*cellWidth), Bottom: top + cellHeight}
			cellBrush, _, _ := winapi.CreateSolidBrush.Call(uintptr(colorRef(cell.BG)))
			winapi.FillRect.Call(hdc, uintptr(unsafe.Pointer(&background)), cellBrush)
			winapi.DeleteObject.Call(cellBrush)
			line := make([]uint16, end-x)
			for i := x; i < end; i++ {
				r := screen.Cells[y][i].Rune
				if r == 0 {
					r = ' '
				}
				if r > 0xFFFF {
					r = '?'
				}
				line[i-x] = uint16(r)
			}
			winapi.SetTextColor.Call(hdc, uintptr(colorRef(cell.FG)))
			winapi.TextOutW.Call(hdc, uintptr(left), uintptr(top), uintptr(unsafe.Pointer(&line[0])), uintptr(len(line)))
			x = end
		}
	}
	screenMu.Unlock()
	winapi.EndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
}

// colorRef converts an ANSI palette index or tagged 24-bit RGB value to GDI COLORREF.
func colorRef(color uint32) uint32 {
	if color&0xFF000000 != 0 {
		rgb := color & 0xFFFFFF
		return (rgb&0xFF)<<16 | (rgb & 0xFF00) | (rgb>>16)&0xFF
	}
	base := [...]uint32{0x000000, 0xCD0000, 0x00CD00, 0xCDCD00, 0x0000EE, 0xCD00CD, 0x00CDCD, 0xE5E5E5, 0x7F7F7F, 0xFF0000, 0x00FF00, 0xFFFF00, 0x5C5CFF, 0xFF00FF, 0x00FFFF, 0xFFFFFF}
	var rgb uint32
	if color < 16 {
		rgb = base[color]
	} else if color < 232 {
		n := color - 16
		levels := [...]uint32{0, 95, 135, 175, 215, 255}
		rgb = levels[n/36]<<16 | levels[(n/6)%6]<<8 | levels[n%6]
	} else if color < 256 {
		v := uint32(8 + (color-232)*10)
		rgb = v<<16 | v<<8 | v
	}
	return (rgb&0xFF)<<16 | (rgb & 0xFF00) | (rgb>>16)&0xFF
}

func main() {
	go inputWriter()
	instance, _, _ := winapi.Kernel32.NewProc("GetModuleHandleW").Call(0)
	wc := wndClassEx{Size: uint32(unsafe.Sizeof(wndClassEx{})), Style: csHRedraw | csVRedraw, WndProc: syscall.NewCallback(windowProc), Instance: instance, Background: colorWindow, ClassName: className}
	if r, _, e := winapi.RegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		panic(e)
	}
	hwnd, _, e := winapi.CreateWindowExW.Call(0, uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(windowTitle)), wsOverlappedWindow|wsVisible, 100, 100, 1100, 720, 0, 0, instance, 0)
	if hwnd == 0 {
		panic(e)
	}
	winapi.User32.NewProc("ShowWindow").Call(hwnd, swShow)
	winapi.User32.NewProc("UpdateWindow").Call(hwnd)
	var m msg
	for {
		r, _, _ := winapi.GetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		winapi.TranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		winapi.DispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}
