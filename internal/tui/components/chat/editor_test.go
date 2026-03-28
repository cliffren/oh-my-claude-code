package chat

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestIsMouseProtocolArtifact(t *testing.T) {
	tests := []struct {
		name string
		msg  tea.KeyMsg
		want bool
	}{
		{
			name: "filters sgr mouse release fragment",
			msg:  tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("[<65;42;29m")},
			want: true,
		},
		{
			name: "filters sgr mouse press fragment",
			msg:  tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("[<0;42;29M")},
			want: true,
		},
		{
			name: "keeps normal text",
			msg:  tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("hello")},
			want: false,
		},
		{
			name: "filters mouse fragment with leading escape",
			msg:  tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("\x1b[<65;61;43M")},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isMouseProtocolArtifact(tt.msg); got != tt.want {
				t.Fatalf("isMouseProtocolArtifact() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEditorIgnoresMouseProtocolArtifactKey(t *testing.T) {
	editor := NewEditorCmp(nil).(*editorCmp)
	editor.textarea.SetValue("before")

	updated, _ := editor.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("[<65;42;29m")})
	got := updated.(*editorCmp)

	if got.textarea.Value() != "before" {
		t.Fatalf("got %q want %q", got.textarea.Value(), "before")
	}
}
