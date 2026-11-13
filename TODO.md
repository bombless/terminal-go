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
- [x] 增加测试：完整序列、分段输入、多个连续序列、空 payload、非法 base64、未知参数、结束符跨 buffer。（覆盖跨 Feed、逐字节输入、ST 拆分、连续 APC、未知 APC；非法 base64 在图片管理器测试中覆盖。）

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
- [x] 明确清屏、滚动、光标移动、窗口 resize、alternate screen 切换时的行为（见下方“图片 placement 的屏幕状态语义”）。
- [x] 图片 placement 参与图文流：默认 C=0 时推进光标并随文本滚动；滚动回溯按滚动偏移绘制。alternate screen 的独立图片状态仍未实现。

#### 图片 placement 的屏幕状态语义

- placement 在 APC 完成时按当前光标 cell 坐标锚定；之后光标移动不会移动已有图片。
- 清屏/擦除文字不会自动删除图片；需使用 Kitty 删除动作。
- 普通滚动时 placement 随文字一同上移；查看 scrollback 时按 `placement.Y + scrollOffset` 绘制，图片与内容保持对齐。
- resize 保留 placement 的 cell 坐标与尺寸；超出客户区的部分由绘制区域裁剪，PTY 尺寸仍通过现有 session resize 路径调整。
- alternate screen 不维护独立图片集合；同一 manager 的 placement 会叠加到当前活动 screen。
- 上述行为是当前 MVP 语义，不代表完整 Kitty scrollback / alternate-screen 图像模型。

### P4 — Windows GUI 绘制

- [x] 在 WM_PAINT 路径增加图片层；文字先绘制，图片覆盖文字，光标最后绘制。
- [x] 增加 DIB section 与 AlphaBlend GDI API。
- [x] 将 RGBA 转为 BGRA/premultiplied-alpha 格式，支持透明 PNG。
- [x] 按 placement cell 矩形缩放绘制。
- [x] 图片输出、删除及窗口 resize 通过现有输出/窗口重绘路径触发更新；滚动回溯时图片按滚动偏移显示。
- [x] DIB 资源按 image ID 缓存，不在每次 WM_PAINT 解码；替换/删除/关闭时释放 GDI 对象。
- [ ] 不得破坏文字、选区、光标和背景绘制。

### P5 — 测试与验收

- [x] 为 APC parser、参数解析、分块重组、图片解码、placement 坐标和缓存生命周期添加单元测试。（包含 PNG 分块传输、RGB/RGBA raw、非法 base64/PNG、尺寸长度校验、未知参数、图片替换/删除及缓存计数。）
- [x] 添加协议 fixture，覆盖完整及分块传输。（internal/kitty/testdata/protocol.apc 含完整 RGBA APC 和双段续传；集成测试经 VT parser + manager 验证像素、placement 与 ACK。）
- [x] 运行 gofmt、go test ./...、go vet ./...。
- [x] 执行 Windows amd64 构建。
- [ ] 在 Windows GUI 中连接 SSH，分别测试远端命令模式和交互登录模式。
- [ ] 用真实 Kitty Graphics Protocol 测试程序验证查询、PNG 显示、多图、删除和窗口 resize。
- [ ] 验证普通 SSH 文本、颜色、光标、键盘输入没有回归。
- [x] 记录已支持协议子集和已知限制。（见 internal/kitty/PROTOCOL_SUPPORT.md。）

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

## 追加进度（2026-11-12）

- 新增 `internal/term/vt_test.go` 覆盖逐字节 APC 输入、ST 边界拆分、连续 APC 状态复用及未知 APC 不泄漏到普通文本。
- 扩展 `internal/kitty/manager_test.go` 覆盖 f=24 RGB、f=32 RGBA、raw payload 长度不匹配、未知扩展参数、feature query 明确拒绝、损坏 PNG 不入缓存、同 ID 图片替换及删除后的缓存/placement 生命周期。
- 最终验证通过：`gofmt`、`go test ./...`、`go test -race ./...`、`go vet ./...`、Windows amd64 `go build -o $env:TEMP\\terminal-go-check.exe ./cmd/terminal`。
- 远端 SSH 可用，且确认 `tview downloaded_image.jpg` 在直接远程命令与 `bash -lc` 登录 shell 两种方式均正常退出并输出 Kitty APC 流；捕获流各约 878 KB，包含 214 个 APC（首块 `a=T,f=100`，后续为分块续传）。这验证了远端发送端输出，但不等同于 GUI 图片显示成功。
- Windows GUI 构建启动 smoke test 成功，窗口进程响应正常，测试进程已关闭。尝试通过 GUI 建立 SSH 会话未成功，因此未完成 GUI 内图片显示、删除、resize 和普通文本回归的端到端验收；不得据此宣称验收通过。
- 本次仅修改两份测试文件及本 TODO；保留既有 `capture.py`、`dist/`、`terminal.exe`、`terminal.exe.old` 等未跟踪内容；未创建 commit。

## 追加进度（2026-11-12 实测补充）

