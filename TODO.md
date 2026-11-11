# TODO — 实现 Kitty Graphics Protocol 图片显示

## 目标与复现

修复 Windows 版 terminal-go，使 SSH 远端程序可以通过 Kitty Graphics Protocol 在 GUI 终端窗口内显示图片。需要验证两种方式：

- PowerShell：ssh bombless@bombless.duckdns.org tview downloaded_image.jpg
- 先 ssh 登录，再在远端 shell 执行：tview downloaded_image.jpg

当前普通 SSH 文本正常，但图片不显示。注意：Kitty Graphics Protocol 的 inline payload 不是简单地把 .jpg 文件字节原样发送。常见 inline 格式是 PNG（f=100）或 RGB/RGBA 原始像素；发送端可能先把 JPG 解码/转换后再传输。必须按协议格式字段解码，不能根据文件扩展名猜协议格式。

## 已有诊断

1. internal/term/vt.go 目前处理普通 ESC、CSI、OSC 等序列，没有实现 Kitty 使用的 APC 控制序列。
2. Kitty Graphics Protocol 使用 APC，典型结构为 ESC _ G <control data> ; <base64 payload> ESC \\。parser 必须跨 Feed buffer 保留状态。
3. cmd/terminal/main.go 的绘制路径主要绘制字符、颜色和光标，没有图片缓存、placement 或图片绘制层。
4. SSH/ConPTY 输出读取路径在 cmd/terminal/main.go 的 readSessionOutput 附近：读取 s.Output 后调用 parser.Feed。协议查询响应还需要写回当前 PTY/SSH 输入流。
5. internal/winapi/procs_windows.go 已有部分 GDI lazy procedure 声明，但尚未发现完整图片绘制实现。

## 相关代码

- internal/term/vt.go：VT parser 状态机。
- internal/term/screen.go：屏幕、cell、光标状态。
- cmd/terminal/main.go：screen/parser 初始化、SSH 输出读取、WM_PAINT/绘制路径。
- internal/winapi/procs_windows.go：Win32 GDI API 声明。
- internal/sshclient/：SSH 与 PTY/ConPTY 输入输出。

行号需在开始实现时重新搜索，不要依赖旧行号。

## TODO

### P0 — 设计数据流

- [x] 检查 git 工作树和用户已有修改，保留无关改动。
- [x] 重新阅读 VT parser、screen、窗口绘制、SSH 输出读取和输入写入路径。
- [x] 确认 parser 跨 Feed 调用保存控制序列状态；APC 可被任意拆包。
- [x] 设计 parser 到图片管理器的事件/回调接口，以及图片管理器到绘制层的数据接口。
- [x] 设计协议响应回传路径：写入当前 PTY/SSH session 输入流；通过现有 inputQueue/inputWriter 异步写入，不在 screen 锁内执行 I/O。

### P1 — APC 解析

- [x] 增加 APC 状态机，识别 ESC _ 起始和 ST（ESC \\）结束。
- [x] 正确处理跨 buffer 的 APC、ESC、ST；payload 不得作为普通文字输出。
- [x] 识别 G 引导符，初步拆分 control data 与 base64 payload（完整参数语义及校验仍未完成）。
- [x] 支持基础参数解析、默认值、重复/非法字段处理及错误/状态响应；feature query（e=...）暂不支持。
- [x] 未知 APC 不得破坏现有 VT 解析。
- [ ] 增加测试：完整序列、分段输入、多个连续序列、空 payload、非法 base64、未知参数、结束符跨 buffer。（目前已覆盖跨 Feed、ST 拆分及未知/畸形 APC；其余场景待补。）

### P2 — 图片传输与缓存（MVP）

- [x] 增加图片管理器，维护 image ID、传输状态、分块 buffer、解码像素和生命周期。
- [x] 支持 inline base64 传输；至少支持 a=T（传输并显示）、a=t（传输）、a=p（显示已传输图像）。
- [x] 支持 m=1 分块续传及 m=0 最后一块，并保存首块 image ID/action/参数上下文。
- [x] 支持基础 a=q 查询并返回协议响应（feature query e=... 暂不支持）。
- [x] 支持 inline PNG（f=100）及 RGB/RGBA raw（f=24/32），校验尺寸、payload 长度和内存上限。
- [x] 使用 Go image、image/png 标准库解码；不根据文件扩展名猜测格式。
- [x] 限制图片尺寸、像素数、base64 长度、图片数量、placement 数量和总缓存。
- [x] 支持图片 ID 覆盖/复用及按 ID/全部删除。
- [x] 对不支持的传输方式、压缩和动作返回错误状态，不伪成功。

### P3 — 图片 placement 与屏幕状态

- [x] 定义 MVP placement：image ID、cell 位置/尺寸；源像素尺寸保存在解码图像中。
- [x] 支持按 APC 完成时的当前光标位置放置图片，并读取 c/r 行列尺寸参数。
- [x] 明确 MVP 像素尺寸与 cell 尺寸换算（当前 GUI cell 固定按 10x20 像素估算）。
- [x] 图片状态与文字 screen 分离，不把 payload 或占位符写进普通字符 cell。
- [ ] 明确清屏、滚动、光标移动、窗口 resize、alternate screen 切换时的行为。
- [x] MVP 限制为 live screen inline placement；滚动回溯、alternate screen 的图片状态尚未实现。

### P4 — Windows GUI 绘制

