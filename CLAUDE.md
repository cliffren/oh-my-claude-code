# TOC — TUI of Claude Code

A polished terminal UI (Bubble Tea) wrapping Claude Code's agent engine. Streaming chat, collapsible tool panels, session management, themes, sidebar.

## Build & Test

```bash
# Build with version
go build -ldflags "-X github.com/cliffren/toc/internal/version.Version=v1.0.0" -o ~/.local/bin/toc .

# Run tests
go test ./...
```

## Project Structure

```
main.go              → entry point, delegates to cmd.Execute()
cmd/                 → CLI commands (cobra), root command, mouse/IME handling
internal/
  app/               → application state
  config/            → config loading (~/.toc.json or .toc.json)
  db/                → SQLite session persistence
  llm/               → multi-LLM support (Claude, OpenAI, Gemini)
  tui/               → terminal UI components
    page/            → chat, logs, command palette
    layout/          → split views, sidebar
  message/           → message types
  session/           → session management
  pubsub/            → event bus
  permission/        → permission modes
  lsp/               → LSP integration
  format/            → markdown rendering
  version/           → version info (set via ldflags)
```

## Key Dependencies

- **bubbletea** v1.3.5 — TUI framework
- **glamour** v0.9.1 — markdown rendering
- **cobra** v1.9.1 — CLI
- **anthropic-sdk-go** v1.4.0 — Claude API
- **go-sqlite3** v0.25.0 — database
- **mcp-go** v0.17.0 — Model Context Protocol

## Conventions

- Bug fix workflow: when a bug is reported, first write a test that reproduces the bug (test should fail), then fix the code and verify the test passes.
- Go module: `github.com/cliffren/toc`
- Config dir: `~/.toc/` (migrated from `.omcc`)
- Active dev branch: `feature/chat-selection-scroll`, merge into `main` after each session
- Tag releases: `git tag v1.x.x && git push origin v1.x.x`
- Avoid calling SetContent/SetItems in View() — use rebuildViewport() on data change only
