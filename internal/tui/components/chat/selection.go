package chat

import (
	selectionpkg "github.com/Krontx/oh-my-claude-code/internal/tui/selection"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type selectionPoint = selectionpkg.Point

type selectionRegion = selectionpkg.Region

type scrollbarThumb = selectionpkg.ScrollbarThumb

type clipboardWriter = selectionpkg.ClipboardWriter

type clipboardFunc = selectionpkg.ClipboardFunc

type selectionController struct {
	controller selectionpkg.Controller
}

func normalizeSelectionBounds(start, end selectionPoint) (selectionPoint, selectionPoint) {
	return selectionpkg.NormalizeBounds(start, end)
}

func extractSelectedText(lines []string, start, end selectionPoint) string {
	return selectionpkg.ExtractText(lines, start, end)
}

func hitTestSelectionRegion(region selectionRegion, x, y int) (selectionPoint, bool) {
	return selectionpkg.HitTestRegion(region, x, y)
}

func clampSelectionPoint(region selectionRegion, x, y int) selectionPoint {
	return selectionpkg.ClampPoint(region, x, y)
}

func (s *selectionController) handleMouse(msg tea.MouseMsg, region selectionRegion, lines []string, writer clipboardWriter) (bool, string, bool, error) {
	return s.controller.HandleMouse(msg, region, lines, writer)
}

func (s selectionController) hasSelection() bool {
	return s.controller.HasSelection()
}

func (s selectionController) capturesMouse() bool {
	return s.controller.CapturesMouse()
}

func (s selectionController) bounds() (selectionPoint, selectionPoint) {
	return s.controller.Bounds()
}

func (s *selectionController) clear() {
	s.controller.Clear()
}

func highlightSelectedLines(lines []string, start, end selectionPoint, style lipgloss.Style) []string {
	return selectionpkg.HighlightLines(lines, start, end, style)
}

func renderSelectionSegment(style lipgloss.Style, segment string) string {
	return selectionpkg.RenderSegment(style, segment)
}

func calculateScrollbarThumb(viewHeight, contentHeight, scrollOffset int) scrollbarThumb {
	return selectionpkg.CalculateScrollbarThumb(viewHeight, contentHeight, scrollOffset)
}

func renderScrollbar(viewHeight int, thumb scrollbarThumb, track, handle string) []string {
	return selectionpkg.RenderScrollbar(viewHeight, thumb, track, handle)
}
