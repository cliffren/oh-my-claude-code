package chat

import (
	"fmt"
	"os/exec"
	// "sort" // LSP disabled
	"strings"

	"github.com/charmbracelet/lipgloss"
	// "github.com/charmbracelet/x/ansi" // LSP disabled
	"github.com/cliffren/toc/internal/config"
	"github.com/cliffren/toc/internal/message"
	"github.com/cliffren/toc/internal/session"
	"github.com/cliffren/toc/internal/tui/components/dialog"
	"github.com/cliffren/toc/internal/tui/styles"
	"github.com/cliffren/toc/internal/tui/theme"
	"github.com/cliffren/toc/internal/version"
)

type SendMsg struct {
	Text        string
	Attachments []message.Attachment
}

type SessionSelectedMsg = session.Session

type SessionClearedMsg struct{}

// ShowSlashCompletionMsg is emitted when "/" is typed into an empty editor.
type ShowSlashCompletionMsg struct{}

// ShowSlashMenuMsg asks the chat page to display the inline slash command menu.
type ShowSlashMenuMsg struct {
	Commands []dialog.Command
}

// InsertEditorTextMsg asks the editor to insert text (e.g. "/compact ") at the cursor.
type InsertEditorTextMsg struct {
	Text string
}

type InternalSlashCommandMsg struct {
	Command string
}

type EditorFocusMsg bool

func header(width int) string {
	return lipgloss.JoinVertical(
		lipgloss.Top,
		logo(width),
		repo(width),
		"",
		cwd(width),
	)
}

// LSP disabled — uncomment to re-enable lspsConfigured display
// func lspsConfigured(width int) string {
// 	cfg := config.Get()
// 	title := "LSP Configuration"
// 	title = ansi.Truncate(title, width, "…")
// 	t := theme.CurrentTheme()
// 	baseStyle := styles.BaseStyle()
// 	lsps := baseStyle.Width(width).Foreground(t.Primary()).Bold(true).Render(title)
// 	var lspNames []string
// 	for name := range cfg.LSP { lspNames = append(lspNames, name) }
// 	sort.Strings(lspNames)
// 	var lspViews []string
// 	for _, name := range lspNames {
// 		lsp := cfg.LSP[name]
// 		lspName := baseStyle.Foreground(t.Text()).Render(fmt.Sprintf("• %s", name))
// 		cmd := ansi.Truncate(lsp.Command, width-lipgloss.Width(lspName)-3, "…")
// 		lspPath := baseStyle.Foreground(t.TextMuted()).Render(fmt.Sprintf(" (%s)", cmd))
// 		lspViews = append(lspViews, baseStyle.Width(width).Render(lipgloss.JoinHorizontal(lipgloss.Left, lspName, lspPath)))
// 	}
// 	return baseStyle.Width(width).Render(lipgloss.JoinVertical(lipgloss.Left, lsps, lipgloss.JoinVertical(lipgloss.Left, lspViews...)))
// }

func logo(width int) string {
	logo := fmt.Sprintf("%s %s", styles.OpenCodeIcon, "toc")
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()

	versionText := baseStyle.
		Foreground(t.TextMuted()).
		Render(version.Version)

	return baseStyle.
		Bold(true).
		Width(width).
		Render(
			lipgloss.JoinHorizontal(
				lipgloss.Left,
				logo,
				" ",
				versionText,
			),
		)
}

func repo(width int) string {
	repo := "https://github.com/cliffren/toc"
	t := theme.CurrentTheme()

	return styles.BaseStyle().
		Foreground(t.TextMuted()).
		Width(width).
		Render(repo)
}

var cachedCWDLabel string

func cwd(width int) string {
	if cachedCWDLabel == "" {
		wd := config.WorkingDirectory()
		cachedCWDLabel = fmt.Sprintf("cwd: %s", wd)
		cmd := exec.Command("git", "-C", wd, "rev-parse", "--abbrev-ref", "HEAD")
		if out, err := cmd.Output(); err == nil {
			if branch := strings.TrimSpace(string(out)); branch != "" {
				cachedCWDLabel += fmt.Sprintf(" (%s)", branch)
			}
		}
	}
	t := theme.CurrentTheme()
	return styles.BaseStyle().
		Foreground(t.TextMuted()).
		Width(width).
		Render(cachedCWDLabel)
}

