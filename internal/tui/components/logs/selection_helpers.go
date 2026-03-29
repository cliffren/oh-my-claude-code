package logs

import selectionpkg "github.com/cliffren/toc/internal/tui/selection"

type clipboardFunc = selectionpkg.ClipboardFunc

type selectionController struct {
	selectionpkg.Controller
}

func (s selectionController) hasSelection() bool {
	return s.HasSelection()
}
