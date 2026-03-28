package dialog

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/Krontx/oh-my-claude-code/internal/tui/layout"
	"github.com/Krontx/oh-my-claude-code/internal/tui/styles"
	"github.com/Krontx/oh-my-claude-code/internal/tui/theme"
	"github.com/Krontx/oh-my-claude-code/internal/tui/util"
)

// Command represents a command that can be executed
type Command struct {
	ID          string
	Title       string
	Description string
	Category    string
	Handler     func(cmd Command) tea.Cmd
}

func (ci Command) Render(selected bool, width int) string {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()

	descStyle := baseStyle.Width(width).Foreground(t.TextMuted())
	itemStyle := baseStyle.Width(width).
		Foreground(t.Text()).
		Background(t.Background())

	if selected {
		itemStyle = itemStyle.
			Background(t.Primary()).
			Foreground(t.Background()).
			Bold(true)
		descStyle = descStyle.
			Background(t.Primary()).
			Foreground(t.Background())
	}

	title := itemStyle.Padding(0, 1).Render(ci.Title)
	if ci.Description != "" {
		description := descStyle.Padding(0, 1).Render(ci.Description)
		return lipgloss.JoinVertical(lipgloss.Left, title, description)
	}
	return title
}

// CommandSelectedMsg is sent when a command is selected
type CommandSelectedMsg struct {
	Command Command
}

// CloseCommandDialogMsg is sent when the command dialog is closed
type CloseCommandDialogMsg struct{}

// CommandDialog interface for the command selection dialog
type CommandDialog interface {
	tea.Model
	layout.Bindings
	SetCommands(commands []Command)
}

type commandDialogCmp struct {
	searchInput  textinput.Model
	allCommands  []Command
	filtered     []Command
	selectedIdx  int
	maxVisible   int
	width        int
	height       int
}

type commandKeyMap struct {
	Enter  key.Binding
	Escape key.Binding
	Up     key.Binding
	Down   key.Binding
}

var commandKeys = commandKeyMap{
	Enter: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "select"),
	),
	Escape: key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "close"),
	),
	Up: key.NewBinding(
		key.WithKeys("up"),
	),
	Down: key.NewBinding(
		key.WithKeys("down"),
	),
}

func (c *commandDialogCmp) Init() tea.Cmd {
	return textinput.Blink
}

func (c *commandDialogCmp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, commandKeys.Enter):
			if c.selectedIdx >= 0 && c.selectedIdx < len(c.filtered) {
				return c, util.CmdHandler(CommandSelectedMsg{
					Command: c.filtered[c.selectedIdx],
				})
			}
		case key.Matches(msg, commandKeys.Escape):
			return c, util.CmdHandler(CloseCommandDialogMsg{})
		case key.Matches(msg, commandKeys.Up):
			if c.selectedIdx > 0 {
				c.selectedIdx--
			}
			return c, nil
		case key.Matches(msg, commandKeys.Down):
			if c.selectedIdx < len(c.filtered)-1 {
				c.selectedIdx++
			}
			return c, nil
		}
	case tea.WindowSizeMsg:
		c.width = msg.Width
		c.height = msg.Height
	}

	// Update search input
	prevValue := c.searchInput.Value()
	var cmd tea.Cmd
	c.searchInput, cmd = c.searchInput.Update(msg)

	// Re-filter if query changed
	if c.searchInput.Value() != prevValue {
		c.applyFilter()
	}

	return c, cmd
}

func (c *commandDialogCmp) applyFilter() {
	query := strings.ToLower(strings.TrimSpace(c.searchInput.Value()))
	if query == "" {
		c.filtered = c.allCommands
	} else {
		words := strings.Fields(query)
		c.filtered = make([]Command, 0)
		for _, cmd := range c.allCommands {
			target := strings.ToLower(cmd.Title + " " + cmd.Description + " " + cmd.Category)
			match := true
			for _, w := range words {
				if !strings.Contains(target, w) {
					match = false
					break
				}
			}
			if match {
				c.filtered = append(c.filtered, cmd)
			}
		}
	}
	c.selectedIdx = 0
}