- 新增 internal/kitty/testdata/protocol.apc：包含完整 RGBA 图片 APC 及两段式 m=1/m=0 分块传输；TestProtocolFixtureCompleteAndChunkedTransfers 从 VT parser 经过 manager 验证两个图片像素、placement 与 ACK。
- 新增 internal/kitty/PROTOCOL_SUPPORT.md，记录已支持协议子集、明确不支持项及 placement 在清屏、滚动、resize、光标移动和 alternate screen 下的当前语义。
- 重新执行 gofmt、go test ./...、go test -race ./...、go vet ./... 和 Windows amd64 GUI 构建，全部通过。
- 本次实际 SSH 复测直接远程命令和 bash -lc 两种方式：均退出码 0，均输出 877,907 字节、214 个 Kitty APC。远端图片为 784x1168 JPEG，tview 先转换为 f=100 PNG，并按 c=29/r=22 分 214 块发送。
- 将捕获的真实 877,907 字节流重放到生产 VT parser + Kitty manager：解析出 214 个 APC，成功重组并解码 784x1168 图片，生成 1 个 placement；manager 生成 1 条最终 ACK。捕获时使用 ssh -T（无 PTY），因此远端 tview 日志显示 not-a-tty，未能读取终端响应；这不是 GUI 端到端验收。
- 新构建 GUI 进程启动成功，窗口正常显示；自动化输入未能将 SSH host 字段提交成功，故 GUI 内 SSH、图片可视输出、删除/resize 与普通文字/选区/光标回归仍未验收。WM_PAINT 源码顺序确认是文字/选区、图片、光标，但不能替代视觉回归。
- 临时真实流诊断测试已删除；未创建 commit，未清理既有未跟踪文件和构建产物。
- 以上新增 APC 边界测试、协议 fixture 与真实流重放已补齐先前进度记录中提到的 parser/传输测试缺口；当前自动化验证通过。GUI 端到端验收仍是主要未完成项。

## 下一步：Windows GUI 手动验收清单

以下项目需要在真实可交互的 Windows GUI SSH 会话中完成并记录结果；源码审查、无 PTY SSH 捕获和 parser/manager 回放均不能代替这些验收。现在可直接使用新增的启动参数自动建立测试会话，避免每次手动填写 host 或重复输入测试命令：

```powershell
# 交互式 SSH：启动后自动连接，随后在终端中手动执行测试命令
.\\terminal.exe --ssh bombless@bombless.duckdns.org

# 远程命令模式：启动后自动连接并执行 tview，便于重复验证远端 APC 输出
.\\terminal.exe --ssh bombless@bombless.duckdns.org --ssh-command "tview downloaded_image.jpg"

# 本地 PowerShell 模式：启动后直接进入 PowerShell
.\\terminal.exe --powershell

# 本地 PowerShell 命令模式：执行诊断/准备命令后保留交互会话
.\\terminal.exe --powershell --powershell-command "Get-Date; Get-Location"
```

建议优先使用 `--ssh` 交互模式验证键盘输入，再使用 `--ssh-command` 远程命令模式重复验证图片输出。`--ssh-command` 应只传入远端命令，不要再嵌套调用本地 `ssh`；客户端会负责建立 SSH 会话。PowerShell 开关用于本地诊断/准备测试环境，不代替 SSH 图形协议验收。

- [ ] 使用 `.\\terminal.exe --ssh bombless@bombless.duckdns.org` 成功自动连接，确认交互式 SSH 登录及键盘输入正常。
- [ ] 在上述交互登录 shell 中执行 `tview downloaded_image.jpg`，确认 784x1168 图片实际可见、尺寸/位置合理且无 payload 乱码。
- [ ] 使用 `.\\terminal.exe --ssh bombless@bombless.duckdns.org --ssh-command "tview downloaded_image.jpg"` 启动远程命令模式，确认命令执行及图片实际可见；记录命令退出后的会话行为。
- [ ] 使用 `--powershell` 和 `--powershell-command` 验证本地 PowerShell 自动启动、命令执行及命令结束后交互会话保留（作为启动参数 smoke test，不替代 SSH 验收）。
- [ ] 验证 Kitty 查询请求得到正确响应；验证图片删除后立即消失且缓存/GDI 资源释放。
- [ ] 验证多图 placement、窗口 resize 后图片裁剪/位置行为符合当前 MVP 语义。
- [ ] 回归普通 SSH 文本、颜色、背景、选区、光标绘制及键盘输入；确认图片层未破坏这些显示/交互。
- [ ] 将 GUI 实测环境、操作步骤、结果及未通过项追加记录到本 TODO；只有全部必要项目实测通过后，才更新对应验收复选框。

## 参考

- Kitty Graphics Protocol 官方规范：https://sw.kovidgoyal.net/kitty/graphics-protocol/
- 重点阅读传输动作、控制数据格式、分块传输、查询响应、图像放置和删除。

## 追加进度（2026-11-12 本轮执行）

- 在 `D:\\mcp-agent-workspace\\terminal-go` 重新执行 `go test ./...`、`go test -race ./...`、`go vet ./...` 和 Windows amd64 `go build -o $env:TEMP\\terminal-go-check.exe ./cmd/terminal`，全部退出码 0。
- 复核 `cmd/terminal/main.go` 的 WM_PAINT 路径：终端背景先填充，文字及选区绘制后调用 `drawKittyImages`，光标最后绘制；图片层位于终端 clip region 内。此为源码顺序审查，不等同于视觉回归验收。
- 启动本轮构建的 `terminal-go-check.exe`，确认窗口创建成功且进程 Responding=True；截图可见 SSH host 输入框、Connect 按钮和 Not connected 状态。
- 尝试通过 GUI 输入 `bombless@bombless.duckdns.org` 并提交连接；本轮自动化键盘输入未能写入自绘 host 字段，GUI 始终保持 Not connected。未建立 GUI SSH 会话，因此没有执行远端 `tview`，也没有验证 GUI 图片像素显示、查询 ACK 往返、删除/多图/resize 或普通文字/选区/光标/键盘回归。
- 已关闭本轮启动的临时 `terminal-go-check` 进程；未触碰其他进程、原有 `terminal.exe`、`terminal.exe.old`、`dist/`、`capture.py` 或其他未跟踪文件。
- 本轮未修改产品源码；仅追加本执行记录到 TODO。未创建 commit。GUI 端到端验收仍未完成，相关复选框保持未勾选。

