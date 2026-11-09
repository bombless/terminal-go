//go:build windows

package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf16"
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
	wsClipChildren     = 0x02000000
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
	wmKillFocus        = 0x0008
	wmChar             = 0x0102
	wmKeyDown          = 0x0100
	wmCommand          = 0x0111
	wmTimer            = 0x0113
	wmPaint            = 0x000F
	wmSetIcon          = 0x0080
	wmCtlColorStatic   = 0x0138
	wmMouseMove        = 0x0200
	wmLButtonDown      = 0x0201
	wmLButtonUp        = 0x0202
	wmRButtonUp        = 0x0205
	mkLButton          = 0x0001
	vkControl          = 0x11
	cfUnicodeText      = 13
	globalMoveable     = 0x0002
	colorWindow        = 5
	swShow             = 5
	imageIcon          = 1
	lrDefaultSize      = 0x0040
	lrShared           = 0x8000
	smCxSmIcon         = 49
	smCySmIcon         = 50
	iconSmall          = 0
	iconBig            = 1
	iconResourceID     = 1
	mbIconError        = 0x0010

	idHost           = 1001
	idConnect        = 1002
	idDisconnect     = 1003
	idStatus         = 1004
	idPowerShell     = 1005
	timerIDHeartbeat = 1
	timerIDCursor    = 2
	enChange         = 0x0300
	gcsCompStr       = 0x0008

	cellWidth     = 10
	cellHeight    = 20
	toolbarHeight = 48
	terminalBG    = 0x001E1E1E // COLORREF for the terminal background.
	cursorBorder  = 0x00A0A0A0 // COLORREF for the cursor outline.
	srcCopy       = 0x00CC0020
	selectionBG   = 0x01A56E2B // Tagged RGB: #A56E2B.
	selectionFG   = 0x01FFFFFF // Tagged RGB: #FFFFFF.
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

type textSelection struct {
	start, end int
	active     bool
	dragging   bool
}

var (
	className                                                   = syscall.StringToUTF16Ptr("TerminalGoWindow")
	windowTitle                                                 = syscall.StringToUTF16Ptr("terminal-go - SSH / PowerShell Terminal")
	mainHwnd                                                    uintptr
	appIcon, appIconSmall                                       uintptr
	hostEdit, connectButton, disconnectButton, powerShellButton uintptr
	statusLabel                                                 uintptr
	screenMu                                                    sync.Mutex
	screen                                                      = term.NewScreen(96, 30)
	parser                                                      = term.NewParser(screen)
	sessionMu                                                   sync.Mutex
	inputWriteMu                                                sync.Mutex
	session                                                     *sshclient.Session
	inputQueue                                                  = make(chan pendingInput, 256)
	selection                                                   textSelection
	cursorBlinkOn                                                      = true
	cursorBlinkInterval                                         uint32 = 530
	lastCursorRc                                                rect
	telemetry                                                   *log.Logger
	telemetryFile                                               *os.File
	paintFrames                                                 uint64
	lastSlowPaint                                               time.Time
	lastHeartbeat                                               time.Time
	terminalFont                                                uintptr
	cjkFont                                                     uintptr
	toolbarBrush                                                uintptr
	backDC                                                      uintptr
	backBitmap                                                  uintptr
	backStockBitmap                                             uintptr
	backW, backH                                                int32
	brushCache                                                  = make(map[uint32]uintptr)
)

func initTelemetry() {
	dir, err := os.UserConfigDir()
	if err == nil {
		dir = filepath.Join(dir, "terminal-go")
		err = os.MkdirAll(dir, 0700)
	}
	if err != nil {
		dir = os.TempDir()
	}
	path := filepath.Join(dir, "telemetry.log")
	f, openErr := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if openErr != nil {
		return
	}
	telemetryFile = f
	telemetry = log.New(f, "", log.LstdFlags|log.Lmicroseconds)
	telemetry.Printf("telemetry_file=%s", path)
	telemetry.Printf("startup version=dev screen=%dx%d; terminal contents, clipboard text, and host names are not logged", screen.Width, screen.Height)
}

func telemetryf(format string, args ...any) {
	if telemetry != nil {
		telemetry.Printf(format, args...)
	}
}

func cursorCellRect(curX, curY int) rect {
	cLeft := int32(12 + curX*cellWidth)
	cTop := int32(toolbarHeight + 6 + curY*cellHeight)
	return rect{
		Left:   cLeft,
		Top:    cTop,
		Right:  cLeft + cellWidth,
		Bottom: cTop + cellHeight,
	}
}

func invalidateCursor(hwnd uintptr) {
	screenMu.Lock()
	curX, curY := screen.X, screen.Y
	screenMu.Unlock()
	if curX >= screen.Width {
		curX = screen.Width - 1
	}
	if curY >= screen.Height {
		curY = screen.Height - 1
	}
	if curX >= 0 && curY >= 0 {
		rc := cursorCellRect(curX, curY)
		if lastCursorRc != rc && lastCursorRc.Right > lastCursorRc.Left {
			winapi.User32.NewProc("InvalidateRect").Call(hwnd, uintptr(unsafe.Pointer(&lastCursorRc)), 0)
		}
		lastCursorRc = rc
		winapi.User32.NewProc("InvalidateRect").Call(hwnd, uintptr(unsafe.Pointer(&rc)), 0)
	} else if lastCursorRc.Right > lastCursorRc.Left {
		winapi.User32.NewProc("InvalidateRect").Call(hwnd, uintptr(unsafe.Pointer(&lastCursorRc)), 0)
		lastCursorRc = rect{}
	}
}

