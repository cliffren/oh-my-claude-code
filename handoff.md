# Session Handoff

## Key Decisions

- **TodoWrite tool as in-memory store** — chose not to persist todo state to DB; it's session-scoped and transient. Uses pubsub.Broker for real-time sidebar updates, matching the pattern of all other services (history, messages, etc.)
- **pubsub.Broker must be pointer-embedded** — discovered that value-embedding `pubsub.Broker` causes nil map panic on `Subscribe()`. All services use `*pubsub.Broker` initialized via `pubsub.NewBroker()`.
- **Git branch display is not auto-refreshing** — `cwd()` runs `git rev-parse` on each `rebuildViewport()` call. No dedicated watcher for branch changes; updates piggyback on file change events. User accepted this trade-off to avoid polling overhead.
- **Input history uses existing message DB** — no new table needed. On session switch, loads all user messages from `app.Messages.List()` as history. New messages are appended in-memory during the session.
- **shift+enter not possible in terminal** — Bubble Tea cannot distinguish shift+enter from enter. Kept existing `\` + Enter for newlines.
- **CLI flag change** — `-c` reassigned from `cwd` to `continue`, `-w` now used for `cwd`
- **Dev branch renamed** — `feature/chat-selection-scroll` renamed to `dev`

## Current State

- **Active branch**: `dev` (in sync with `main`)
- **main**: pushed to `https://github.com/cliffren/toc`
- **Installed binary**: `~/.local/bin/toc` at v1.0.0
- **Working tree**: clean
- **Tests**: 9 pass, 0 fail
- **Build**: clean

### Session commits (3):
1. `1f60bc2` feat: add input history with Up/Down keys and session persistence
2. `226d75a` fix: add copy notification to all selection areas, show git branch in sidebar, update CLI flags
3. `b6678ef` feat: add TodoWrite tool with sidebar task list display

## Completed Tasks

- **Created CLAUDE.md** — project documentation for future sessions
- **Cleaned up old artifacts** — deleted `~/.omcc`, `~/Projects/ai-auto/test/.omcc`, `~/.local/bin/omcc`
- **TodoWrite tool** — LLM can manage task lists; sidebar shows tasks with status icons (✓/◉/○)
- **Fixed Anthropic provider** — `Required` field was not passed in tool schema; now all 4 providers are aligned
- **Copy notification** — added "Copied to clipboard" notification to sidebar, permission dialog, and log details (was missing)
- **Git branch in sidebar** — cwd line shows current branch name
- **CLI flags** — `-c` for continue, `-w` for cwd
- **Input history** — Up/Down keys cycle through sent messages, persisted via message DB
- **README updated** — added new features, usage examples

## Known Issues

- **TodoStore.Get() returns a copy** — defensive copy added, but if someone adds mutation logic later, worth noting
- **`.goreleaser.yml` still references old module path** — `github.com/Krontx/oh-my-claude-code` in ldflags; needs updating if releasing via goreleaser
- **Ghostty evaluation** — installed v1.3.1 but user hasn't fully switched from iTerm2

## Next Steps

1. **Test TodoWrite in real usage** — run toc with a complex multi-step task and verify sidebar updates correctly
2. **Test input history** — verify Up/Down works across session switches and restarts
3. **Fix `.goreleaser.yml`** — update module path and binary name from old project
4. **Continue feature development** on `dev` branch
5. **Tag releases** — `git tag v1.0.x && git push origin v1.0.x`
