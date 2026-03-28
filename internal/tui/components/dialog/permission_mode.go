package dialog

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/Krontx/oh-my-claude-code/internal/tui/layout"
	"github.com/Krontx/oh-my-claude-code/internal/tui/styles"
	"github.com/Krontx/oh-my-claude-code/internal/tui/theme"
	"github.com/Krontx/oh-my-claude-code/internal/tui/util"
)

// Permission mode values matching Claude Code CLI --permission-mode flag.
var permissionModes = []struct {
	value       string
	label       string
	description string
}{
	{"default", "Default", "Ask before dangerous operations"},
	{"acceptEdits", "Accept Edits", "Auto-accept all file edits"},
	{"bypassPermissions", "Bypass All", "Skip all permission checks"},
	{"plan", "Plan Only", "Plan but don't execute anything"},
}

type PermissionModeSelectedMsg struct {
	Mode string
}

type ClosePermissionModeDialogMsg struct{}

type PermissionModeDialog interface {
	tea.Model
	layout.Bindings
	SetCurrentMode(mode string)
}

type permissionModeDialogCmp struct {
	selectedIdx int
	width       int
	height      int
}

type permissionModeKeyMap struct {
	Up     key.Binding
	Down   key.Binding
	Enter  key.Binding
	Escape key.Binding
	J      key.Binding
	K      key.Binding
}

var permissionModeKeys = permissionModeKeyMap{
	Up:     key.NewBinding(key.WithKeys("up"), key.WithHelp("↑", "previous")),
	Down:   key.NewBinding(key.WithKeys("down"), key.WithHelp("↓", "next")),
	Enter:  key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "select")),
	Escape: key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "close")),
	J:      key.NewBinding(key.WithKeys("j"), key.WithHelp("j", "next")),
	K:      key.NewBinding(key.WithKeys("k"), key.WithHelp("k", "previous")),
}

func (d *permissionModeDialogCmp) SetCurrentMode(mode string) {
	for i, m := range permissionModes {
		if m.value == mode {
			d.selectedIdx = i
			return
		}
	}
	d.selectedIdx = 0
}

func (d *permissionModeDialogCmp) Init() tea.Cmd { return nil }

func (d *permissionModeDialogCmp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, permissionModeKeys.Up) || key.Matches(msg, permissionModeKeys.K):
			if d.selectedIdx > 0 {
				d.selectedIdx--
			} else {
				d.selectedIdx = len(permissionModes) - 1
			}
		case key.Matches(msg, permissionModeKeys.Down) || key.Matches(msg, permissionModeKeys.J):
			if d.selectedIdx < len(permissionModes)-1 {
				d.selectedIdx++
			} else {
				d.selectedIdx = 0
			}
		case key.Matches(msg, permissionModeKeys.Enter):
			return d, util.CmdHandler(PermissionModeSelectedMsg{Mode: permissionModes[d.selectedIdx].value})
		case key.Matches(msg, permissionModeKeys.Escape):
			return d, util.CmdHandler(ClosePermissionModeDialogMsg{})
		}
	case tea.WindowSizeMsg:
		d.width = msg.Width
		d.height = msg.Height
	}
	return d, nil
}

func (d *permissionModeDialogCmp) View() string {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()

	const dialogWidth = 36

	title := baseStyle.
		Foreground(t.Primary()).
		Bold(true).
		Width(dialogWidth).
		Padding(0, 0, 1).
		Render("Permission Mode")

	items := make([]string, len(permissionModes))
	for i, m := range permissionModes {
		itemStyle := baseStyle.Width(dialogWidth)
		if i == d.selectedIdx {
			itemStyle = itemStyle.Background(t.Primary()).Foreground(t.Background()).Bold(true)
		}
		desc := baseStyle.Width(dialogWidth)
		if i == d.selectedIdx {
			desc = desc.Background(t.Primary()).Foreground(t.Background())
		} else {
			desc = desc.Foreground(t.TextMuted())
		}
		label := m.label
		if i == d.selectedIdx {
			label = "● " + label
		} else {
			label = "  " + label
		}
		items[i] = lipgloss.JoinVertical(lipgloss.Left,
			itemStyle.Render(label),
			desc.Render("  "+strings.Repeat(" ", 2)+m.description),
		)
	}

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		title,
		baseStyle.Width(dialogWidth).Render(lipgloss.JoinVertical(lipgloss.Left, items...)),
	)

	return baseStyle.Padding(1, 2).
		Border(lipgloss.RoundedBorder()).
		BorderBackground(t.Background()).
		BorderForeground(t.TextMuted()).
		Width(lipgloss.Width(content) + 4).
		Render(content)
}

func (d *permissionModeDialogCmp) BindingKeys() []key.Binding {
	return layout.KeyMapToSlice(permissionModeKeys)
}

func NewPermissionModeDialog() PermissionModeDialog {
	return &permissionModeDialogCmp{}
}