func resetCursorBlink(hwnd uintptr) {
	cursorBlinkOn = true
	if cursorBlinkInterval > 0 {
		winapi.User32.NewProc("SetTimer").Call(hwnd, timerIDCursor, uintptr(cursorBlinkInterval), 0)
	}
	invalidateCursor(hwnd)
}

func windowProc(hwnd uintptr, message uint32, wParam, lParam uintptr) uintptr {
	switch message {
	case wmCreate:
		telemetryf("window_message wm_create_begin")
		if appIcon != 0 {
			winapi.User32.NewProc("SendMessageW").Call(hwnd, wmSetIcon, iconBig, appIcon)
		}
		if appIconSmall != 0 {
			winapi.User32.NewProc("SendMessageW").Call(hwnd, wmSetIcon, iconSmall, appIconSmall)
		}
		fontName := syscall.StringToUTF16Ptr("Consolas")
		terminalFont, _, _ = winapi.CreateFontW.Call(^uintptr(15), 0, 0, 0, 400, 0, 0, 0, 1, 0, 0, 5, 0x31, uintptr(unsafe.Pointer(fontName)))
		cjkFontName := syscall.StringToUTF16Ptr("Microsoft YaHei")
		cjkFont, _, _ = winapi.CreateFontW.Call(^uintptr(15), 0, 0, 0, 400, 0, 0, 0, 1, 0, 0, 5, 0, uintptr(unsafe.Pointer(cjkFontName)))
		toolbarBrush, _, _ = winapi.CreateSolidBrush.Call(0x001E1E1E)
		createToolbar(hwnd)
		winapi.User32.NewProc("SetTimer").Call(hwnd, timerIDHeartbeat, 1000, 0)
		blinkTime, _, _ := winapi.User32.NewProc("GetCaretBlinkTime").Call()
		if blinkTime == 0 || blinkTime > 2000 {
			blinkTime = 530
		}
		cursorBlinkInterval = uint32(blinkTime)
		winapi.User32.NewProc("SetTimer").Call(hwnd, timerIDCursor, uintptr(cursorBlinkInterval), 0)
		lastHeartbeat = time.Now()
		telemetryf("window_message wm_create_end host=%d connect=%d disconnect=%d powershell=%d status=%d", hostEdit, connectButton, disconnectButton, powerShellButton, statusLabel)
		return 0
	case wmSetFocus:
		resetCursorBlink(hwnd)
		return 0
	case wmKillFocus:
		cursorBlinkOn = false
		invalidateCursor(hwnd)
		return 0
	case wmSize:
		telemetryf("window_message wm_size_begin width=%d height=%d", uint16(lParam), uint16(lParam>>16))
		layoutToolbar(hwnd, int32(uint16(lParam)), int32(uint16(lParam>>16)))
		resizeTerminal(hwnd)
		telemetryf("window_message wm_size_end")
		return 0
	case wmLButtonDown:
		py := int32(int16(uint16(lParam >> 16)))
		if py < toolbarHeight {
			return 0
		}
		winapi.User32.NewProc("SetFocus").Call(hwnd)
		resetCursorBlink(hwnd)
		x, y := mouseCell(lParam)
		selection = textSelection{start: y*screen.Width + x, end: y*screen.Width + x, active: true, dragging: true}
		winapi.User32.NewProc("SetCapture").Call(hwnd)
		winapi.User32.NewProc("InvalidateRect").Call(hwnd, 0, 0)
		return 0
	case wmMouseMove:
		if selection.dragging && wParam&mkLButton != 0 {
			x, y := mouseCell(lParam)
			selection.end = y*screen.Width + x
			winapi.User32.NewProc("InvalidateRect").Call(hwnd, 0, 0)
		}
		return 0
	case wmRButtonUp:
		showClipboardMenu(hwnd, lParam)
		return 0
	case wmLButtonUp:
		if selection.dragging {
			x, y := mouseCell(lParam)
			selection.end = y*screen.Width + x
			selection.dragging = false
			winapi.User32.NewProc("ReleaseCapture").Call()
			winapi.User32.NewProc("InvalidateRect").Call(hwnd, 0, 0)
		}
		return 0
	case wmCommand:
		controlID := uint16(wParam)
		notifyCode := uint16(wParam >> 16)
		if controlID == idHost {
			if notifyCode == enChange {
				telemetryf("host_edit_change")
			}
			return 0
		}
		switch controlID {
		case idConnect:
			if notifyCode == 0 { // BN_CLICKED
				connectSSH(hwnd)
				return 0
			}
		case idPowerShell:
			if notifyCode == 0 { // BN_CLICKED
				startPowerShell(hwnd)
				return 0
			}
		case idDisconnect:
			if notifyCode == 0 { // BN_CLICKED
				disconnectSession("Disconnected")
				return 0
			}
		}
		r, _, _ := winapi.DefWindowProcW.Call(hwnd, uintptr(message), wParam, lParam)
		return r
	case wmChar:
		resetCursorBlink(hwnd)
		if wParam == 3 {
			if selection.active && selection.start != selection.end {
				copySelection()
			} else {
				writeInput([]byte{3}) // Preserve remote Ctrl+C when no text is selected.
			}
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
		resetCursorBlink(hwnd)
		if keyDown(vkControl) {
			switch wParam {
			case 'C':
				if selection.active && selection.start != selection.end {
					copySelection()
					return 0
				}
			case 'V':
				pasteClipboard()
				return 0
			case 'A':
				selectAll(hwnd)
				return 0
			}
		}
		if seq := keySequence(wParam); len(seq) != 0 {
			writeInput(seq)
			return 0
		}
	case wmPaint:
		telemetryf("window_message wm_paint_begin")
		paint(hwnd)
		telemetryf("window_message wm_paint_end")
		return 0
	case wmCtlColorStatic:
		hdc := wParam
		winapi.SetTextColor.Call(hdc, 0x00D0D0D0)
		winapi.SetBkColor.Call(hdc, 0x001E1E1E)
		return toolbarBrush
	case wmTimer:
		switch wParam {
		case timerIDHeartbeat:
			if time.Since(lastHeartbeat) >= 5*time.Second {
				hostLength, _, _ := winapi.User32.NewProc("GetWindowTextLengthW").Call(hostEdit)
				telemetryf("ui_heartbeat message_loop_alive host_length=%d input_queue=%d", hostLength, len(inputQueue))
				lastHeartbeat = time.Now()
			}
			return 0
		case timerIDCursor:
			focused, _, _ := winapi.User32.NewProc("GetFocus").Call()
			if focused == hwnd {
				cursorBlinkOn = !cursorBlinkOn
				invalidateCursor(hwnd)
			}
			return 0
		}
		return 0
	case wmDestroy:
		disconnectSession("Window closed")
		winapi.User32.NewProc("KillTimer").Call(hwnd, timerIDHeartbeat)
		winapi.User32.NewProc("KillTimer").Call(hwnd, timerIDCursor)
		if terminalFont != 0 {
			winapi.DeleteObject.Call(terminalFont)
			terminalFont = 0
		}
		if cjkFont != 0 {
			winapi.DeleteObject.Call(cjkFont)
			cjkFont = 0
		}
		if toolbarBrush != 0 {
			winapi.DeleteObject.Call(toolbarBrush)
			toolbarBrush = 0
		}
		if backDC != 0 {
			if backStockBitmap != 0 {
				winapi.SelectObject.Call(backDC, backStockBitmap)
				backStockBitmap = 0
			}
			if backBitmap != 0 {
				winapi.DeleteObject.Call(backBitmap)
				backBitmap = 0
			}
			winapi.DeleteDC.Call(backDC)
			backDC = 0
		}
		for color, brush := range brushCache {
			winapi.DeleteObject.Call(brush)
			delete(brushCache, color)
		}
		winapi.PostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := winapi.DefWindowProcW.Call(hwnd, uintptr(message), wParam, lParam)
	return r
}

func keyDown(key int) bool {
	r, _, _ := winapi.User32.NewProc("GetKeyState").Call(uintptr(key))
	return uint16(r)&0x8000 != 0
}

func selectAll(hwnd uintptr) {
	screenMu.Lock()
	selection = textSelection{start: 0, end: screen.Width*screen.Height - 1, active: true}
	screenMu.Unlock()
	winapi.User32.NewProc("InvalidateRect").Call(hwnd, 0, 0)
}

func selectedText() string {
	screenMu.Lock()
	defer screenMu.Unlock()
	if !selection.active || selection.start == selection.end {
		return ""
	}
	first, last := selection.start, selection.end
	if first > last {
		first, last = last, first
	}
	var out strings.Builder
	for pos := first; pos <= last; pos++ {
		x, y := pos%screen.Width, pos/screen.Width
		if y >= screen.Height {
			break
		}
		r := screen.Cells[y][x].Rune
		if r == 0 {
			if x > 0 && term.RuneWidth(screen.Cells[y][x-1].Rune) == 2 {
				if x == screen.Width-1 && pos < last {
					out.WriteByte('\n')
				}
				continue // second half of a wide glyph
			}
			r = ' '
		}
		out.WriteRune(r)
		if x == screen.Width-1 && pos < last {
			out.WriteByte('\n')
		}
	}
	return strings.TrimRight(out.String(), " \n")
}

func copySelection() {
	value := selectedText()
	if value == "" {
		return
	}
	text16, err := syscall.UTF16FromString(value)
	if err != nil {
		return
	}
	kernel32 := winapi.Kernel32
	h, _, _ := kernel32.NewProc("GlobalAlloc").Call(globalMoveable, uintptr(len(text16)*2))
	if h == 0 {
		return
	}
	p, _, _ := kernel32.NewProc("GlobalLock").Call(h)
	if p == 0 {
		kernel32.NewProc("GlobalFree").Call(h)
		return
	}
	kernel32.NewProc("RtlMoveMemory").Call(p, uintptr(unsafe.Pointer(&text16[0])), uintptr(len(text16)*2))
	kernel32.NewProc("GlobalUnlock").Call(h)
	opened, _, _ := winapi.User32.NewProc("OpenClipboard").Call(0)
	if opened == 0 {
		kernel32.NewProc("GlobalFree").Call(h)
		return
	}
	winapi.User32.NewProc("EmptyClipboard").Call()
	result, _, _ := winapi.User32.NewProc("SetClipboardData").Call(cfUnicodeText, h)
	winapi.User32.NewProc("CloseClipboard").Call()
	if result == 0 {
		kernel32.NewProc("GlobalFree").Call(h)
	}
}

func pasteClipboard() {
	opened, _, _ := winapi.User32.NewProc("OpenClipboard").Call(0)
	if opened == 0 {
		return
	}
	h, _, _ := winapi.User32.NewProc("GetClipboardData").Call(cfUnicodeText)
	if h == 0 {
		winapi.User32.NewProc("CloseClipboard").Call()
		return
	}
	p, _, _ := winapi.Kernel32.NewProc("GlobalLock").Call(h)
	if p == 0 {
		winapi.User32.NewProc("CloseClipboard").Call()
		return
	}
	n, _, _ := winapi.Kernel32.NewProc("lstrlenW").Call(p)
	var value string
	if n > 0 {
		units := make([]uint16, n)
		winapi.Kernel32.NewProc("RtlMoveMemory").Call(uintptr(unsafe.Pointer(&units[0])), p, n*2)
		value = string(utf16.Decode(units))
	}
	winapi.Kernel32.NewProc("GlobalUnlock").Call(h)
	winapi.User32.NewProc("CloseClipboard").Call()
	value = strings.ReplaceAll(strings.ReplaceAll(value, "\r\n", "\n"), "\n", "\r")
	if value != "" {
		writeInput([]byte(value))
	}
}

func showClipboardMenu(hwnd, lParam uintptr) {
	menu, _, _ := winapi.User32.NewProc("CreatePopupMenu").Call()
	if menu == 0 {
		return
	}
	defer winapi.User32.NewProc("DestroyMenu").Call(menu)
	appendItem := winapi.User32.NewProc("AppendMenuW")
	copyText, pasteText, allText := syscall.StringToUTF16Ptr("Copy\tCtrl+C"), syscall.StringToUTF16Ptr("Paste\tCtrl+V"), syscall.StringToUTF16Ptr("Select all\tCtrl+A")
	appendItem.Call(menu, 0, 1, uintptr(unsafe.Pointer(copyText)))
	appendItem.Call(menu, 0, 2, uintptr(unsafe.Pointer(pasteText)))
	appendItem.Call(menu, 0, 3, uintptr(unsafe.Pointer(allText)))
	pt := point{X: int32(int16(uint16(lParam))), Y: int32(int16(uint16(lParam >> 16)))}
	winapi.User32.NewProc("ClientToScreen").Call(hwnd, uintptr(unsafe.Pointer(&pt)))
	cmd, _, _ := winapi.User32.NewProc("TrackPopupMenu").Call(menu, 0x0100, uintptr(pt.X), uintptr(pt.Y), 0, hwnd, 0)
	switch cmd {
	case 1:
		copySelection()
	case 2:
		pasteClipboard()
	case 3:
		selectAll(hwnd)
	}
}

func mouseCell(lParam uintptr) (int, int) {
	x := int(int16(uint16(lParam)))
	y := int(int16(uint16(lParam >> 16)))
	x = (x - 12) / cellWidth
	y = (y - toolbarHeight - 6) / cellHeight
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	if x >= screen.Width {
		x = screen.Width - 1
	}
	if y >= screen.Height {
		y = screen.Height - 1
	}
	return x, y
}

func createToolbar(hwnd uintptr) {
	create := winapi.CreateWindowExW
	hostClass := syscall.StringToUTF16Ptr("EDIT")
	buttonClass := syscall.StringToUTF16Ptr("BUTTON")
	staticClass := syscall.StringToUTF16Ptr("STATIC")
	hostText := syscall.StringToUTF16Ptr("")
	connectText := syscall.StringToUTF16Ptr("Connect")
	disconnectText := syscall.StringToUTF16Ptr("Disconnect")
	powerShellText := syscall.StringToUTF16Ptr("PowerShell")
	statusText := syscall.StringToUTF16Ptr("Not connected")
	hostEdit, _, _ = create.Call(0, uintptr(unsafe.Pointer(hostClass)), uintptr(unsafe.Pointer(hostText)), wsChild|wsVisible|wsBorder|wsTabStop|esAutoHScroll, 12, 10, 300, 26, hwnd, idHost, 0, 0)
	connectButton, _, _ = create.Call(0, uintptr(unsafe.Pointer(buttonClass)), uintptr(unsafe.Pointer(connectText)), wsChild|wsVisible|wsTabStop|bsPushButton, 322, 10, 88, 26, hwnd, idConnect, 0, 0)
	disconnectButton, _, _ = create.Call(0, uintptr(unsafe.Pointer(buttonClass)), uintptr(unsafe.Pointer(disconnectText)), wsChild|wsVisible|wsTabStop|bsPushButton, 418, 10, 96, 26, hwnd, idDisconnect, 0, 0)
	powerShellButton, _, _ = create.Call(0, uintptr(unsafe.Pointer(buttonClass)), uintptr(unsafe.Pointer(powerShellText)), wsChild|wsVisible|wsTabStop|bsPushButton, 522, 10, 96, 26, hwnd, idPowerShell, 0, 0)
	statusLabel, _, _ = create.Call(0, uintptr(unsafe.Pointer(staticClass)), uintptr(unsafe.Pointer(statusText)), wsChild|wsVisible|ssLeft, 626, 14, 330, 22, hwnd, idStatus, 0, 0)
}

func layoutToolbar(hwnd uintptr, width, height int32) {
	if width < 1 {
		return
	}
	move := winapi.User32.NewProc("MoveWindow")
	statusWidth := width - 638
	if statusWidth < 80 {
		statusWidth = 80
	}
	move.Call(hostEdit, 12, 10, 300, 26, 1)
	move.Call(connectButton, 322, 10, 88, 26, 1)
	move.Call(disconnectButton, 418, 10, 96, 26, 1)
	move.Call(powerShellButton, 522, 10, 96, 26, 1)
	move.Call(statusLabel, 626, 14, uintptr(statusWidth), 22, 1)
	_ = hwnd
	_ = height
}

func setStatus(value string) {
	if statusLabel != 0 {
		p, err := syscall.UTF16PtrFromString(value)
		if err == nil {
			winapi.User32.NewProc("SetWindowTextW").Call(statusLabel, uintptr(unsafe.Pointer(p)))
			winapi.User32.NewProc("InvalidateRect").Call(statusLabel, 0, 1)
			winapi.User32.NewProc("UpdateWindow").Call(statusLabel)
		}
	}
}

func hasSession() bool {
	sessionMu.Lock()
	defer sessionMu.Unlock()
	return session != nil
}

func connectSSH(hwnd uintptr) {
	if hasSession() {
		setStatus("Already connected")
		return
	}
	length, _, _ := winapi.User32.NewProc("GetWindowTextLengthW").Call(hostEdit)
	buffer := make([]uint16, int(length)+1)
	winapi.User32.NewProc("GetWindowTextW").Call(hostEdit, uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)))
	host := strings.TrimSpace(syscall.UTF16ToString(buffer))
	if host == "" {
		setStatus("Enter user@host or host")
		return
	}
	resolveStarted := time.Now()
	executable, err := sshclient.ResolveExecutable("")
	telemetryf("ssh_resolve duration=%s err=%v", time.Since(resolveStarted), err)
	if err != nil {
		setStatus("SSH unavailable: " + err.Error())
		return
	}
	var sshArgs []string
	cleanHost := host
	if lastColon := strings.LastIndex(host, ":"); lastColon != -1 && !strings.Contains(host[lastColon:], "]") {
		portPart := host[lastColon+1:]
		if portNum, err := strconv.Atoi(portPart); err == nil && portNum > 0 && portNum <= 65535 {
			sshArgs = append(sshArgs, "-p", portPart)
			cleanHost = host[:lastColon]
		}
	}
	sshArgs = append(sshArgs, cleanHost)
	startSession(hwnd, "SSH", "Connecting to "+host+"...", "Connected: "+host, executable, sshArgs)
}