## 追加进度（2026-11-12 本轮 GUI 实测）

- 环境：Windows 11 amd64；使用本轮新构建的 `%TEMP%\\terminal-go-todo-check.exe`，远端 `bombless@bombless.duckdns.org`。
- 自动化回归再次通过：`go test ./...`、`go test -race ./...`、`go vet ./...`、Windows amd64 `go build ./cmd/terminal` 均退出码 0。
- `--powershell --powershell-command "Get-Date; Get-Location"`：GUI 成功启动并连接本地 PowerShell；截图确认命令输出及 `PS D:\\mcp>` 交互提示符，进程 Responding=True。该启动参数 smoke test 通过。
- `--ssh bombless@bombless.duckdns.org --ssh-command "tview downloaded_image.jpg"`：GUI 自动连接成功，远端命令运行并输出 tview 诊断；截图中未看到图片。该模式报告 stdin/stdout not-a-tty，不能据此作为交互终端图像验收通过。
- `--ssh bombless@bombless.duckdns.org`：GUI 交互 SSH 登录成功，看到远端欢迎信息和 shell prompt。通过剪贴板输入执行 `tview downloaded_image.jpg`，远端确认图片 784x1168、终端 grid 104x30，并向 stdout 发送 877,906 bytes、214 个 Kitty APC 分块（首块 `a=T,f=100`，`c=48,r=28`）。
- **交互模式图像验收失败**：tview 发送 Kitty `a=q` 探测（日志显示 `i=99,s=1,v=1,f=24,t=d`）后报告 `Probe response received (timeout): <empty>`；随后图像响应同样 timeout。GUI 截图中只有文本诊断，没有图片像素显示。由此确认当前仍未满足核心验收标准；需要继续定位 APC 查询响应是否被解析、graphicsWorker 是否执行及 inputWriter 是否把响应送回 SSH PTY，之后再验证图像绘制。不能将 parser/manager 单元测试或真实流回放等同于 GUI 成功。
- 本轮没有验证图片删除、多图、resize、透明像素、普通文字/颜色/选区/光标完整回归；对应 TODO 继续保持未勾选。交互 SSH 只验证了登录和命令输出，键盘原始输入回归也尚未充分验证。
- 已关闭本轮创建的三个 terminal-go GUI 进程及其直接子进程；没有终止其他用户进程。未修改产品源码，未清理用户文件/构建产物，未创建 commit。
- 结论：自动化检查及本地 PowerShell 启动 smoke test 通过；SSH GUI 可连接，但 Kitty 查询响应与图片显示实测失败。GUI 端到端验收未通过，后续应先修复此问题再继续完整视觉回归。

## 追加诊断（2026-11-12 ConPTY APC pass-through）

- 为区分 parser、manager 和 PTY 写回路径，临时在 `parser.OnAPC`、`graphicsWorker`、`writeInput` 与 `inputWriter` 增加只记录长度/状态的 telemetry；新构建运行真实 SSH `tview` 后，日志没有出现任何 APC callback 或 reply enqueue 记录。诊断日志代码随后已全部移除，`cmd/terminal/main.go` 恢复到本轮诊断前的 SHA-256（`b9141c5984f92685f802f61065fdd63bbe99eca45e60cce66f3bbb9bdc38d974`），未保留临时埋点。
- 又在本地 PowerShell ConPTY 会话中，使用 `[Console]::OpenStandardOutput().Write(...)` 直接输出一个原始 `ESC _ G ... ESC \\` Kitty APC；命令执行后 parser callback 同样没有触发。结合单元测试/真实流离线重放均能解析 APC，当前证据指向 APC 在 Windows ConPTY/控制台输出路径到达 `s.Output` 前被过滤或消费，而不是图片 manager 的 PNG 解码逻辑。
- 该诊断尚未通过独立原始 pipe SSH 实验最终确认 ConPTY 内部具体过滤环节；但已将故障边界缩小到 ConPTY 到 VT parser 之间。后续实现方向应评估 SSH 使用 raw stdin/stdout pipes（必要时 `ssh -tt` 强制远端 PTY）而非 ConPTY，确保 APC 字节原样进入 parser；同时设计远端窗口尺寸同步和键盘输入转发。不要在未验证 pipe 模式前宣称修复。
- 进一步用独立 raw-pipe SSH 验证：直接执行 `ssh -tt bombless@bombless.duckdns.org tview downloaded_image.jpg`，将 stdout 重定向到二进制文件（不经过 ConPTY）；退出码 0，捕获 880,753 bytes，检测到 215 个 `ESC _ G` APC 起始序列（包括探测与图像分块）。这证明强制远端 PTY 的 SSH 在普通 pipe stdout 下确实能交付 Kitty APC 字节；与 GUI ConPTY 路径未触发 parser callback 的结果形成对照。下一步可实现/验证 SSH 专用 raw-pipe session，并将键盘输入直接写入 ssh stdin。
- 本地 APC 探针 GUI 进程及子进程已关闭；未触碰其他用户进程。诊断埋点已移除；恢复源码后重新运行 `go test ./...`、`go vet ./...`、Windows amd64 build，全部通过。
## 新上下文交接（2026-11-12）

