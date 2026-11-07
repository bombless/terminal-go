# terminal-go

一个面向 Windows 的原生终端模拟器，目标是使用 **GCCGO** 编译，并通过 Windows API 实现窗口与控制台宿主。

## 当前开发状态

当前分支包含 Win32 GUI、终端屏幕缓冲区和基础 ANSI/VT 解析核心。解析器支持跨多次输入的 UTF-8 多字节字符与 CSI 序列，以及部分光标移动、擦除和基础 SGR 颜色控制。屏幕缓冲区有对应的回归测试。

ConPTY 会话、完整交互式 Shell、完整 VT 兼容性、Unicode 字形宽度与剪贴板尚未完成；不会将当前原型描述为完整终端。新增 Go 测试尚未在本次开发环境执行，GCCGO/Windows 构建也尚未验证。

## 目标环境

- Windows 10 1809 或更新版本（ConPTY 集成后）
- MinGW-w64 GCCGO，目标架构需与 Windows 一致
- 不使用 CGO

## 计划能力

- Win32 原生窗口、绘制与消息循环
- ConPTY + CreateProcessW 启动 PowerShell / CMD
- ANSI/VT100/VT220 转义序列、颜色、光标与滚动
- 键盘输入、Unicode、剪贴板和窗口尺寸同步

## 编译

尚未确定并验证 GCCGO/MinGW-w64 工具链版本与 Windows 目标三元组。请先参考 `TODO.md` 的 P0 清单；在真实 Windows + GCCGO 环境验证前，不提供未经验证的构建命令。

## 测试

在兼容的 Go 开发环境中可运行：

```sh
go test ./...
```

这仅用于终端核心的常规 Go 测试，不代表 GCCGO Windows 目标构建或 Win32 GUI 已通过验证。

## 目录

- `cmd/terminal`：Windows GUI 入口
- `internal/winapi`：Win32 API 封装
- `internal/term`：屏幕缓冲区与 VT 解析器