func startPowerShell(hwnd uintptr) {
	if hasSession() {
		setStatus("Already connected")
		return
	}
	executable, err := sshclient.ResolvePowerShell("")
	if err != nil {
		setStatus("PowerShell unavailable: " + err.Error())
		return
	}
	startSession(hwnd, "PowerShell", "Starting PowerShell...", "Connected: PowerShell", executable, nil)
}

// startSession launches one supported child process in ConPTY and installs it
// as the active session unless another connect already won the race.
func startSession(hwnd uintptr, label, pendingStatus, connectedStatus, executable string, args []string) {
	started := time.Now()
	telemetryf("session_start label=%s executable=%s", label, filepath.Base(executable))
	setStatus(pendingStatus)
	spawnStarted := time.Now()
	s, err := sshclient.Start(executable, args, screen.Width, screen.Height)
	telemetryf("session_spawn label=%s duration=%s err=%v total=%s", label, time.Since(spawnStarted), err, time.Since(started))
	if err != nil {
		setStatus(label + " start failed: " + err.Error())
		return
	}
	sessionMu.Lock()
	if session != nil {
		sessionMu.Unlock()
		_ = s.Close()
		return
	}
	session = s
	sessionMu.Unlock()
	setStatus(connectedStatus)
	winapi.User32.NewProc("SetFocus").Call(hwnd)
	resetCursorBlink(hwnd)
	go readSessionOutput(s, hwnd, label)
}

