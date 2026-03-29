# Session Handoff

## Key Decisions

- **ctrl+e repurposed** — original behavior was "open temp file in external editor, send content as message". Changed to "open current project directory in configured editor". More useful in practice; the compose-in-editor flow had little real-world value.
- **Editor config priority** — `tui.editor` (config file) → `$EDITOR` env var → `vim` fallback. Deliberately dropped `nvim` as default since it's not universally installed. Documented in README.
- **GUI editor detached, not blocking** — `c.Start()` + `go c.Wait()` instead of `tea.ExecProcess`. GUI editors (VS Code, Cursor, Zed) exit their launcher process immediately; blocking the TUI would be wrong. Terminal editors that open a directory would also work but TUI won't block — acceptable trade-off.
- **Fixed silent ExecProcess bug** — `util.ReportError`/`util.ReportWarn` return `tea.Cmd`, not `tea.Msg`. Using them in a `tea.ExecProcess` callback caused silent drop (compiled fine since both are `interface{}`). Fixed by returning `util.InfoMsg{...}` directly.
- **User config set to `"code"`** — `~/.toc.json` `tui.editor` set to `"code"` (no `--wait`, since we're opening a directory not a file).

## Current State

- **Active branch**: `dev` (in sync with `main`)
- **main**: pushed to `https://github.com/cliffren/toc`
- **Installed binary**: `~/.local/bin/toc` at v1.0.0
- **Working tree**: clean
- **Tests**: 9 pass, 0 fail
- **Build**: clean

### Recent commits:
1. `e9da842` docs: update handoff
2. `5a1be07` docs: document tui.editor config option in README
3. `aacd825` feat: ctrl+e opens project in configured editor
4. `1f60bc2` feat: add input history with Up/Down keys and session persistence
5. `226d75a` fix: add copy notification to all selection areas, show git branch in sidebar, update CLI flags
6. `b6678ef` feat: add TodoWrite tool with sidebar task list display

## Unfinished Tasks

- None from this session.

## Known Issues

- **Terminal editors + ctrl+e** — `vim .` opens a directory browser but TUI won't block (by design for GUI editors). If a user sets a terminal editor, it opens in background and TUI stays active — may be confusing. Could detect GUI vs terminal editor via config or heuristic if needed.
- **`go c.Wait()` goroutine** — minor; process is reaped eventually but no explicit lifecycle management.
- **`.goreleaser.yml`** still references old module path `github.com/Krontx/oh-my-claude-code`; needs updating if releasing via goreleaser.

## Next Steps

1. **Test ctrl+e** — verify `code` opens the project directory correctly in VS Code
2. **Fix `.goreleaser.yml`** — update module path and binary name if planning a release
3. **Continue feature development** on `dev` branch
