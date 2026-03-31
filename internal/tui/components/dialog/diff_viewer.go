package dialog

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/cliffren/toc/internal/config"
	"github.com/cliffren/toc/internal/diff"
	"github.com/cliffren/toc/internal/tui/layout"
	"github.com/cliffren/toc/internal/tui/styles"
	"github.com/cliffren/toc/internal/tui/theme"
	"github.com/cliffren/toc/internal/tui/util"
)

type ShowDiffMsg struct {
	FilePath string
}

type CloseDiffViewerMsg struct{}

type diffLoadedMsg struct {
	content string
}

type DiffViewer interface {
	tea.Model
	layout.Bindings
	SetDiff(filePath string) tea.Cmd
}

type diffViewerCmp struct {
	filePath string
	viewport viewport.Model
	width    int
	height   int
}

var diffViewerKeys = struct {
	Up     key.Binding
	Down   key.Binding
	Escape key.Binding
	J      key.Binding
	K      key.Binding
}{
	Up:     key.NewBinding(key.WithKeys("up"), key.WithHelp("↑", "scroll up")),
	Down:   key.NewBinding(key.WithKeys("down"), key.WithHelp("↓", "scroll down")),
	Escape: key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "close")),
	J:      key.NewBinding(key.WithKeys("j"), key.WithHelp("j", "scroll down")),
	K:      key.NewBinding(key.WithKeys("k"), key.WithHelp("k", "scroll up")),
}

func (d *diffViewerCmp) Init() tea.Cmd { return nil }

func (d *diffViewerCmp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case diffLoadedMsg:
		d.viewport.SetContent(msg.content)
		d.viewport.GotoTop()
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, diffViewerKeys.Escape):
			return d, util.CmdHandler(CloseDiffViewerMsg{})
		case key.Matches(msg, diffViewerKeys.Up) || key.Matches(msg, diffViewerKeys.K):
			d.viewport.LineUp(1)
		case key.Matches(msg, diffViewerKeys.Down) || key.Matches(msg, diffViewerKeys.J):
			d.viewport.LineDown(1)
		}
	case tea.WindowSizeMsg:
		d.resize(msg.Width, msg.Height)
	}
	return d, nil
}

func (d *diffViewerCmp) View() string {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()

	title := baseStyle.Bold(true).Width(d.width - 4).Foreground(t.Primary()).
		Render(fmt.Sprintf("Diff: %s", d.filePath))

	hint := baseStyle.Foreground(t.TextMuted()).Width(d.width - 4).
		Render("esc close  j/k scroll")

	contentStyle := lipgloss.NewStyle().Background(t.Background())
	content := contentStyle.Render(d.viewport.View())

	body := lipgloss.JoinVertical(lipgloss.Top,
		title,
		baseStyle.Render(strings.Repeat(" ", d.width-4)),
		content,
		hint,
	)

	return baseStyle.
		Padding(1, 0, 0, 1).
		Border(lipgloss.RoundedBorder()).
		BorderBackground(t.Background()).
		BorderForeground(t.TextMuted()).
		Width(d.width).Height(d.height).
		Render(body)
}

func (d *diffViewerCmp) BindingKeys() []key.Binding {
	return layout.KeyMapToSlice(diffViewerKeys)
}

func (d *diffViewerCmp) resize(w, h int) {
	d.width = min(int(float64(w)*0.8), 160)
	d.height = min(int(float64(h)*0.8), h-4)
	d.viewport.Width = d.width - 4
	d.viewport.Height = max(d.height-6, 3)
}

func (d *diffViewerCmp) SetDiff(filePath string) tea.Cmd {
	d.filePath = filePath
	d.viewport.SetContent("Loading diff...")
	vpWidth := d.viewport.Width
	return func() tea.Msg {
		workingDir := config.WorkingDirectory()
		cmd := exec.Command("git", "diff", filePath)
		cmd.Dir = workingDir
		out, err := cmd.Output()
		if err != nil || len(out) == 0 {
			return diffLoadedMsg{content: "No diff available"}
		}
		formatted, fmtErr := diff.FormatDiff(string(out), diff.WithTotalWidth(vpWidth))
		if fmtErr != nil {
			return diffLoadedMsg{content: string(out)}
		}
		return diffLoadedMsg{content: formatted}
	}
}

func NewDiffViewer() DiffViewer {
	return &diffViewerCmp{}
}
