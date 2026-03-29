# Session Handoff

## Key Decisions

- **Renamed binary/project to `toc`** — user didn't want the original `omcc` name; `toc` (TUI of Claude Code) chosen for its "Table of Contents" connotation and brevity
- **Renamed GitHub repo to `cliffren/toc`** — original `oh-my-claude-code` conflicted with another well-known project; all module paths updated accordingly
- **Merged feature branch into main by force-reset** — feature branch was far ahead of main; user chose to overwrite main entirely
- **Kept `feature/chat-selection-scroll` as active dev branch** — changes are merged into main after each session
- **Version tagged at v1.0.0** — fork diverged significantly from upstream; fresh versioning starting at v1.0.0
- **Build must use `-ldflags`** — `go build -ldflags "-X github.com/cliffren/toc/internal/version.Version=v1.0.0" -o ~/.local/bin/toc .`
- **Sidebar SetContent moved out of View()** — was called every frame (anti-pattern); now via `rebuildViewport()` only when data changes; ThemeChangedMsg handler added

## Current State

- **Active branch**: `feature/chat-selection-scroll`
- **main**: in sync with feature branch, pushed to `https://github.com/cliffren/toc`
- **Installed binary**: `~/.local/bin/toc` at v1.0.0
- **Working tree**: clean
- **Tests**: all pass
- **Build**: clean

## Unfinished Tasks

- **Rename project directory** — user wants to rename `/Users/rentao/Projects/oh-my-claude-code` → `/Users/rentao/Projects/toc`; deferred to avoid breaking current session context. Do at the start of next session.
- **Ghostty evaluation** — installed v1.3.1, config at `~/.config/ghostty/config` (font-size=16, theme=Atom One Dark); user hasn't fully switched from iTerm2 yet

## Known Issues

- **Old `.omcc` dirs not cleaned up** — data was migrated to `.toc` but originals still exist at:
  - `/Users/rentao/Projects/oh-my-claude-code/.omcc`
  - `/Users/rentao/Projects/ai-auto/test/.omcc`
  - `/Users/rentao/.omcc`
- **`omcc` binary** still at `~/.local/bin/omcc` — can be deleted

## Next Steps

1. **Rename project directory**: `mv /Users/rentao/Projects/oh-my-claude-code /Users/rentao/Projects/toc`, open new session from that path
2. **Clean up** old `.omcc` dirs and `~/.local/bin/omcc`
3. **Continue feature development** on `feature/chat-selection-scroll`
4. **Tag future releases**: `git tag v1.0.x && git push origin v1.0.x && go build -ldflags "..." -o ~/.local/bin/toc .`
