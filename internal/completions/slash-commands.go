package completions

// InternalCommands are handled by the TUI directly, not sent to Claude.
var InternalCommands = map[string]bool{
	"model": true, "sessions": true, "theme": true,
	"effort": true, "help": true, "new": true,
}