- [x] 在 WM_PAINT 路径增加图片层；文字先绘制，图片覆盖文字，光标最后绘制。
- [x] 增加 DIB section 与 AlphaBlend GDI API。
- [x] 将 RGBA 转为 BGRA/premultiplied-alpha 格式，支持透明 PNG。
- [x] 按 placement cell 矩形缩放绘制。
- [x] 图片输出、删除及窗口 resize 通过现有输出/窗口重绘路径触发更新；滚动回溯图像不显示。
- [x] DIB 资源按 image ID 缓存，不在每次 WM_PAINT 解码；替换/删除/关闭时释放 GDI 对象。
- [ ] 不得破坏文字、选区、光标和背景绘制。

### P5 — 测试与验收

- [ ] 为 APC parser、参数解析、分块重组、图片解码、placement 坐标和缓存生命周期添加单元测试。
- [ ] 添加协议 fixture，覆盖完整及分块传输。
- [x] 运行 gofmt、go test ./...、go vet ./...。
- [x] 执行 Windows amd64 构建。
- [ ] 在 Windows GUI 中连接 SSH，分别测试远端命令模式和交互登录模式。
- [ ] 用真实 Kitty Graphics Protocol 测试程序验证查询、PNG 显示、多图、删除和窗口 resize。
- [ ] 验证普通 SSH 文本、颜色、光标、键盘输入没有回归。
- [ ] 记录已支持协议子集和已知限制。

## MVP 推荐范围

优先实现 APC 跨 buffer 解析、a=q 查询响应、base64 PNG（f=100）、a=T/a=t/a=p、m=1/0 分块、当前可见 screen inline placement、Windows GDI 绘制及透明 PNG、图片删除与资源上限。

暂缓：文件路径传输（t=f；远端路径不能由本地 Windows 客户端直接读取）、共享内存传输、动画、复杂 z-index/裁剪组合和完整滚动回溯图像支持。对暂缓功能必须明确报告不支持。

## 验收标准

- Windows 版 terminal-go 执行 ssh user@host tview downloaded_image.jpg 时，远端程序发送 Kitty 图形协议后图片实际显示。
- 交互 SSH 登录后执行同一程序，图片同样显示。
- 至少一张 PNG 正确显示，透明区域、尺寸和位置合理。
- 分块 payload 不显示为乱码、不丢失；查询响应能返回远端。
- 图片删除后不再显示，资源能释放。
- 普通终端文字和 SSH 会话功能无回归。

## 当前进度（2026-11-11）

- 已修改 `internal/term/vt.go`：新增有上限的 APC 缓冲与 `ESC _` / `ESC \\` framing 状态，识别 Kitty `G` APC 并通过 `OnAPC` 回调交付结构化内容；APC 不进入普通屏幕文本。
- 已修改 `internal/term/vt_test.go`：新增跨 Feed APC、ST 跨 Feed、未知及畸形 APC 的测试。
- 已验证：`gofmt`、`go test ./...`、`go vet ./...`、Windows amd64 `go build -o $env:TEMP\\terminal-go-check.exe ./cmd/terminal` 均通过。
- 本次新增 `internal/kitty/manager.go` / `manager_test.go`：实现 a=T/a=t/a=p/a=d/a=q 基础动作、PNG/RGB/RGBA 解码、分块缓存、ID 管理、内存上限及协议响应。
- `cmd/terminal/main.go` 已将 parser.OnAPC 接入有界 graphicsQueue/graphicsWorker；图片解码不持有 screen 锁，响应通过 inputQueue/inputWriter 回写当前 PTY。WM_PAINT 使用 DIB section + AlphaBlend 绘制透明图片，并缓存 DIB。
- `internal/winapi/procs_windows.go` 增加 CreateDIBSection、AlphaBlend、RtlMoveMemory 声明。
- 本次已验证：gofmt、go test ./...、go vet ./...、Windows amd64 `go build ./cmd/terminal` 全部通过。
- 已启动新构建的 Windows GUI 并截图确认窗口正常出现，随后关闭 smoke-test 进程；但尚未完成真实 SSH 图片显示验收，因此不能宣称远端 `tview` 两种启动方式已实测成功。APC framing 的完整连续序列/空 payload 等边界测试仍需补齐。
- 当前明确限制：只支持 direct inline medium t=d；不支持 file/shared-memory、zlib 压缩、feature query（e=...）、动画、完整删除选择器、滚动回溯及 alternate-screen 图片管理。placement 暂按固定 10x20 cell 估算，不支持高级裁剪/z-index。
- 本次未修改或清理既有未跟踪的 `capture.py`、`dist/`、`terminal.exe`、`terminal.exe.old`；未创建 commit。
- 最近检查的工作树：`TODO.md`、`internal/term/vt.go`、`internal/term/vt_test.go` 有未提交修改；未跟踪 `capture.py`、`dist/`、`terminal.exe`、`terminal.exe.old`。这些既有文件/产物均未被本次修改或清理。
- 未创建 commit。

## 当前基线（开始工作前重新确认）

- 仓库：D:\mcp-agent-workspace\terminal-go
- Go module：github.com/bombless/terminal-go
- 平台：Windows GUI；SSH/PTY 输出经 VT parser 进入 screen。
- 最近一次在项目目录执行 go test ./...：通过（见上方当前进度）。
- 工作树存在上述未提交修改及未跟踪文件；开始后续工作时先重新检查，不要覆盖或清理用户数据。
- 本文件是实现交接文档；后续开始工作时重新检查代码，不要假定旧行号或状态仍有效。

## 参考

- Kitty Graphics Protocol 官方规范：https://sw.kovidgoyal.net/kitty/graphics-protocol/
- 重点阅读传输动作、控制数据格式、分块传输、查询响应、图像放置和删除。