package chat

import selectionpkg "github.com/cliffren/oh-my-claude-code/internal/tui/selection"

type clipboardExecFunc selectionpkg.ClipboardExecFunc

func clipboardCommand(goos string) (string, []string, error) {
	return selectionpkg.ClipboardCommand(goos)
}

func (f clipboardExecFunc) Write(text string) error {
	return selectionpkg.ClipboardExecFunc(f).Write(text)
}

func (f clipboardExecFunc) writeForPlatform(goos, text string) error {
	return selectionpkg.ClipboardExecFunc(f).WriteForPlatform(goos, text)
}

func defaultClipboardExec(name string, args []string, input string) error {
	return selectionpkg.DefaultClipboardExec(name, args, input)
}

func newClipboardWriter() clipboardWriter {
	return selectionpkg.NewClipboardWriter()
}