**当前结论：核心功能仍未验收通过。** Kitty APC parser/manager、离线真实流重放和自动化测试均通过，但 Windows GUI 经 ConPTY 的 SSH 会话收不到 APC；真实交互 tview 查询响应为空并超时，图片没有显示。不要把离线解析成功误报为 GUI 图片显示成功。

**下一步优先任务：实现 SSH 专用 raw-pipe transport，绕过 ConPTY。**

1. 先重新检查当前工作树、cmd/terminal/main.go、internal/sshclient/ 和 SSH session 接口；不要依赖旧行号，不要覆盖用户已有修改。
2. 设计 SSH 子进程的 stdin/stdout/stderr pipe：stdout 原始字节直接送入现有 VT parser（必须保留 ESC _ G APC）；stdin 接收终端键盘输入和 Kitty query ACK。stderr 单独处理，避免混入 VT/APC 数据。
3. 用 ssh -tt 保留远端 PTY，补上本地窗口 resize 到远端 PTY 的同步、Ctrl-C/EOF/退出码处理、进程关闭与 goroutine 生命周期；普通文本/ANSI 和键盘交互不得回归。不要把 raw-pipe 模式与无 PTY ssh -T 混淆。
4. 先加可重复的 transport/parser 集成测试，再用真实 GUI 运行 --ssh bombless@bombless.duckdns.org 并执行 tview downloaded_image.jpg；确认 a=q ACK 确实返回、图片像素实际显示后，再测 --ssh-command、删除、多图、resize、透明 PNG 和文字/选区/光标回归。
5. 每个文件修改前读取最新内容/hash，所有编辑使用 hash guard；完成后跑 gofmt、go test ./...、go test -race ./...、go vet ./... 和 Windows amd64 build，并把结果追加到本 TODO。只有 GUI 实测通过才勾选验收项。

**本轮验证基线：** go test ./...、go test -race ./...、go vet ./...、Windows amd64 build 全部通过；本轮诊断埋点已删除，cmd/terminal/main.go 恢复为诊断前内容（SHA-256 b9141c5984f92685f802f61065fdd63bbe99eca45e60cce66f3bbb9bdc38d974）。raw-pipe ssh -tt 捕获 880,753 bytes、215 个 APC 起始序列，确认该传输方式能保留 Kitty APC。

**工作区保护：** 本轮没有创建 commit，没有清理用户文件或构建产物。仓库原有 README.md、TODO.md、cmd/terminal/main.go、internal/kitty/manager_test.go、internal/term/vt_test.go 等修改及 build.ps1、capture.py、cmd/terminal/cli.go、cmd/terminal/cli_test.go、dist/、internal/kitty/PROTOCOL_SUPPORT.md、internal/kitty/testdata/、terminal.exe、terminal.exe.old 等未跟踪内容需继续保留；开始新上下文工作时先重新执行 git status 确认，不要 reset/clean/stash 或覆盖这些内容。

## 追加实验结论（2026-11-12 ConPTY flags / APC 字节透明性对照）

**结论：ConPTY 的 APC 字节透明性取决于底层 ConPTY 实现；单纯将 CreatePseudoConsole flags 改为 0x8 并不能修复 Windows inbox ConPTY。terminal-go 应继续保留 raw-pipe SSH 作为默认输出路径。**

- 对照代码：terminal-go 的 `internal/sshclient/conpty_windows.go` 使用 `CreatePseudoConsole(flags=0)`，没有 fallback；terminal-libghosty 的 `internal/terminal/conpty_windows.go` 先尝试 `flags=0x8`，失败再 fallback 到 0。
- terminal-libghosty 优先加载工作区自带的 `dist/conpty.dll` 与 `dist/OpenConsole.exe`。该 bundled DLL 导出所需 ConPTY API；使用它创建 flags=0x8 的 HPCON 成功（HRESULT 0，未 fallback）。
- 在 bundled conpty.dll + flags=0x8 下，用 PowerShell 子进程分别通过 `[Console]::Out.Write(string)`、`[Console]::OpenStandardOutput().Write(byte[])` 单次写入及 raw bytes 4 KiB 分块写入合成 Kitty APC。body 为 32 B、4 KiB、64 KiB、192 KiB、1 MiB、4 MiB；捕获帧长度及 SHA-256 均与期望完全一致。192 KiB、1 MiB、4 MiB raw-single 又各连续重复三次，均精确匹配。
- Windows inbox `kernel32!CreatePseudoConsole` 同样接受 flags=0x8（HRESULT 0），但这不代表它保留 APC。通过显式将 libghosty 的 ConPTY procedure 地址替换为 kernel32 导出地址后，192 KiB、1 MiB、4 MiB 三种合成 APC 连续三轮均只捕获到 49 bytes 标记区间，长度及 SHA-256 不匹配（first_mismatch=1）。
- terminal-go 现有 flags=0 ConPTY 路径也未通过字节完整性对照：32 B、4 KiB、64 KiB、192 KiB、1 MiB body 的三种写入方式均出现 49 bytes 标记区间及 SHA-256 mismatch。先前 raw-pipe SSH baseline 则曾连续精确捕获约 196 KB payload；另有 `ssh -tt` raw stdout 实验捕获 880,753 bytes、215 个 APC 起始序列。
- 因此不能将问题归因于 Console.Out 与 OpenStandardOutput 的 API 差异，也不能将 flags=0x8 视为通用修复。bundled DLL 的成功只证明该特定 ConPTY 实现/打包组合通过了本轮合成载荷测试；移植前仍需核实 DLL/OpenConsole 来源、版本、兼容性、许可及分发要求。
- 本轮没有修改生产代码或更改默认 transport。建议继续以 raw-pipe SSH（stdout 原始字节进入 VT parser、stdin 接收键盘与 Kitty ACK、stderr 独立处理，并保留 `ssh -tt` 远端 PTY）为实现方向；ConPTY 可作为普通终端兼容路径或后续独立评估对象，不应承担 Kitty APC 默认通路。

