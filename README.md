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
- Vertical split layout with sidebar showing modified files

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

# Non-interactive prompt
toc -p "Explain the use of context in Go"

# With debug logging
toc -d
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

## Acknowledgements

This project is a fork of [oh-my-claude-code](https://github.com/Krontx/oh-my-claude-code) by [@Krontx](https://github.com/Krontx), which itself is based on [OpenCode](https://github.com/opencode-ai/opencode) by [@opencode-ai](https://github.com/opencode-ai).

A huge thanks to both teams for their excellent work — the core agent engine and TUI architecture this project builds upon wouldn't exist without them.

## License

MIT — see [LICENSE](LICENSE)
