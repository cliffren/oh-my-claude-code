# toc

**A polished TUI for Claude Code.**

`toc` (TUI of Claude Code) wraps [Claude Code](https://docs.anthropic.com/en/docs/claude-code)'s full agent engine in a beautiful [Bubble Tea](https://github.com/charmbracelet/bubbletea) terminal interface. You get Claude Code's complete capabilities — tools, hooks, MCP servers, custom agents, plan mode — rendered in a clean terminal UI.

## Features

- Real-time streaming chat with Claude Code
- Collapsible tool call panels (Bash, Read, Edit, Write, Glob, Grep, ...)
- Thinking/reasoning display
- Session management — search, rename, delete
- Inline `/` command menu and `ctrl+p` command palette
- Text selection and clipboard support
- Multiple theme support
- Vertical split layout with sidebar showing task progress and modified files
- Side-by-side diff view with syntax highlighting and intra-line change detection

## Prerequisites

- [Claude Code CLI](https://docs.anthropic.com/en/docs/claude-code) installed and authenticated
- Go 1.24+

## Install

Build from source:

```bash
git clone https://github.com/cliffren/toc.git
cd toc
go build -ldflags "-X github.com/cliffren/toc/internal/version.Version=v1.0.0" -o toc .
```

## Usage

```bash
# Interactive mode
toc

# Continue the most recent session
toc -c

# Non-interactive prompt
toc -p "Explain the use of context in Go"

# With debug logging
toc -d

# Specify working directory
toc -w /path/to/project
```

## Configuration

Config file: `~/.toc.json` or `.toc.json` in your project directory.

```json
{
  "providers": {
    "claude-code": {}
  },
  "agents": {
    "coder": { "model": "claude-code-sonnet" }
  }
}
```

Available models: `claude-code-sonnet`, `claude-code-opus`, `claude-code-haiku`

Set `CLAUDE_CODE_PATH` to use a custom Claude Code binary location.

## Improvements over the original

This fork adds a number of fixes and features on top of [oh-my-claude-code](https://github.com/Krontx/oh-my-claude-code):

- **Tool call grouping** — all tool calls from one assistant turn are collapsed into a single block; click to expand individual results
- **Inline `/` command menu** — type `/` to open a lightweight inline menu above the editor; `ctrl+p` still opens the full command palette
- **Session management** — search, rename, and delete sessions from the UI
- **Permission mode** — switch between permission modes via `ctrl+\` with a status bar badge showing the current mode
- **IME cursor fix** — physical cursor position is tracked correctly so macOS IME candidate windows appear in the right place
- **CJK table rendering** — markdown tables use runewidth-aware column alignment for CJK characters
- **Command palette stability** — fixed height, no resize on search or scroll
- **Vertical divider** — clear visual separator between the chat and sidebar panels
- **Task progress tracking** — TodoWrite tool with real-time sidebar display (✓ completed, ◉ in progress, ○ pending)
- **Git branch display** — sidebar shows current git branch next to working directory
- **Performance fixes** — avoid state mutation in `View()`, reuse pubsub subscription channels, O(1) tool response lookup
- **1M context window models** — added Sonnet/Opus 1M variants for long-context sessions

## Acknowledgements

This project is a fork of [oh-my-claude-code](https://github.com/Krontx/oh-my-claude-code) by [@Krontx](https://github.com/Krontx), which itself is based on [OpenCode](https://github.com/opencode-ai/opencode) by [@opencode-ai](https://github.com/opencode-ai).

A huge thanks to both teams for their excellent work — the core agent engine and TUI architecture this project builds upon wouldn't exist without them.

## License

MIT — see [LICENSE](LICENSE)