### 本轮验证结果

- libghosty 既有 ConPTY smoke / IO / long-output tests 连续运行通过；`TestKittyScriptE2E` 通过，报告 ImageID=1、placement Col=0 Row=18、Width=180 Height=180、像素 64x64（16,384 bytes）。
- terminal-go focused OpenSSH/PowerShell ConPTY tests 通过；libghosty focused passthrough 与 Kitty E2E tests 在临时探针清理后复跑通过。
- 最终编译检查通过：`go test ./internal/terminal -run '^$'`、`go test ./internal/sshclient -run '^$'`。
- 临时对照测试及诊断文件已清理；libghosty 原有 tracked probe 文件已从 HEAD 精确恢复，最终 git status clean。terminal-go 原有工作区修改及未跟踪文件仍保留；未修改生产代码，未创建 commit。
- 本轮尚未完成的进一步实验：1/16/256/16384 bytes 分块粒度矩阵、flush/no-flush 独立对照、Kitty query response 严格验收、SSH stderr/host key 行为、TUI resize、并发 session 与资源泄漏回归。

### TODO 状态调整

- [x] 对照 terminal-go flags=0、Windows inbox kernel32 flags=0x8、bundled conpty.dll flags=0x8 的 APC 字节透明性。
- [x] 对照 Console.Out、OpenStandardOutput raw byte 与 4 KiB 分块写入，覆盖最大 4 MiB 合成 payload。
- [ ] 核实 bundled conpty.dll / OpenConsole.exe 的来源、版本、许可和分发条件；未完成前不移植或依赖该 DLL。
- [ ] 完成 raw-pipe SSH transport 的实现与 GUI 端到端验收；当前实验只证明 raw pipe 可保留 APC 字节，尚未证明 GUI transport 已完成。
- [ ] 补齐分块粒度、flush、query response、stderr/host key、resize、并发和资源生命周期测试。

## 追加执行记录（2026-11-12 本轮 raw-pipe GUI 验证）

- 重新检查工作树：`README.md`、`TODO.md`、`cmd/terminal/main.go`、`internal/kitty/manager_test.go`、`internal/sshclient/conpty_windows.go`、`internal/sshclient/conpty_windows_test.go` 有既有修改；`EXP-RESULT.md`、`EXP.md`、`PLAN.md`、`TEST.md`、`capture.py`、`dist/`、`terminal.exe`、`terminal.exe.old` 为既有未跟踪内容。均未覆盖、清理或回滚。
- 当前代码已包含 Windows SSH raw-pipe transport：`StartRaw` 为 ssh.exe 建立独立 stdin/stdout/stderr 管道，GUI 默认选择 raw-pipe，SSH 参数使用 `-tt`；stderr 单独 drain，stdout 进入 VT parser，Kitty ACK 经现有 inputQueue/inputWriter 回写。ConPTY 保留为 `TERMINAL_SSH_TRANSPORT=conpty` 的显式实验选项。
- 从项目根目录执行未缓存 `go test -count=1 ./...`、`go test -race -count=1 ./...`、`go vet ./...` 和 Windows amd64 `go build -o %TEMP%\\terminal-go-current-check.exe ./cmd/terminal`，全部退出码 0。
- 使用本轮构建启动 GUI：`terminal-go-current-check.exe --ssh bombless@bombless.duckdns.org --ssh-command "tview downloaded_image.jpg"`。SSH 连接成功，远端命令退出码 0，GUI 进程保持响应；远端 tview 输出 214 个 Kitty APC 分块（报告 stdout 877,906 bytes）。
- 本轮 telemetry 证明 raw-pipe stdout 已将 Kitty APC 交给生产 parser：收到 `a=q` 后 manager 生成 12-byte 查询响应；最终 `m=0` 图像块被 manager 解码，生成 image ID 1、784x1168 pixels、29x22 cells placement，并生成 11-byte 图像 ACK。GDI `AlphaBlend` 返回成功，DIB draw 路径确实执行。stderr 单独收到 44 bytes；SSH 最终退出码 0。
- **视觉验收仍失败/未确认**：GUI 截图仍显示 tview 的文本诊断，没有清晰可见的图片；远端 tview 报告 `Image response received (timeout): <empty>`。telemetry 记录 placement 锚定在 cell `(0,27)`，目标矩形为 `(12,602)-(302,1042)`，而客户区高度只有 681 px，图像大部分位于客户区之外。需要先定位远端 cursor/placement 坐标和 tview 交互模式行为，不能把 `AlphaBlend` API 成功等同于图片视觉验收成功。查询探测阶段曾收到 `i=99;OK`，但最终图像响应超时仍未解释。
- raw-pipe session 的 `Resize` 当前明确返回“不支持远端 PTY window-size updates”；本轮没有实现或验证远端动态 resize。`tview` 诊断报告 window pixels `0x0` 并采用 cell size fallback，说明 raw-pipe 模式的远端尺寸/像素尺寸同步仍需设计。不能通过 ConPTY 替代，因为已有实验确认 ConPTY 会过滤 APC。
- 本轮没有验证交互 shell 模式下键盘输入、图片删除、多图、透明 PNG、resize、Host Key/认证失败提示、文字/颜色/选区/光标完整回归或并发资源生命周期；对应验收项继续保持未勾选。已关闭本轮启动的 GUI 测试进程；发现另有一个非本轮启动的同主机 `ssh.exe` 进程，未触碰。
- 本轮未修改产品源码；仅追加本执行记录。未创建 commit，未清理任何既有用户文件或构建产物。