func (c *commandDialogCmp) View() string {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()

	maxWidth := 50

	for _, cmd := range c.filtered {
		w := len(cmd.Title) + 4
		if w > maxWidth {
			maxWidth = w
		}
		if cmd.Description != "" {
			w = len(cmd.Description) + 4
			if w > maxWidth {
				maxWidth = w
			}
		}
	}
	if maxWidth > 60 {
		maxWidth = 60
	}

	// Search input
	c.searchInput.Width = maxWidth - 4
	searchBox := baseStyle.
		Width(maxWidth).
		Padding(0, 1).
		Render(c.searchInput.View())

	// Build list with category headers
	maxVisible := c.maxVisible
	if maxVisible > len(c.filtered) {
		maxVisible = len(c.filtered)
	}

	// Calculate scroll window
	startIdx := 0
	if len(c.filtered) > maxVisible {
		halfVisible := maxVisible / 2
		if c.selectedIdx >= halfVisible && c.selectedIdx < len(c.filtered)-halfVisible {
			startIdx = c.selectedIdx - halfVisible
		} else if c.selectedIdx >= len(c.filtered)-halfVisible {
			startIdx = len(c.filtered) - maxVisible
		}
	}
	endIdx := startIdx + maxVisible
	if endIdx > len(c.filtered) {
		endIdx = len(c.filtered)
	}

	listItems := make([]string, 0)
	lastCategory := ""
	for i := startIdx; i < endIdx; i++ {
		cmd := c.filtered[i]
		// Category header
		if cmd.Category != "" && cmd.Category != lastCategory {
			lastCategory = cmd.Category
			header := baseStyle.
				Width(maxWidth).
				Foreground(t.TextMuted()).
				Bold(true).
				Padding(0, 1).
				Render("── " + cmd.Category + " ──")
			listItems = append(listItems, header)
		}
		listItems = append(listItems, cmd.Render(i == c.selectedIdx, maxWidth))
	}

	listContent := ""
	if len(c.filtered) == 0 {
		listContent = baseStyle.
			Width(maxWidth).
			Foreground(t.TextMuted()).
			Padding(0, 1).
			Render("No matching commands")
	} else {
		listContent = lipgloss.JoinVertical(lipgloss.Left, listItems...)
	}

	// Count display
	countText := ""
	if c.searchInput.Value() != "" {
		countText = baseStyle.
			Width(maxWidth).
			Foreground(t.TextMuted()).
			Padding(0, 1).
			Render(strings.Repeat(" ", 0) + string(rune('0'+len(c.filtered)/100%10)) + string(rune('0'+len(c.filtered)/10%10)) + string(rune('0'+len(c.filtered)%10)))
	}
	_ = countText

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		searchBox,
		baseStyle.Width(maxWidth).Render(""),
		listContent,
		baseStyle.Width(maxWidth).Render(""),
	)

	return baseStyle.Padding(1, 2).
		Border(lipgloss.RoundedBorder()).
		BorderBackground(t.Background()).
		BorderForeground(t.TextMuted()).
		Width(lipgloss.Width(content) + 4).
		Render(content)
}

func (c *commandDialogCmp) BindingKeys() []key.Binding {
	return layout.KeyMapToSlice(commandKeys)
}

func (c *commandDialogCmp) SetCommands(commands []Command) {
	c.allCommands = commands
	c.searchInput.SetValue("")
	c.applyFilter()
}

// NewCommandDialogCmp creates a new command selection dialog
func NewCommandDialogCmp() CommandDialog {
	ti := textinput.New()
	ti.Placeholder = "Type to search..."
	ti.Focus()
	ti.CharLimit = 50

	t := theme.CurrentTheme()
	ti.PromptStyle = lipgloss.NewStyle().Foreground(t.Primary())
	ti.TextStyle = lipgloss.NewStyle().Foreground(t.Text())
	ti.Prompt = "> "

	return &commandDialogCmp{
		searchInput: ti,
		maxVisible:  12,
	}
}
