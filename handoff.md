# omcc Handoff

## 本次 Session 已完成

### TUI Bug 修复
- **滚动乱码** — editor.go 添加 `tea.MouseMsg` 消费 + `cmd/mousefilter.go` 全局状态机过滤 SGR 鼠标转义序列碎片（`tea.WithFilter`）
- **输入框布局** — split.go: bottomPanel 宽度改为 leftWidth，右侧 sidebar 占满全高，输入框只在左列下方
- **editor SetSize** — 去除冗余 SetWidth 调用
- **sidebar 选择/复制** — 添加 viewport + selectionController + clipboardWriter

### 定位修正：omcc 只是 TUI 壳
- coder/task prompt：Claude Code provider 返回空，不注入 system prompt
- claudecode.go：coder/task 用 `--append-system-prompt`，title/summarizer 用 `--system-prompt`
- claudecode.go：删除 prepend system message 到用户消息
- title 生成：prompt 简化 + 50 字符硬截断 + fallback 到用户消息
- bash.go：opencode → omcc 署名
- provider.go：opencode.ai → GitHub 仓库 URL

## 待开发功能（按优先级）

### P0 — 从 init 事件动态获取 slash_commands

Claude Code CLI `system.init` 事件包含关键字段：
```
slash_commands: [50+ 个命令]
tools: [24 个工具]
model, permissionMode, agents, skills, claude_code_version
```
当前 omcc 只硬编码了 7 个 CLI slash commands。

修复：claudecode.go 解析 init → 传给 TUI → 动态更新命令列表

### P0 — 权限确认透传验证

omcc 用 `--print` 模式调 CLI。需验证：
- CLI 的权限请求是否通过 stream-json 透传？
- 还是 `--print` 模式下 CLI 直接执行不经确认？
- omcc 自己的 permission 系统（internal/permission/）是否是唯一防线？

### P1 — Thinking/Tool 结果可折叠展开

当前固定截断（thinking 20 行，tool 10 行）。应支持折叠/展开切换。

### P1 — Plan mode 支持

检测 Claude 返回的 plan 相关事件，显示 plan 内容让用户确认。

### P1 — `--continue` 继续上次对话

启动时或 TUI 内快捷键继续最近对话。

### P2 — Session 管理增强（删除/重命名/搜索）

### P2 — Permission mode 切换 UI

### P3 — Worktree / Cost budget / Auto-compact
