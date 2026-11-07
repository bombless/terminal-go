# terminal-go

一个面向 Windows 的原生终端模拟器，目标是使用 **GCCGO** 编译，并通过 Windows API 实现窗口与控制台宿主。

## 当前开发状态

当前分支正在搭建 Win32 GUI、终端屏幕缓冲区和 ANSI/VT 解析核心。ConPTY 会话、完整交互式 Shell、完整 VT 兼容性、Unicode 字形宽度与剪贴板尚未完成；不会将当前原型描述为完整终端。

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

待 GCCGO/MinGW-w64 工具链与 Windows CI 验证后补充精确命令。不同发行版的 GCCGO Windows 目标支持存在差异。

## 目录

- `cmd/terminal`：Windows GUI 入口
- `internal/winapi`：Win32 API 封装
- `internal/term`：屏幕缓冲区与 VT 解析器
