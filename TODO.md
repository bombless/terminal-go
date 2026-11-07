# TODO — terminal-go Windows Terminal

> 交接文档：供新的 ChatGPT 会话继续开发。当前工作分支为 `feature/windows-terminal`，基于 `main`。
> 当前 PR：[#1 — feat: bootstrap native Windows terminal foundation](https://github.com/bombless/terminal-go/pull/1)

## 项目目标

实现一个 Windows 原生终端模拟器，使用 **GCCGO** 编译，通过 Windows API 实现窗口、伪终端与 Shell 进程管理。不使用 CGO。

目标 Shell：PowerShell、CMD（后续可扩展到其他控制台程序）。

## 当前仓库状态

目前已经提交到 `feature/windows-terminal` 的文件：

- `go.mod` — Go module：`github.com/bombless/terminal-go`
- `README.md` — 项目方向、目录与当前状态说明
- `internal/term/screen.go` — 固定尺寸终端单元格缓冲区
- `internal/term/vt.go` — 基础 ANSI/VT 解析器
- `internal/winapi/procs_windows.go` — Win32 DLL / procedure 动态绑定
- `cmd/terminal/main.go` — Win32 GUI 窗口、GDI 绘制与消息循环

### 当前实现边界

**这是一个初始脚手架，不是可用的交互式终端。**

- GUI 窗口与消息循环已搭建。
- 屏幕缓冲区与少量 VT 指令解析已搭建。
- 尚未创建 ConPTY，也未启动 PowerShell/CMD。
- 尚未实现键盘输入、Shell 输出管道、窗口 resize。
- VT 解析器目前只覆盖少数 CSI 指令；字节到 Unicode 的处理也不完整。
- 尚未在 Windows 上用 GCCGO 实际编译或运行验证。

## TODO 优先级

### P0 — 先让工程能在目标工具链编译

- [ ] 确定目标 GCCGO 发行版、版本与目标三元组（例如 x86_64-w64-mingw32）。
- [ ] 在真实 Windows + GCCGO 环境编译整个项目，记录完整命令和错误。
- [ ] 验证 GCCGO 是否提供当前代码使用的 `syscall.NewLazyDLL`、`syscall.NewCallback`、`syscall.StringToUTF16Ptr` 等 API。
- [ ] 如果 GCCGO 的 syscall 包不兼容，改为适合该工具链的纯 Go Win32 动态调用封装；不要引入 CGO。
- [ ] 检查 Win32 结构体布局、字段对齐、指针宽度和调用约定，确保 32/64 位目标不会发生 ABI 错误。
- [ ] 添加可重复的 Windows 构建脚本（PowerShell 或 Makefile），并在 README 中写清楚前置条件。

### P1 — 完成最小可交互终端（ConPTY）

- [ ] 为 `internal/winapi` 补齐必要的 Win32 类型、常量和 API。
- [ ] 动态加载 `CreatePseudoConsole`、`ResizePseudoConsole`、`ClosePseudoConsole`（Kernel32）。
- [ ] 使用 `CreatePipe` 创建 ConPTY 输入/输出管道。
- [ ] 使用 `STARTUPINFOEXW`、属性列表与 `PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE` 将伪控制台关联到子进程。
- [ ] 使用 `CreateProcessW` 启动 PowerShell；提供 CMD 作为可配置 Shell。
- [ ] 正确处理句柄继承、关闭顺序、错误码与资源释放。
- [ ] 建立独立输出读取循环，将 Shell 输出送入终端解析器。
- [ ] 建立键盘输入路径，将字符和控制键编码后写入 ConPTY 输入管道。
- [ ] 处理窗口关闭、子进程退出、管道断开和异常退出。
- [ ] 明确最低系统版本：ConPTY 从 Windows 10 version 1809 / build 17763 开始提供。

### P2 — 终端渲染和输入

- [ ] 处理 WM_SIZE，根据客户区大小计算列数/行数。
- [ ] 根据新尺寸调用 `ResizePseudoConsole`，并调整屏幕缓冲区。
- [ ] 改进 GDI 绘制：字体、行高、列宽、背景色、前景色、光标。
- [ ] 实现颜色 SGR（基础 16 色、256 色，后续可选真彩色）。
- [ ] 实现光标移动、定位、保存/恢复、清屏、擦除行/屏、滚屏。
- [ ] 实现常用控制键：Enter、Backspace、Tab、方向键、Home/End、Delete、Ctrl+C 等。
- [ ] 正确处理 UTF-8 解码和 UTF-16 输入；支持非 BMP 字符。
- [ ] 处理组合字符和 East Asian Width（全角字符占两列）。
- [ ] 增加剪贴板复制/粘贴。
- [ ] 处理 DPI 缩放和字体尺寸变化。

### P3 — 稳定性、兼容性与体验

- [ ] 为 VT 解析器添加单元测试，覆盖跨多次 Feed 的分段转义序列。
- [ ] 添加屏幕缓冲区滚动、边界、擦除和光标测试。
- [ ] 测试 PowerShell、CMD、常见 TUI 程序（例如编辑器、Git 交互命令）。
- [ ] 处理窗口最小化、恢复、resize、快速输出和退出竞态。
- [ ] 避免 UI 线程被管道读写阻塞。
- [ ] 增加可配置字体、字号、配色和初始 Shell。
- [ ] 增加日志和可诊断错误信息。
- [ ] 添加 Windows CI（若可获得 GCCGO runner/toolchain）。
- [ ] 更新 README：安装、构建、运行、限制和已测试环境。

## 技术约束与实现建议

1. **GCCGO 是硬约束。** 不要默认标准 Go 的 Windows API 封装在 GCCGO 中都可用。每个关键 syscall/API 都要用指定 GCCGO 工具链实测。
2. **不使用 CGO。** Windows API 通过纯 Go syscall/动态过程调用接入。
3. **ConPTY 是 Shell 集成方案。** 不要只用 `cmd.exe` 标准输入输出重定向冒充完整终端；交互式 TUI 需要伪控制台语义。
4. **窗口消息与 I/O 分离。** Win32 UI 消息循环不能被 Shell 输出读取或管道写入阻塞。
5. **资源生命周期明确。** 句柄、伪控制台、进程、线程、管道与属性列表都要在成功/失败路径释放。
6. **先做可验证的垂直切片。** 优先达成“启动窗口 → ConPTY → 启动 PowerShell → 能输入命令并看到输出 → resize → 正常退出”，再扩展完整 VT 特性。
7. **不要声称已通过编译或运行测试，除非确实在目标 Windows/GCCGO 环境执行过。**

## 建议下一会话的第一步

1. 先读取仓库 `README.md`、`go.mod`、本文件和现有源文件，确认分支仍是 `feature/windows-terminal`。
2. 先解决 P0：确定 GCCGO 目标工具链并验证 Win32 API 调用方式。
3. 检查当前 Win32 结构体与回调签名；修复 ABI/编译问题后再开始 ConPTY。
4. 每完成一个可独立验证的阶段，提交小而清晰的 commit，并更新本文件的勾选状态。

## 当前 PR

继续在 PR #1 上迭代，不要另起一条无关分支，除非维护者明确要求。
