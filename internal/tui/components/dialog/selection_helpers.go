package dialog

import selectionpkg "github.com/cliffren/oh-my-claude-code/internal/tui/selection"

type clipboardFunc = selectionpkg.ClipboardFunc

type selectionController struct {
	selectionpkg.Controller
}

func (s selectionController) hasSelection() bool {
	return s.HasSelection()
}
