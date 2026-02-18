# oh-my-claude-code (omcc)

**Claude Code's brain, OpenCode's beauty.**

A polished [Bubble Tea](https://github.com/charmbracelet/bubbletea) TUI that wraps [Claude Code](https://docs.anthropic.com/en/docs/claude-code)'s full agent engine. You get Claude Code's complete capabilities — tools, hooks, MCP servers, custom agents, plan mode — rendered in OpenCode's beautiful terminal interface.

## Why?

Claude Code has the best AI coding agent capabilities but basic terminal output. OpenCode has a gorgeous TUI but its own AI backend. **omcc** combines them: Claude Code does the thinking, OpenCode does the rendering.

## Prerequisites

- [Claude Code CLI](https://docs.anthropic.com/en/docs/claude-code) installed and authenticated
- Go 1.24+

## Install

```bash
go install github.com/Krontx/oh-my-claude-code@latest
```

Or build from source:

```bash
git clone https://github.com/Krontx/oh-my-claude-code.git
cd oh-my-claude-code
go build -o omcc .
```

## Usage

```bash
# Interactive mode
omcc

# Non-interactive prompt
omcc -p "Explain the use of context in Go"

# With debug logging
omcc -d
```

## How It Works

omcc spawns `claude` as a subprocess with `--output-format stream-json`, mapping Claude Code's streaming events to OpenCode's TUI components:

- **Text streaming** renders incrementally in the chat view
- **Tool uses** (Bash, Read, Edit, Write, etc.) display as collapsible panels
- **Thinking/reasoning** shows in a dedicated section
- **Session resume** works across restarts via `--resume`

Claude Code manages its own agentic loop — tool execution, permission handling, MCP servers, hooks, and `.claude/` configuration all work exactly as they do in the native CLI.

## Configuration

Config file: `~/.omcc.json` or `.omcc.json` in your project directory.

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

Set `CLAUDE_CODE_PATH` environment variable to use a custom Claude Code binary location.

## Attribution

Based on [OpenCode](https://github.com/opencode-ai/opencode) (MIT License). See [NOTICE.md](NOTICE.md) for details.

## License

MIT — see [LICENSE](LICENSE)
