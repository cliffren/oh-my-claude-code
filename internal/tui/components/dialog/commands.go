package dialog

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/cliffren/oh-my-claude-code/internal/tui/layout"
	"github.com/cliffren/oh-my-claude-code/internal/tui/styles"
	"github.com/cliffren/oh-my-claude-code/internal/tui/theme"
	"github.com/cliffren/oh-my-claude-code/internal/tui/util"
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
	GetSelected() (Command, bool)
	SetMaxVisible(n int)
}

type commandDialogCmp struct {
	searchInput  textinput.Model
	allCommands  []Command
	filtered     []Command
	selectedIdx  int
	maxVisible   int
	linesPerItem int // 1 = no descriptions, 2 = with descriptions (default)
	fixedWidth   int // computed once in SetCommands, stays constant
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
	case tea.MouseMsg:
		// Ignore all mouse events — prevents scroll escape sequences
		// from being interpreted as text input.
		return c, nil
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

	maxWidth := c.fixedWidth
	if maxWidth == 0 {
		maxWidth = 50
	}

	// Search input
	c.searchInput.Width = maxWidth - 4
	searchBox := baseStyle.
		Width(maxWidth).
		Padding(0, 1).
		Render(c.searchInput.View())

	// Build list with category headers — use fixed maxVisible, never shrink.
	maxVisible := c.maxVisible

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

	var innerContent string
	if len(c.filtered) == 0 {
		innerContent = baseStyle.
			Width(maxWidth).
			Foreground(t.TextMuted()).
			Padding(0, 1).
			Render("No matching commands")
	} else {
		innerContent = lipgloss.JoinVertical(lipgloss.Left, listItems...)
	}

	// Fixed-height list area: linesPerItem lines per item (1 = no desc, 2 = with desc).
	// This keeps the dialog height constant regardless of search results.
	fixedListHeight := maxVisible * c.linesPerItem
	listContent := baseStyle.Width(maxWidth).Height(fixedListHeight).Render(innerContent)

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

	// Fixed outer dimensions so the dialog never resizes during search/scroll.
	fixedOuterWidth := maxWidth + 8  // content + padding(2*2) + border(2)
	fixedOuterHeight := fixedListHeight + 7 // searchBox(1) + sep(1) + list + sep(1) + padding(2) + border(2)

	return baseStyle.Padding(1, 2).
		Border(lipgloss.RoundedBorder()).
		BorderBackground(t.Background()).
		BorderForeground(t.TextMuted()).
		Width(fixedOuterWidth).
		Height(fixedOuterHeight).
		Render(content)
}

func (c *commandDialogCmp) BindingKeys() []key.Binding {
	return layout.KeyMapToSlice(commandKeys)
}

func (c *commandDialogCmp) SetMaxVisible(n int) {
	if n > 0 {
		c.maxVisible = n
	}
}

func (c *commandDialogCmp) GetSelected() (Command, bool) {
	if c.selectedIdx >= 0 && c.selectedIdx < len(c.filtered) {
		return c.filtered[c.selectedIdx], true
	}
	return Command{}, false
}

func (c *commandDialogCmp) SetCommands(commands []Command) {
	c.allCommands = commands
	c.searchInput.SetValue("")

	// Compute fixed width from ALL commands (not filtered) so it never changes.
	w := 50
	for _, cmd := range commands {
		if tw := len(cmd.Title) + 4; tw > w {
			w = tw
		}
		if cmd.Description != "" {
			if dw := len(cmd.Description) + 4; dw > w {
				w = dw
			}
		}
	}
	if w > 60 {
		w = 60
	}
	c.fixedWidth = w

	c.applyFilter()
}

// NewCommandDialogCmp creates a new command selection dialog.
// opts[0] = maxVisible (default 12), opts[1] = linesPerItem (default 2).
func NewCommandDialogCmp(opts ...int) CommandDialog {
	ti := textinput.New()
	ti.Placeholder = "Type to search..."
	ti.Focus()
	ti.CharLimit = 50

	t := theme.CurrentTheme()
	ti.PromptStyle = lipgloss.NewStyle().Foreground(t.Primary())
	ti.TextStyle = lipgloss.NewStyle().Foreground(t.Text())
	ti.Prompt = "> "

	mv := 12
	lpi := 2
	if len(opts) > 0 {
		mv = opts[0]
	}
	if len(opts) > 1 {
		lpi = opts[1]
	}
	return &commandDialogCmp{
		searchInput:  ti,
		maxVisible:   mv,
		linesPerItem: lpi,
	}
}
