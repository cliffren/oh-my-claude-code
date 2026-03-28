package chat

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestMessagesCaptureMouseWhileSelectionDragIsActive(t *testing.T) {
	cmp := NewMessagesCmp(nil).(*messagesCmp)
	cmp.viewport.Width = 20
	cmp.viewport.Height = 4
	cmp.viewport.SetContent("hello world\nsecond line")

	_, _ = cmp.Update(tea.MouseMsg{X: 1, Y: 0, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	_, _ = cmp.Update(tea.MouseMsg{X: 5, Y: 0, Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft})

	if !cmp.CapturesMouse() {
		t.Fatal("expected chat messages pane to capture mouse during drag selection")
	}

	_, _ = cmp.Update(tea.MouseMsg{X: 5, Y: 0, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft})

	if cmp.CapturesMouse() {
		t.Fatal("expected chat messages pane to release mouse capture after selection ends")
	}
}
