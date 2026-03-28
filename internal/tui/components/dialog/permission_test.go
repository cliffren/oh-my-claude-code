package dialog

import (
	"testing"

	"github.com/Krontx/oh-my-claude-code/internal/llm/tools"
	"github.com/Krontx/oh-my-claude-code/internal/permission"
	tea "github.com/charmbracelet/bubbletea"
)

func TestPermissionDialogCopiesSelectedViewportText(t *testing.T) {
	cmp := NewPermissionDialogCmp().(*permissionDialogCmp)
	cmp.width = 60
	cmp.height = 20
	cmp.permission = permission.PermissionRequest{
		ID:       "perm-1",
		ToolName: tools.BashToolName,
		Params: tools.BashPermissionsParams{
			Command: "printf hello-world",
		},
	}
	cmp.contentViewPort.Width = 56
	cmp.contentViewPort.Height = 8
	cmp.contentViewPort.SetContent("hello world\nsecond line")

	var copied string
	cmp.clipboard = clipboardFunc(func(text string) error {
		copied = text
		return nil
	})

	region := cmp.contentSelectionRegion()
	_, _ = cmp.Update(tea.MouseMsg{X: region.X, Y: region.Y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	_, _ = cmp.Update(tea.MouseMsg{X: region.X + 5, Y: region.Y, Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft})
	_, _ = cmp.Update(tea.MouseMsg{X: region.X + 5, Y: region.Y, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft})

	if copied != "hello" {
		t.Fatalf("got copied text %q want %q", copied, "hello")
	}
	if cmp.selection.hasSelection() {
		t.Fatal("expected selection to be cleared after release (auto-copy clears selection)")
	}
}