### 下一步具体工作

1. 为 raw-pipe 增加可观测的 PTY resize 方案。当前 Windows OpenSSH 子进程的 stdin/stdout 是匿名 pipe，无法直接调用 ConPTY `ResizePseudoConsole`；需评估可维护的 SSH channel window-change 能力或受控的本地尺寸桥接，不能伪装 resize 已成功。
2. 定位 image ACK timeout：确认 11-byte ACK 是否确实完整写入 ssh stdin、是否被远端读到，以及 tview 等待的是哪个 image ID / 响应格式；增加不记录敏感内容的写入结果 telemetry 或独立 pipe 集成测试。
3. 调查 placement `(0,27)` 的来源，检查 tview 在 GUI 非交互命令模式和交互 shell 模式下是否发送光标定位序列；在确认 cursor 语义前不要擅自把图片强制放到屏幕顶部。
4. 完成交互 SSH 手动/自动输入测试，再分别验收 PNG 可视显示、删除、多图、resize 和普通文字/选区/光标回归。

## 追加执行记录（2026-11-12 本轮 ACK 生命周期排查）

- 重新检查工作树：既有修改和未跟踪文件与本轮开始时一致；未执行 reset、clean、stash，未覆盖或清理任何既有文件。
- 在 `cmd/terminal/main.go` 发现 SSH stdout EOF 与异步 graphics/input worker 存在关闭顺序竞争：`readSessionOutput` 可能在 `graphicsWorker` 完成最后一个 APC 之前将全局 session 置空并关闭输入 pipe，导致最终 Kitty ACK 虽已解析/排队却可能来不及写入。
- 增加 graphicsQueue 与 inputQueue 的 FIFO flush barrier：stdout EOF 后先等待此前 APC 全部处理，再等待此前输入/ACK 全部写入队列完成，然后才 detach/close session。barrier 不记录终端内容，也不改变协议 payload。
- 本轮验证通过：`gofmt -w cmd/terminal/main.go`、`go test -count=1 ./...`、`go test -race -count=1 ./...`、`go vet ./...`、Windows amd64 `go build -o %TEMP%\\terminal-go-todo-check.exe ./cmd/terminal`，全部退出码 0。
- 使用新构建 GUI 运行 `--ssh bombless@bombless.duckdns.org --ssh-command "tview downloaded_image.jpg"`：SSH 连接成功，截图确认 tview 的 Kitty feature probe 收到 `i=99;OK` 并判定 probe passed；生产 parser 收到 214 个 APC，manager 完成 784x1168 PNG 解码，创建 image ID 1、29x22 placement，并生成 11-byte `i=1;OK` 图像响应；GDI AlphaBlend 返回成功。
- **图像 ACK/视觉验收仍失败**：tview 仍报告 `Image response received (timeout): <empty>`，截图未见完整图片。placement 位于 cell `(0,27)`，目标矩形 `(12,602)-(302,1042)`，而客户区高度约 681 px，绝大部分图像被客户区裁掉。远端诊断报告 PTY grid 为 80x24、window pixels 为 0x0 并使用 fallback；本地 GUI parser grid 为 104x30，存在本地与远端 PTY 尺寸不一致。flush barrier 已保证 worker/输入队列顺序，但当前日志没有记录每次小型 pipe write 的完成字节数，因此尚不能据此断言远端确实读取到最终 image ACK。
- 本轮尝试的额外最小 ACK 探针命令因远端 Python 命令行引号转义错误而未形成有效结果；不作为通过或失败证据。首个 GUI 启动参数也曾因 PowerShell 参数拆分而被 CLI 拒绝，随后正确引用的 tview 命令运行成功。
- 本轮只修改 `cmd/terminal/main.go` 并追加本 TODO；没有修改 SSH transport 实现、没有实现 PTY resize，也没有完成交互 SSH、图片删除、多图、透明 PNG、resize 和普通文字/选区/光标/键盘回归。临时 GUI 测试进程已关闭；未创建 commit，未清理原有构建产物或未跟踪文件。

### 后续优先事项

