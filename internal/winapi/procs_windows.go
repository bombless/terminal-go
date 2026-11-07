//go:build windows

package winapi

import "syscall"

// Lazy DLL/procedure lookup keeps this package free of cgo and external link steps.
var (
 User32=syscall.NewLazyDLL("user32.dll"); Gdi32=syscall.NewLazyDLL("gdi32.dll"); Kernel32=syscall.NewLazyDLL("kernel32.dll")
 RegisterClassExW=User32.NewProc("RegisterClassExW"); CreateWindowExW=User32.NewProc("CreateWindowExW"); DefWindowProcW=User32.NewProc("DefWindowProcW")
 GetMessageW=User32.NewProc("GetMessageW"); TranslateMessage=User32.NewProc("TranslateMessage"); DispatchMessageW=User32.NewProc("DispatchMessageW"); PostQuitMessage=User32.NewProc("PostQuitMessage")
 BeginPaint=User32.NewProc("BeginPaint"); EndPaint=User32.NewProc("EndPaint"); FillRect=User32.NewProc("FillRect"); GetClientRect=User32.NewProc("GetClientRect")
 TextOutW=Gdi32.NewProc("TextOutW"); CreateSolidBrush=Gdi32.NewProc("CreateSolidBrush"); DeleteObject=Gdi32.NewProc("DeleteObject"); SetTextColor=Gdi32.NewProc("SetTextColor"); SetBkMode=Gdi32.NewProc("SetBkMode")
)
