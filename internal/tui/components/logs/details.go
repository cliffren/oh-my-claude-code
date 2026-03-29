package logs

import (
	"fmt"
	"strings"
	"time"

	"github.com/cliffren/toc/internal/logging"
	"github.com/cliffren/toc/internal/tui/layout"
	selectionpkg "github.com/cliffren/toc/internal/tui/selection"
	"github.com/cliffren/toc/internal/tui/styles"
	"github.com/cliffren/toc/internal/tui/theme"
	"github.com/cliffren/toc/internal/tui/util"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

type DetailComponent interface {
	tea.Model
	layout.Sizeable
	layout.Bindings
}

type detailCmp struct {
	width, height int
	currentLog    logging.LogMessage
	viewport      viewport.Model
	selection     selectionController
	clipboard     selectionpkg.ClipboardWriter
}

func (i *detailCmp) Init() tea.Cmd {
	messages := logging.List()
	if len(messages) == 0 {
		return nil
	}
	i.currentLog = messages[0]
	return nil
}

func (i *detailCmp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.MouseMsg:
		if msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown || msg.Button == tea.MouseButtonWheelLeft || msg.Button == tea.MouseButtonWheelRight {
			var cmd tea.Cmd
			i.viewport, cmd = i.viewport.Update(msg)
			return i, cmd
		}

		_, _, _, err := i.selection.HandleMouse(msg, i.selectionRegion(), i.visiblePlainLines(), i.clipboard)
		if err != nil {
			return i, util.ReportError(err)
		}
		if i.selection.CapturesMouse() || msg.Action == tea.MouseActionRelease {
			return i, nil
		}
		var cmd tea.Cmd
		i.viewport, cmd = i.viewport.Update(msg)
		return i, cmd
	case selectedLogMsg:
		if msg.ID != i.currentLog.ID {
			i.currentLog = logging.LogMessage(msg)
			i.updateContent()
		}
	}

	return i, nil
}

func (i *detailCmp) updateContent() {
	var content strings.Builder
	t := theme.CurrentTheme()

	// Format the header with timestamp and level
	timeStyle := lipgloss.NewStyle().Foreground(t.TextMuted())
	levelStyle := getLevelStyle(i.currentLog.Level)

	header := lipgloss.JoinHorizontal(
		lipgloss.Center,
		timeStyle.Render(i.currentLog.Time.Format(time.RFC3339)),
		"  ",
		levelStyle.Render(i.currentLog.Level),
	)

	content.WriteString(lipgloss.NewStyle().Bold(true).Render(header))
	content.WriteString("\n\n")

	// Message with styling
	messageStyle := lipgloss.NewStyle().Bold(true).Foreground(t.Text())
	content.WriteString(messageStyle.Render("Message:"))
	content.WriteString("\n")
	content.WriteString(lipgloss.NewStyle().Padding(0, 2).Render(i.currentLog.Message))
	content.WriteString("\n\n")

	// Attributes section
	if len(i.currentLog.Attributes) > 0 {
		attrHeaderStyle := lipgloss.NewStyle().Bold(true).Foreground(t.Text())
		content.WriteString(attrHeaderStyle.Render("Attributes:"))
		content.WriteString("\n")

		// Create a table-like display for attributes
		keyStyle := lipgloss.NewStyle().Foreground(t.Primary()).Bold(true)
		valueStyle := lipgloss.NewStyle().Foreground(t.Text())

		for _, attr := range i.currentLog.Attributes {
			attrLine := fmt.Sprintf("%s: %s",
				keyStyle.Render(attr.Key),
				valueStyle.Render(attr.Value),
			)
			content.WriteString(lipgloss.NewStyle().Padding(0, 2).Render(attrLine))
			content.WriteString("\n")
		}
	}

	i.viewport.SetContent(content.String())
}

func getLevelStyle(level string) lipgloss.Style {
	style := lipgloss.NewStyle().Bold(true)
	t := theme.CurrentTheme()

	switch strings.ToLower(level) {
	case "info":
		return style.Foreground(t.Info())
	case "warn", "warning":
		return style.Foreground(t.Warning())
	case "error", "err":
		return style.Foreground(t.Error())
	case "debug":
		return style.Foreground(t.Success())
	default:
		return style.Foreground(t.Text())
	}
}

func (i *detailCmp) View() string {
	t := theme.CurrentTheme()
	visible := strings.Split(i.viewport.View(), "\n")
	if i.selection.HasSelection() {
		start, end := i.selection.Bounds()
		visible = selectionpkg.HighlightLines(visible, start, end, lipgloss.NewStyle().Background(t.BackgroundSecondary()).Foreground(t.Text()))
	}
	return styles.ForceReplaceBackgroundWithLipgloss(strings.Join(visible, "\n"), t.Background())
}

func (i *detailCmp) GetSize() (int, int) {
	return i.width, i.height
}

func (i *detailCmp) SetSize(width int, height int) tea.Cmd {
	i.width = width
	i.height = height
	i.viewport.Width = i.width
	i.viewport.Height = i.height
	i.updateContent()
	return nil
}

func (i *detailCmp) BindingKeys() []key.Binding {
	return layout.KeyMapToSlice(i.viewport.KeyMap)
}

func (i *detailCmp) CapturesMouse() bool {
	return i.selection.CapturesMouse()
}

func NewLogsDetails() DetailComponent {
	return &detailCmp{
		viewport:  viewport.New(0, 0),
		clipboard: selectionpkg.NewClipboardWriter(),
	}
}

func (i *detailCmp) selectionRegion() selectionpkg.Region {
	return selectionpkg.Region{X: 0, Y: 0, Width: i.viewport.Width, Height: i.viewport.Height}
}

func (i *detailCmp) visiblePlainLines() []string {
	visible := strings.Split(i.viewport.View(), "\n")
	lines := make([]string, len(visible))
	for idx, line := range visible {
		lines[idx] = ansi.Strip(line)
	}
	return lines
}
