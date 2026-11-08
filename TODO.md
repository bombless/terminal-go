# TODO — 修复 SSH 终端鼠标选区渲染异常

## 问题描述

在 Windows 上运行 `D:\mcp-agent-workspace\terminal-go\terminal.exe`，连接 SSH 后，用鼠标左键拖动选择终端文字时出现两个问题：

1. 鼠标经过/选中的文字区域变成纯黑色，和终端背景融为一体，文字看不清或消失。
2. 选区变化时，原本位于该区域的文字似乎向右偏移，出现横向错位。

目标：修复选区背景/前景颜色和字符定位，确保拖动选择时文字稳定、不闪烁、不偏移；不破坏 SSH 输入、VT 解析、复制等现有功能。

## 已定位代码

主要文件：`cmd/terminal/main.go`

### 1. 选区颜色常量与颜色转换逻辑不匹配（高度可疑，已从代码确认）

绘制选区时（约第 701–710 行）：

```go
if selected {
    cell.BG, cell.FG = 0x00A56E2B, 0x00FFFFFF
}
...
if selection.active && isSelected(y*screen.Width+end) {
    next.BG, next.FG = 0x00A56E2B, 0x00FFFFFF
}
```

`colorRef`（约第 760 行）将最高字节非零的值识别为带标记的 24-bit RGB：

```go
func colorRef(color uint32) uint32 {
    if color&0xFF000000 != 0 {
        rgb := color & 0xFFFFFF
        return (rgb&0xFF)<<16 | (rgb & 0xFF00) | (rgb>>16)&0xFF
    }
    // 否则按 ANSI 调色板索引处理
    ...
}
```

因此，选区使用的 `0x00A56E2B` 和 `0x00FFFFFF` 最高字节均为 0，会走调色板索引分支，而不是 RGB 分支。它们都远超 255，最终得到的 RGB 为 0（黑色）。这能直接解释选区背景和前景颜色都变黑的问题。

修复时不要只随意给常量加标记；应先明确项目的颜色表示约定。可考虑使用明确的 RGB 构造/转换函数，或符合当前 tagged-RGB 约定的值，并补充单元测试，覆盖选区颜色以及 ANSI 调色板颜色，防止回归。

### 2. 文本绘制没有明确指定每个字符的网格间距（需要验证）

绘制代码约第 724–736 行：

```go
line := make([]uint16, end-x)
...
winapi.SetTextColor.Call(hdc, uintptr(colorRef(cell.FG)))
winapi.TextOutW.Call(hdc, uintptr(left), uintptr(top),
    uintptr(unsafe.Pointer(&line[0])), uintptr(len(line)))
```

终端网格尺寸在约第 60–62 行固定为：

```go
cellWidth     = 10
cellHeight    = 20
toolbarHeight = 48
```

每段文本的起点按 `12 + x*cellWidth` 计算，但 `TextOutW` 使用当前 GDI 字体的自然字宽绘制，并没有保证每个字符恰好占据 10 像素。拖动选区会改变文本 run 的分段边界，可能使不同 run 的文字位置看起来不连续或偏移。

需要检查窗口字体是否被显式设置（尤其是是否使用等宽字体、是否调用 `SelectObject`），并核实字体实际度量与 `cellWidth/cellHeight` 是否一致。考虑使用 `ExtTextOutW` 配合逐字符 dx 间距，或选择与网格度量严格匹配的等宽字体。不要在没有验证前假定这就是偏移的唯一原因。

### 3. 鼠标坐标映射

`mouseCell` 约第 406–423 行：

```go
x = (x - 12) / cellWidth
y = (y - toolbarHeight - 6) / cellHeight
```

绘制起点也使用左边距 12、顶部偏移 `toolbarHeight + 6`，目前看两处常量相互对应。仍需验证窗口 DPI、负坐标、边界和拖动捕获期间坐标是否一致。

### 4. 选区绘制路径

`paint` 约第 686–750 行：
- 用 `FillRect` 绘制背景色块。
- 将同一前景色/背景色的连续 cell 合并成 run。
- 对每个 run 调用 `TextOutW`。
- 选区通过 `isSelected(position)` 判断，位置按 `y*screen.Width+x` 线性计算。

检查 run 分段前后的颜色和文本绘制是否使用同一组 cell 边界；确认选区绘制没有修改底层 `screen.Cells`（当前代码使用 cell 副本，但应继续保持这一点）。

## 建议处理步骤

- [ ] 先检查当前工作树状态，保留用户已有改动；不要覆盖无关文件。
- [ ] 检查 `cmd/terminal/main.go` 的窗口字体创建/选择、GDI 绘制 API 声明和现有颜色编码约定。
- [ ] 修复选区颜色编码问题，确保选区背景和前景都不是黑色，并保持 ANSI/RGB 颜色转换正确。
- [ ] 修复或验证固定网格文本绘制：每个 cell 的绘制宽度必须与 `cellWidth` 一致，run 分段不能造成文字横向漂移。
- [ ] 核对鼠标坐标映射、窗口 DPI、拖动捕获和选区边界。
- [ ] 添加颜色转换单元测试；如可行，将可测试的颜色/网格计算逻辑提取到独立文件或包。
- [ ] 运行 `gofmt`、`go test ./...`、`go vet ./...` 和 Windows 构建。
- [ ] 在 Windows 实际运行程序，连接 SSH，验证拖动选区、反向拖选、跨行拖选、复制、全选、不同 ANSI 前景/背景色及窗口缩放。
- [ ] 确认未影响 SSH 会话、VT 屏幕内容和正常文本绘制。

## 验收标准

1. 鼠标拖动选中文字时，选区有清晰可见的背景色，选中文字清晰可读。
2. 选区开始、扩展、缩小和反向拖动时，原有字符始终停留在正确的终端 cell，不向右漂移、不重叠、不丢字。
3. 选区之外的文字和背景颜色不受影响；ANSI 颜色及 24-bit RGB 颜色仍正确。
4. 复制得到的文本与实际选区一致，跨行选择行为合理。
5. 测试和 Windows 构建通过，且完成真实 GUI + SSH 手动验证。

## 当前已知环境

- 仓库：`D:\mcp-agent-workspace\terminal-go`
- Go module：`github.com/bombless/terminal-go`
- Windows GUI 入口：`cmd/terminal/main.go`
- 终端屏幕与 VT 解析：`internal/term/`
- SSH/ConPTY：`internal/sshclient/`
- 本 TODO 仅针对鼠标选区颜色与文本定位问题；不要顺手重构 SSH 或 VT 架构。