func readSessionOutput(s *sshclient.Session, hwnd uintptr, label string) {
	buf := make([]byte, 8192)
	windowStart := time.Now()
	var windowBytes, windowChunks int64
	var parseTotal, parseMax time.Duration
	for {
		n, err := s.Output.Read(buf)
		if n > 0 {
			parseStart := time.Now()
			screenMu.Lock()
			parser.Feed(buf[:n])
			screenMu.Unlock()
			cursorBlinkOn = true
			parseElapsed := time.Since(parseStart)
			windowBytes += int64(n)
			windowChunks++
			parseTotal += parseElapsed
			if parseElapsed > parseMax {
				parseMax = parseElapsed
			}
			winapi.User32.NewProc("InvalidateRect").Call(hwnd, 0, 0)
			if time.Since(windowStart) >= 5*time.Second {
				telemetryf("output bytes=%d chunks=%d parse_total=%s parse_max=%s input_queue=%d", windowBytes, windowChunks, parseTotal, parseMax, len(inputQueue))
				windowStart = time.Now()
				windowBytes, windowChunks = 0, 0
				parseTotal, parseMax = 0, 0
			}
		}
		if err != nil {
			telemetryf("output_reader_end label=%s err=%v pending_bytes=%d pending_chunks=%d", label, err, windowBytes, windowChunks)
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
		telemetryf("session_exit label=%s wait_error=%v", label, waitErr)
		setStatus(label + " ended: " + waitErr.Error())
	} else {
		telemetryf("session_exit label=%s code=%d", label, code)
		setStatus(fmt.Sprintf("%s exited (%d)", label, code))
	}
	winapi.User32.NewProc("InvalidateRect").Call(hwnd, 0, 0)
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
		telemetryf("input_queue_full queued=%d capacity=%d dropped_bytes=%d", len(inputQueue), cap(inputQueue), len(data))
		setStatus("Session input queue full")
	}
}