1. 为不含输入内容的短写入增加完成字节数/错误 telemetry，或使用独立 raw-pipe SSH 集成探针确认 11-byte image ACK 被远端读取；核对 tview 实际等待的 image ID 与响应格式。
2. 解决 PTY 尺寸同步：当前 OpenSSH raw-pipe `-tt` 使用的远端 PTY 尺寸与 GUI grid 不一致（远端 80x24、本地 104x30），且远端 window pixels 为 0x0。评估可维护的 SSH channel window-change 实现；不要把 `Resize` 的“不支持”错误隐藏掉。
3. 追踪 APC 最终块完成时 cursor row 27 的来源，并确认远端是否在图像发送前发出了光标定位序列；不可简单强制把图片移动到顶部。
4. 在真实交互 SSH shell 中复测 `tview downloaded_image.jpg`，确认图片像素可见后再进行删除、多图、resize、透明 PNG 和常规终端回归。

## 新上下文交接摘要（2026-11-12，供直接续做）

- **当前实现状态**：SSH GUI 默认走 OpenSSH raw-pipe（`ssh -tt`），stdout 原始字节进入 VT parser，stderr 独立 drain，stdin 用于键盘输入及 Kitty ACK；ConPTY 仅保留为显式实验选项。`cmd/terminal/main.go` 已增加 graphics/input FIFO flush barrier，避免 stdout EOF 时过早关闭 session 导致末尾 ACK 丢失。
- **自动化验证基线**：最近一次 `gofmt`、`go test -count=1 ./...`、`go test -race -count=1 ./...`、`go vet ./...`、Windows amd64 GUI build 全部通过。新上下文开始时仍需先检查工作树，再按修改范围重跑验证。
- **最近 GUI 实测**：raw-pipe SSH 连接成功；tview Kitty feature probe 收到 `i=99;OK`；214 个 APC 成功进入 parser/manager，784x1168 PNG 解码成功，创建 image ID 1、29x22 placement，生成 11-byte `i=1;OK` 响应，GDI AlphaBlend 调用成功。但 tview 仍报告 `Image response received (timeout): <empty>`，截图没有确认图片可见，因此核心验收仍失败/未完成。flush barrier 只保证本地 worker 与队列顺序，不证明远端读取了 ACK。
- **当前关键疑点**：placement 锚定在 cell `(0,27)`，目标矩形 `(12,602)-(302,1042)`，GUI 客户区高度约 681 px，图像大部分被裁；远端 PTY grid 80x24、本地 GUI grid 104x30，远端 window pixels 为 0x0。必须分别调查 ACK 写入是否完整到达远端、tview 等待的响应 ID/格式、cursor row 27 来源，以及 SSH PTY resize/window-change。
- **建议新上下文第一步**：先 `git status`、读取最新 `cmd/terminal/main.go`、`internal/sshclient` transport 代码及相关测试；保留所有现存修改。然后优先添加不含终端内容的 pipe write 完成字节数/错误观测或远端 ACK 读取探针，确认 ACK 往返；不要先改 placement 坐标掩盖根因。
- **尚未验收**：交互 shell 键盘输入完整回归、图片实际可见、删除、多图、透明 PNG、PTY resize、普通文字/颜色/背景/选区/光标回归、并发与资源生命周期。相关验收复选框保持未勾选。
- **保护约束**：不要 reset/clean/stash，不要覆盖或清理用户已有修改、未跟踪文件、`dist/` 或构建产物；不要创建 commit，除非用户明确要求。

## 追加执行记录（2026-11-12 本轮 ACK 写入可观测性）

- 重新检查 Git 工作树并保留所有既有修改/未跟踪内容；本轮开始时 `cmd/terminal/main.go` 与 `TODO.md` 均已有用户修改，编辑前读取了完整文件 SHA-256。
- 根据上一轮交接，给 `inputWriter` 增加仅记录计数与错误的短写入 telemetry：对不超过 64 bytes 的输入（包括 Kitty query/image ACK）记录请求字节数、实际写入字节数、错误、锁等待/写入耗时和队列长度；发生任何写入错误时也记录。不会记录键盘输入或协议 payload 内容。
- 此观测只能证明字节已被 Windows 本地 SSH stdin pipe 接收，不能证明远端 SSH server / tview 已读取或接受 ACK；仍需结合真实 GUI 日志与远端探针判断。
- 本轮尚未完成测试与 Windows 构建；后续执行结果应追加在此处。未修改 SSH transport、未实现 raw-pipe PTY resize、未更改 placement 坐标；GUI 图片显示验收仍未通过。未创建 commit，未清理或覆盖任何用户文件/构建产物。修改文件前先读取当前内容/hash，并使用 hash guard。

### 本轮验证与真实 SSH 复测补充

- 随后已完成 `gofmt`、`go test -count=1 ./...`、`go test -race -count=1 ./...`、`go vet ./...` 和 Windows amd64 `go build ./cmd/terminal`，全部退出码 0。
- 新构建 GUI 真实连接 `bombless@bombless.duckdns.org` 并运行 `tview downloaded_image.jpg`：Kitty feature query ACK 为 12/12 bytes，成功写入本地 ssh.exe stdin pipe；收到 214 个 APC，最终 PNG 解码为 784x1168，创建 29x22 placement。
- 新 telemetry 捕获到关键失败：图像 ACK 请求 11 bytes，但实际写入 0 bytes，错误为 `The pipe is being closed`。也就是说 ACK 已由 manager 生成并入队，但远端 tview/ssh 子进程在本地 writer 执行最终写入前已关闭 stdin；此前 FIFO barrier 不能保证子进程仍存活，也不能证明远端读到 ACK。
- 时间观测：首个图像 APC 到最终 `m=0` APC 约 1.14 秒；最终 PNG 解码/转换耗时约 41.6 ms。GDI AlphaBlend 调用成功，但 placement 矩形 `(12,602)-(302,1042)` 超出 681 px 客户区下边界，不能视为图片可见验收通过。
- 本轮 GUI 进程已停止，随后确认未遗留其 SSH 子进程。未修改其他用户进程或清理构建产物；未创建 commit。
- **下一步应优先解决 ACK 时序/远端等待超时**：分别测量远端 tview 等待窗口、SSH 输出 APC 分块到达节奏和最终 ACK 写入时刻；不要把“本地 manager 生成响应”当成“远端收到响应”。随后实现可维护的 SSH PTY window-change/resize，并处理 cursor row 27 与可视区域问题。所有 GUI 视觉与交互回归继续保持未验收。

