//go:build windows

package winapi

import "syscall"

// Lazy DLL/procedure lookup keeps this package free of cgo and external link steps.
var (
	User32           = syscall.NewLazyDLL("user32.dll")
	Gdi32            = syscall.NewLazyDLL("gdi32.dll")
	Kernel32         = syscall.NewLazyDLL("kernel32.dll")
	Ole32            = syscall.NewLazyDLL("ole32.dll")
	Imm32            = syscall.NewLazyDLL("imm32.dll")
	RegisterClassExW = User32.NewProc("RegisterClassExW")
	CreateWindowExW  = User32.NewProc("CreateWindowExW")
	DefWindowProcW   = User32.NewProc("DefWindowProcW")
	GetMessageW      = User32.NewProc("GetMessageW")
	TranslateMessage = User32.NewProc("TranslateMessage")
	DispatchMessageW = User32.NewProc("DispatchMessageW")
	PostQuitMessage  = User32.NewProc("PostQuitMessage")
	BeginPaint       = User32.NewProc("BeginPaint")
	EndPaint         = User32.NewProc("EndPaint")
	FillRect         = User32.NewProc("FillRect")
	GetClientRect    = User32.NewProc("GetClientRect")
	TextOutW         = Gdi32.NewProc("TextOutW")
	ExtTextOutW      = Gdi32.NewProc("ExtTextOutW")
	SelectObject     = Gdi32.NewProc("SelectObject")
	CreateFontW      = Gdi32.NewProc("CreateFontW")
	CreateSolidBrush = Gdi32.NewProc("CreateSolidBrush")
	DeleteObject     = Gdi32.NewProc("DeleteObject")
	SetTextColor     = Gdi32.NewProc("SetTextColor")
	SetBkColor       = Gdi32.NewProc("SetBkColor")
	SetBkMode        = Gdi32.NewProc("SetBkMode")
	FrameRect        = User32.NewProc("FrameRect")

	OleInitialize            = Ole32.NewProc("OleInitialize")
	OleUninitialize          = Ole32.NewProc("OleUninitialize")
	ImmGetContext            = Imm32.NewProc("ImmGetContext")
	ImmReleaseContext        = Imm32.NewProc("ImmReleaseContext")
	ImmGetCompositionStringW = Imm32.NewProc("ImmGetCompositionStringW")

	// ConPTY and process APIs are exported by Kernel32 on supported Windows versions.
	CreatePseudoConsole               = Kernel32.NewProc("CreatePseudoConsole")
	ResizePseudoConsole               = Kernel32.NewProc("ResizePseudoConsole")
	ClosePseudoConsole                = Kernel32.NewProc("ClosePseudoConsole")
	CreatePipe                        = Kernel32.NewProc("CreatePipe")
	InitializeProcThreadAttributeList = Kernel32.NewProc("InitializeProcThreadAttributeList")
	UpdateProcThreadAttribute         = Kernel32.NewProc("UpdateProcThreadAttribute")
	DeleteProcThreadAttributeList     = Kernel32.NewProc("DeleteProcThreadAttributeList")
	CreateProcessW                    = Kernel32.NewProc("CreateProcessW")
	GetExitCodeProcess                = Kernel32.NewProc("GetExitCodeProcess")
	WaitForSingleObject               = Kernel32.NewProc("WaitForSingleObject")
	CloseHandle                       = Kernel32.NewProc("CloseHandle")
	TerminateProcess                  = Kernel32.NewProc("TerminateProcess")
)
