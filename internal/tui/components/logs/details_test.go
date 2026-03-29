package logs

import (
	"testing"
	"time"

	"github.com/cliffren/toc/internal/logging"
	tea "github.com/charmbracelet/bubbletea"
)

func TestLogsDetailsCopiesSelectedViewportText(t *testing.T) {
	cmp := NewLogsDetails().(*detailCmp)
	cmp.currentLog = logging.LogMessage{
		ID:      "log-1",
		Time:    time.Unix(0, 0),
		Level:   "info",
		Message: "hello world",
	}
	cmp.width = 40
	cmp.height = 8
	cmp.viewport.Width = 40
	cmp.viewport.Height = 8
	cmp.updateContent()

	var copied string
	cmp.clipboard = clipboardFunc(func(text string) error {
		copied = text
		return nil
	})

	_, _ = cmp.Update(tea.MouseMsg{X: 2, Y: 3, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	_, _ = cmp.Update(tea.MouseMsg{X: 7, Y: 3, Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft})
	_, _ = cmp.Update(tea.MouseMsg{X: 7, Y: 3, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft})

	if copied != "hello" {
		t.Fatalf("got copied text %q want %q", copied, "hello")
	}
	if cmp.selection.hasSelection() {
		t.Fatal("expected selection to be cleared after release (auto-copy clears selection)")
	}
}