## 追加执行记录（2026-11-12 本轮 PNG 解码优化与 ACK 往返探针）

- `internal/kitty/manager.go` 将 PNG/raw 像素到 NRGBA 的逐像素 `At`/颜色模型转换改为 `image/draw.Draw`，使用 Go 标准库对常见 PNG 解码图像类型的优化路径；不提前 ACK，不降低 PNG 完整解码校验。
- 真实 784x1168 PNG 的 manager 最终块处理时间从此前约 41.6 ms 降至本轮约 24.6 ms（包含 manager 处理总耗时）；真实 GUI 中仍未让 `tview downloaded_image.jpg` 通过，tview 继续显示 `Image response received (timeout): <empty>`，图片 placement 仍位于 cell `(0,27)`，超出客户区下沿。
- 新增一次隔离的端到端 ACK 探针：远端 Python 在 `stty raw -echo` 下发送合法 1x1 PNG 的 Kitty `a=T,f=100,m=0` APC，随后读取恰好 11 bytes；GUI 生产 parser/manager 解码并创建 image ID 1、1x1 placement，`inputWriter` 记录 `requested_bytes=11 written_bytes=11 err=<nil>`，远端程序输出 `ACK_MATCH=True` 并以退出码 0 结束。证明 raw-pipe SSH 的 ACK 格式及 stdin 往返本身可用。
- 尝试 350 ms 延迟最终图片 ACK 的实验仍未修复 tview 超时，且导致 ACK 写入时 pipe 已关闭；该延迟及 helper 已完整撤销，不保留启发式等待。
- 最终验证通过：`gofmt`、`go test -count=1 ./...`、`go test -race -count=1 ./...`、`go vet ./...`、Windows amd64 `go build ./cmd/terminal`，全部退出码 0。
- 当前判断：普通 Kitty 图片 ACK 可被远端 raw PTY 程序正确接收；tview 特定路径仍有响应时序/读取行为问题，不能宣称图片显示验收通过。动态 PTY resize、tview 的等待阶段、placement row 27 和 GUI 视觉/交互回归仍待完成。未创建 commit，未清理或覆盖其他用户文件/构建产物。

## 追加执行记录（2026-11-13 本轮 TODO 执行）

- 重新检查 Git 工作树：既有 tracked 修改为 `README.md`、`TODO.md`、`cmd/terminal/main.go`、`internal/kitty/manager.go`、`internal/kitty/manager_test.go`、`internal/sshclient/conpty_windows.go`、`internal/sshclient/conpty_windows_test.go`；既有未跟踪文件/目录为 `EXP-RESULT.md`、`EXP.md`、`PLAN.md`、`TEST.md`、`capture.py`、`dist/`、`terminal.exe`、`terminal.exe.old`。本轮均未覆盖、清理、暂存或回滚。
- 本轮从项目根目录重新执行 `go test -count=1 ./...`、`go test -race -count=1 ./...`、`go vet ./...` 及 Windows amd64 `go build ./cmd/terminal`，全部退出码 0；构建输出为 `%TEMP%\\terminal-go-todo-verify.exe`。
- 使用正确引用的启动参数运行新构建 GUI：`--ssh bombless@bombless.duckdns.org --ssh-command "tview downloaded_image.jpg"`。SSH 连接成功，远端 tview 输出 880,839 bytes，stdout 读取 EOF，SSH 退出码 0；GUI 进程保持响应。
- 本轮生产 telemetry 观察到 214 个 Kitty APC 分块；最终块处理耗时约 28.5 ms，成功解码 784x1168 PNG，创建 image ID 1、placement cell `(0,27)`、尺寸 `29x22`。最终图像 ACK 请求/写入均为 11/11 bytes、无写入错误；GDI AlphaBlend 返回成功。
- **GUI 视觉验收仍失败**：截图中仍只显示 tview 诊断文字，未见图片像素。placement 像素矩形为 `(12,602)-(302,1042)`，客户区为 `1067x681`，图片主体被客户区下沿裁切。远端 tview 仍报告 `Image response received (timeout): <empty>`，尽管本地 SSH stdin pipe 已完整接收 11-byte ACK；这只能证明写入本地子进程管道，不能证明远端应用读到/接受 ACK。
- 本轮测试启动的 GUI 进程已关闭；未触碰其他进程。未修改产品源码、未创建 commit、未清理任何原有文件或构建产物。交互 shell、动态 PTY resize、图片删除/多图/透明 PNG、普通文本/颜色/选区/光标/键盘回归仍未验收，相关复选框保持未勾选。