// inputWriter serializes pipe writes away from the Win32 message loop.
func inputWriter() {
	for item := range inputQueue {
		itemStarted := time.Now()
		inputWriteMu.Lock()
		lockWait := time.Since(itemStarted)
		sessionMu.Lock()
		if session != item.session || item.session.Input == nil {
			sessionMu.Unlock()
			inputWriteMu.Unlock()
			continue
		}
		input := item.session.Input
		sessionMu.Unlock()
		data := item.data
		writeStarted := time.Now()
		originalBytes := len(data)
		for len(data) > 0 {
			n, err := input.Write(data)
			if err != nil {
				setStatus("Session input failed: " + err.Error())
				break
			}
			if n == 0 {
				setStatus("Session input pipe closed")
				break
			}
			data = data[n:]
		}
		writeElapsed := time.Since(writeStarted)
		if lockWait > 20*time.Millisecond || writeElapsed > 100*time.Millisecond {
			telemetryf("input_write bytes=%d lock_wait=%s write_time=%s queue_remaining=%d", originalBytes, lockWait, writeElapsed, len(inputQueue))
		}
		inputWriteMu.Unlock()
	}
}

func disconnectSession(status string) {
	sessionMu.Lock()
	s := session
	session = nil
	sessionMu.Unlock()
	if s != nil {
		go func() { inputWriteMu.Lock(); _ = s.Close(); inputWriteMu.Unlock() }()
	}
	if mainHwnd != 0 {
		winapi.User32.NewProc("InvalidateRect").Call(mainHwnd, 0, 0)
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

// ensureBackBuffer returns a memory DC with a bitmap matching the window client
// area. fresh reports that the bitmap was (re)created, so the caller must render
// the whole terminal area rather than only the invalid rect.
func ensureBackBuffer(windowDC uintptr, width, height int32) (uintptr, bool) {
	if width <= 0 || height <= 0 {
		return 0, false
	}
	if backDC == 0 {
		dc, _, _ := winapi.CreateCompatibleDC.Call(windowDC)
		if dc == 0 {
			return 0, false
		}
		backDC = dc
	}
	fresh := false
	if backBitmap == 0 || backW != width || backH != height {
		bitmap, _, _ := winapi.CreateCompatibleBitmap.Call(windowDC, uintptr(width), uintptr(height))
		if bitmap == 0 {
			return 0, false
		}
		stock, _, _ := winapi.SelectObject.Call(backDC, bitmap)
		if backBitmap != 0 {
			winapi.DeleteObject.Call(backBitmap)
		} else {
			backStockBitmap = stock
		}
		backBitmap = bitmap
		backW, backH = width, height
		fresh = true
	}
	return backDC, fresh
}

// brushRef returns a cached solid brush for a GDI COLORREF so streaming
// repaints do not create and destroy hundreds of brushes per frame.
func brushRef(color uint32) uintptr {
	if brush, ok := brushCache[color]; ok {
		return brush
	}
	brush, _, _ := winapi.CreateSolidBrush.Call(uintptr(color))
	if brush == 0 {
		return 0
	}
	if len(brushCache) >= 1024 {
		for key, cached := range brushCache {
			winapi.DeleteObject.Call(cached)
			delete(brushCache, key)
		}
	}
	brushCache[color] = brush
	return brush
}

func paint(hwnd uintptr) {
	paintStarted := time.Now()
	var runs, cells int
	var ps paintStruct
	windowDC, _, _ := winapi.BeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	if windowDC == 0 {
		return
	}
	var rc rect
	winapi.GetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&rc)))
	hdc, fresh := ensureBackBuffer(windowDC, rc.Right, rc.Bottom)
	if hdc == 0 {
		winapi.EndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
		return
	}
	clip := ps.Paint
	if clip.Left < 0 {
		clip.Left = 0
	}
	if clip.Top < toolbarHeight {
		clip.Top = toolbarHeight
	}
	if clip.Right > rc.Right {
		clip.Right = rc.Right
	}
	if clip.Bottom > rc.Bottom {
		clip.Bottom = rc.Bottom
	}
	if fresh {
		clip = rect{Left: 0, Top: toolbarHeight, Right: rc.Right, Bottom: rc.Bottom}
	}
	if clip.Right <= clip.Left || clip.Bottom <= clip.Top {
		winapi.EndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
		return
	}
	// Render off-screen and blit once: the window must never show a frame that
	// has only been cleared while streamed output is loading.
	winapi.IntersectClipRect.Call(hdc, uintptr(clip.Left), uintptr(clip.Top), uintptr(clip.Right), uintptr(clip.Bottom))
	brush := brushRef(terminalBG)
	termRect := rect{Left: 0, Top: toolbarHeight, Right: rc.Right, Bottom: rc.Bottom}
	winapi.FillRect.Call(hdc, uintptr(unsafe.Pointer(&termRect)), brush)
	winapi.SetBkMode.Call(hdc, 1)
	var previousFont uintptr
	if terminalFont != 0 {
		previousFont, _, _ = winapi.SelectObject.Call(hdc, terminalFont)
	}
	screenMu.Lock()
	for y := 0; y < screen.Height; y++ {
		for x := 0; x < screen.Width; {
			cell := screen.Cells[y][x]
			selected := selection.active && isSelected(y*screen.Width+x)
			if selected {
				cell.BG, cell.FG, cell.Faint = selectionBG, selectionFG, false
			}
			end := x + 1
			for end < screen.Width {
				next := screen.Cells[y][end]
				if selection.active && isSelected(y*screen.Width+end) {
					next.BG, next.FG, next.Faint = selectionBG, selectionFG, false
				}
				if next.FG != cell.FG || next.BG != cell.BG || next.Faint != cell.Faint {
					break
				}
				end++
			}
			left := int32(12 + x*cellWidth)
			top := int32(toolbarHeight + 6 + y*cellHeight)
			background := rect{Left: left, Top: top, Right: left + int32((end-x)*cellWidth), Bottom: top + cellHeight}
			cellBrush := brushRef(colorRef(cell.BG))
			winapi.FillRect.Call(hdc, uintptr(unsafe.Pointer(&background)), cellBrush)
			runs++
			cells += end - x
			line := make([]uint16, 0, end-x)
			dx := make([]int32, 0, end-x)
			for i := x; i < end; i++ {
				r := screen.Cells[y][i].Rune
				if r == 0 {
					if i > 0 && term.RuneWidth(screen.Cells[y][i-1].Rune) == 2 {
						continue // continuation cell of the preceding wide glyph
					}
					r = ' '
				}
				if r > 0xFFFF {
					r = '?'
				}
				line = append(line, uint16(r))
				dx = append(dx, int32(cellWidth*term.RuneWidth(r)))
			}
			fgColor := colorRef(cell.FG)
			if cell.Faint {
				fgColor = term.DimColorRef(cell.FG)
			}
			winapi.SetTextColor.Call(hdc, uintptr(fgColor))
			drawGlyphRuns(hdc, left, top, &background, line, dx)
			x = end
		}
	}
	curX, curY := screen.X, screen.Y
	cursorVisible := screen.CursorVisible
	screenMu.Unlock()
	sessionMu.Lock()
	hasSession := session != nil
	sessionMu.Unlock()
	focused, _, _ := winapi.User32.NewProc("GetFocus").Call()
	isFocused := focused == hwnd

	if curX >= screen.Width {
		curX = screen.Width - 1
	}
	if curY >= screen.Height {
		curY = screen.Height - 1
	}

	showCursor := hasSession && cursorVisible && ((isFocused && cursorBlinkOn) || (!isFocused && true))
	if showCursor && curX >= 0 && curY >= 0 {
		cLeft := int32(12 + curX*cellWidth)
		cTop := int32(toolbarHeight + 6 + curY*cellHeight)
		cursorRc := rect{Left: cLeft, Top: cTop, Right: cLeft + cellWidth, Bottom: cTop + cellHeight}
		cursorBrush := brushRef(cursorBorder)
		winapi.FrameRect.Call(hdc, uintptr(unsafe.Pointer(&cursorRc)), cursorBrush)
		lastCursorRc = cursorRc
	}
	if previousFont != 0 {
		winapi.SelectObject.Call(hdc, previousFont)
	}
	winapi.SelectClipRgn.Call(hdc, 0)
	winapi.BitBlt.Call(windowDC, uintptr(clip.Left), uintptr(clip.Top), uintptr(clip.Right-clip.Left), uintptr(clip.Bottom-clip.Top), hdc, uintptr(clip.Left), uintptr(clip.Top), srcCopy)
	winapi.EndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	elapsed := time.Since(paintStarted)
	paintFrames++
	if elapsed >= 50*time.Millisecond && time.Since(lastSlowPaint) >= time.Second {
		telemetryf("paint_slow duration=%s cells=%d text_runs=%d frame=%d", elapsed, cells, runs, paintFrames)
		lastSlowPaint = time.Now()
	} else if paintFrames%100 == 0 {
		telemetryf("paint_sample duration=%s cells=%d text_runs=%d frame=%d", elapsed, cells, runs, paintFrames)
	}
}

