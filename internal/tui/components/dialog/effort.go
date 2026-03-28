package dialog

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/Krontx/oh-my-claude-code/internal/config"
	"github.com/Krontx/oh-my-claude-code/internal/llm/models"
	"github.com/Krontx/oh-my-claude-code/internal/tui/layout"
	"github.com/Krontx/oh-my-claude-code/internal/tui/styles"
	"github.com/Krontx/oh-my-claude-code/internal/tui/theme"
	"github.com/Krontx/oh-my-claude-code/internal/tui/util"
)

var effortLevels = []string{"low", "medium", "high", "max"}

type EffortSelectedMsg struct {
	Effort string
}

type CloseEffortDialogMsg struct{}

type EffortDialog interface {
	tea.Model
	layout.Bindings
}

type effortDialogCmp struct {
	selectedIdx int
	width       int
	height      int
	canReason   bool
	modelName   string
}

type effortKeyMap struct {
	Up     key.Binding
	Down   key.Binding
	Enter  key.Binding
	Escape key.Binding
	J      key.Binding
	K      key.Binding
}

var effortKeys = effortKeyMap{
	Up:     key.NewBinding(key.WithKeys("up"), key.WithHelp("↑", "previous")),
	Down:   key.NewBinding(key.WithKeys("down"), key.WithHelp("↓", "next")),
	Enter:  key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "select")),
	Escape: key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "close")),
	J:      key.NewBinding(key.WithKeys("j"), key.WithHelp("j", "next")),
	K:      key.NewBinding(key.WithKeys("k"), key.WithHelp("k", "previous")),
}

func (e *effortDialogCmp) Init() tea.Cmd {
	cfg := config.Get()
	coder := cfg.Agents[config.AgentCoder]
	model := models.SupportedModels[coder.Model]
	e.canReason = model.CanReason
	e.modelName = model.Name

	current := strings.ToLower(coder.ReasoningEffort)
	for i, level := range effortLevels {
		if level == current {
			e.selectedIdx = i
			return nil
		}
	}
	e.selectedIdx = 2
	return nil
}

func (e *effortDialogCmp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, effortKeys.Up) || key.Matches(msg, effortKeys.K):
			if e.selectedIdx > 0 {
				e.selectedIdx--
			} else {
				e.selectedIdx = len(effortLevels) - 1
			}
		case key.Matches(msg, effortKeys.Down) || key.Matches(msg, effortKeys.J):
			if e.selectedIdx < len(effortLevels)-1 {
				e.selectedIdx++
			} else {
				e.selectedIdx = 0
			}
		case key.Matches(msg, effortKeys.Enter):
			return e, util.CmdHandler(EffortSelectedMsg{Effort: effortLevels[e.selectedIdx]})
		case key.Matches(msg, effortKeys.Escape):
			return e, util.CmdHandler(CloseEffortDialogMsg{})
		}
	case tea.WindowSizeMsg:
		e.width = msg.Width
		e.height = msg.Height
	}
	return e, nil
}

func (e *effortDialogCmp) View() string {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()

	if !e.canReason {
		msg := baseStyle.Foreground(t.Warning()).Render(
			fmt.Sprintf("%s does not support effort levels", e.modelName),
		)
		return baseStyle.Padding(1, 2).
			Border(lipgloss.RoundedBorder()).
			BorderBackground(t.Background()).
			BorderForeground(t.TextMuted()).
			Render(msg)
	}

	const dialogWidth = 30

	title := baseStyle.
		Foreground(t.Primary()).
		Bold(true).
		Width(dialogWidth).
		Padding(0, 0, 1).
		Render("Select Effort Level")

	items := make([]string, len(effortLevels))
	for i, level := range effortLevels {
		itemStyle := baseStyle.Width(dialogWidth)
		if i == e.selectedIdx {
			itemStyle = itemStyle.Background(t.Primary()).
				Foreground(t.Background()).Bold(true)
		}
		label := strings.ToUpper(level[:1]) + level[1:]
		items[i] = itemStyle.Render(label)
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

func (e *effortDialogCmp) BindingKeys() []key.Binding {
	return layout.KeyMapToSlice(effortKeys)
}

func NewEffortDialogCmp() EffortDialog {
	return &effortDialogCmp{}
}
