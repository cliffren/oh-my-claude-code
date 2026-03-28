package dialog

import selectionpkg "github.com/Krontx/oh-my-claude-code/internal/tui/selection"

type clipboardFunc = selectionpkg.ClipboardFunc

type selectionController struct {
	selectionpkg.Controller
}

func (s selectionController) hasSelection() bool {
	return s.HasSelection()
}