// drawGlyphRuns emits one ExtTextOutW per contiguous script run so East Asian
// glyphs use a font that contains them. The fixed per-cell dx array keeps every
// glyph on the terminal grid regardless of the font's natural advances.
func drawGlyphRuns(hdc uintptr, left, top int32, clip *rect, line []uint16, dx []int32) {
	for start := 0; start < len(line); {
		font := terminalFont
		if term.RuneWidth(rune(line[start])) == 2 && cjkFont != 0 {
			font = cjkFont
		}
		end := start + 1
		for end < len(line) {
			next := terminalFont
			if term.RuneWidth(rune(line[end])) == 2 && cjkFont != 0 {
				next = cjkFont
			}
			if next != font {
				break
			}
			end++
		}
		if font != 0 {
			winapi.SelectObject.Call(hdc, font)
		}
		winapi.ExtTextOutW.Call(hdc, uintptr(left), uintptr(top), 0x0004,
			uintptr(unsafe.Pointer(clip)), uintptr(unsafe.Pointer(&line[start])),
			uintptr(end-start), uintptr(unsafe.Pointer(&dx[start])))
		for i := start; i < end; i++ {
			left += dx[i]
		}
		start = end
	}
}

func isSelected(position int) bool {
	first, last := selection.start, selection.end
	if first > last {
		first, last = last, first
	}
	return position >= first && position <= last
}

