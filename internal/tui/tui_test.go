package tui

import (
	"testing"

	"github.com/Krontx/oh-my-claude-code/internal/app"
	"github.com/Krontx/oh-my-claude-code/internal/lsp"
	tea "github.com/charmbracelet/bubbletea"
)

func TestTopLevelMouseHandlingDoesNotDisableMouseForChatSelection(t *testing.T) {
	model := New(&app.App{LSPClients: map[string]*lsp.Client{}}).(*appModel)

	updated, cmd := model.Update(tea.MouseMsg{
		X:      2,
		Y:      2,
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonLeft,
	})
	updatedModel := updated.(appModel)

	if !updatedModel.mouseEnabled {
		t.Fatal("mouse should remain enabled")
	}
	if cmd != nil {
		t.Fatal("expected no top-level disable mouse command")
	}
}
