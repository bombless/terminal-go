//go:build windows

package main

import (
 "syscall"
 "unsafe"
 "github.com/bombless/terminal-go/internal/term"
 "github.com/bombless/terminal-go/internal/winapi"
)
const ( csVRedraw=0x0001; csHRedraw=0x0002; wsOverlappedWindow=0x00CF0000; wsVisible=0x10000000; wmDestroy=0x0002; wmPaint=0x000F; colorWindow=5; swShow=5 )
type point struct{ X,Y int32 }; type msg struct{ Hwnd uintptr; Message uint32; WParam,LParam uintptr; Time uint32; Pt point }; type rect struct{ Left,Top,Right,Bottom int32 }
type paintStruct struct{ Hdc uintptr; Erase int32; Paint rect; Restore,IncUpdate int32; Reserved [32]byte }
type wndClassEx struct{ Size,Style uint32; WndProc uintptr; ClsExtra,WndExtra int32; Instance,Icon,Cursor,Background uintptr; MenuName,ClassName *uint16; IconSm uintptr }
var screen=term.NewScreen(100,32); var parser=term.NewParser(screen)
var className=syscall.StringToUTF16Ptr("TerminalGoWindow"); var windowTitle=syscall.StringToUTF16Ptr("terminal-go - GCCGO / Win32")
func windowProc(hwnd uintptr,message uint32,wParam,lParam uintptr) uintptr { switch message { case wmDestroy: winapi.PostQuitMessage.Call(0); return 0; case wmPaint: paint(hwnd); return 0 }; r,_,_:=winapi.DefWindowProcW.Call(hwnd,uintptr(message),wParam,lParam); return r }
func paint(hwnd uintptr) { var ps paintStruct; hdc,_,_:=winapi.BeginPaint.Call(hwnd,uintptr(unsafe.Pointer(&ps))); var rc rect; winapi.GetClientRect.Call(hwnd,uintptr(unsafe.Pointer(&rc))); brush,_,_:=winapi.CreateSolidBrush.Call(0x001E1E1E); winapi.FillRect.Call(hdc,uintptr(unsafe.Pointer(&rc)),brush); winapi.DeleteObject.Call(brush); winapi.SetBkMode.Call(hdc,1); winapi.SetTextColor.Call(hdc,0x00D4D4D4)
 for y:=0;y<screen.Height;y++ { line:=make([]uint16,screen.Width); for x:=0;x<screen.Width;x++ { r:=screen.Cells[y][x].Rune; if r==0 {r=' '}; if r>0xFFFF {r='?'}; line[x]=uint16(r) }; if len(line)>0 { winapi.TextOutW.Call(hdc,12,uintptr(12+y*20),uintptr(unsafe.Pointer(&line[0])),uintptr(len(line))) } }; winapi.EndPaint.Call(hwnd,uintptr(unsafe.Pointer(&ps))) }
func main() { instance,_,_:=winapi.Kernel32.NewProc("GetModuleHandleW").Call(0); wc:=wndClassEx{Size:uint32(unsafe.Sizeof(wndClassEx{})),Style:csHRedraw|csVRedraw,WndProc:syscall.NewCallback(windowProc),Instance:instance,Background:colorWindow,ClassName:className}; if r,_,e:=winapi.RegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)));r==0 {panic(e)}
 hwnd,_,e:=winapi.CreateWindowExW.Call(0,uintptr(unsafe.Pointer(className)),uintptr(unsafe.Pointer(windowTitle)),wsOverlappedWindow|wsVisible,100,100,1000,680,0,0,instance,0); if hwnd==0 {panic(e)}; _=parser; winapi.User32.NewProc("ShowWindow").Call(hwnd,swShow); winapi.User32.NewProc("UpdateWindow").Call(hwnd); var m msg
 for { r,_,_:=winapi.GetMessageW.Call(uintptr(unsafe.Pointer(&m)),0,0,0); if int32(r)<=0 {break}; winapi.TranslateMessage.Call(uintptr(unsafe.Pointer(&m))); winapi.DispatchMessageW.Call(uintptr(unsafe.Pointer(&m))) }
}