// colorRef converts an ANSI palette index or tagged 24-bit RGB value to GDI COLORREF.
func colorRef(color uint32) uint32 {
	return term.ColorRef(color)
}

// isIMEComposing returns true if an input method editor (IME) is actively composing text.
func isIMEComposing(hwnd uintptr) bool {
	hIMC, _, _ := winapi.ImmGetContext.Call(hwnd)
	if hIMC == 0 {
		return false
	}
	defer winapi.ImmReleaseContext.Call(hwnd, hIMC)
	res, _, _ := winapi.ImmGetCompositionStringW.Call(hIMC, gcsCompStr, 0, 0)
	return int32(res) > 0
}

func init() {
	runtime.LockOSThread()
}

// fatal reports startup failures in a dialog because the GUI subsystem has no
// console where a panic message could be read.
func fatal(format string, args ...any) {
	text, _ := syscall.UTF16PtrFromString(fmt.Sprintf(format, args...))
	caption, _ := syscall.UTF16PtrFromString("terminal-go")
	winapi.User32.NewProc("MessageBoxW").Call(0, uintptr(unsafe.Pointer(text)), uintptr(unsafe.Pointer(caption)), mbIconError)
	if telemetryFile != nil {
		telemetryf("fatal")
		_ = telemetryFile.Close()
	}
	os.Exit(1)
}

func loadAppIcon(instance uintptr, width, height, flags uintptr) uintptr {
	icon, _, _ := winapi.User32.NewProc("LoadImageW").Call(instance, iconResourceID, imageIcon, width, height, flags)
	return icon
}

func main() {
	runtime.LockOSThread()
	winapi.OleInitialize.Call(0)
	defer winapi.OleUninitialize.Call()
	initTelemetry()
	defer func() {
		telemetryf("shutdown")
		if telemetryFile != nil {
			_ = telemetryFile.Close()
		}
	}()
	telemetryf("main input_writer_start")
	go inputWriter()
	telemetryf("main before_get_module_handle")
	instance, _, _ := winapi.Kernel32.NewProc("GetModuleHandleW").Call(0)
	telemetryf("main after_get_module_handle instance=%d", instance)
	appIcon = loadAppIcon(instance, 0, 0, lrDefaultSize|lrShared)
	smallCx, _, _ := winapi.User32.NewProc("GetSystemMetrics").Call(smCxSmIcon)
	smallCy, _, _ := winapi.User32.NewProc("GetSystemMetrics").Call(smCySmIcon)
	appIconSmall = loadAppIcon(instance, smallCx, smallCy, lrShared)
	wc := wndClassEx{Size: uint32(unsafe.Sizeof(wndClassEx{})), Style: csHRedraw | csVRedraw, WndProc: syscall.NewCallback(windowProc), Instance: instance, Icon: appIcon, IconSm: appIconSmall, Background: colorWindow, ClassName: className}
	telemetryf("main before_register_class")
	if r, _, e := winapi.RegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		telemetryf("main register_class_failed err=%v", e)
		fatal("Failed to register window class: %v", e)
	}
	telemetryf("main after_register_class")
	telemetryf("main before_create_window")
	hwnd, _, e := winapi.CreateWindowExW.Call(0, uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(windowTitle)), wsOverlappedWindow|wsClipChildren|wsVisible, 100, 100, 1100, 720, 0, 0, instance, 0)
	if hwnd == 0 {
		telemetryf("main create_window_failed err=%v", e)
		fatal("Failed to create window: %v", e)
	}
	mainHwnd = hwnd
	telemetryf("main after_create_window hwnd=%d", hwnd)
	winapi.User32.NewProc("ShowWindow").Call(hwnd, swShow)
	telemetryf("main after_show_window")
	winapi.User32.NewProc("UpdateWindow").Call(hwnd)
	telemetryf("main after_update_window")
	var m msg
	for {
		r, _, _ := winapi.GetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		if m.Message == wmKeyDown && m.WParam == 0x0D { // VK_RETURN
			focus, _, _ := winapi.User32.NewProc("GetFocus").Call()
			if focus == hostEdit {
				if isIMEComposing(hostEdit) {
					winapi.TranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
					winapi.DispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
					continue
				}
				connectSSH(hwnd)
				continue
			}
		}
		winapi.TranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		winapi.DispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}
