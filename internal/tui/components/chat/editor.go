package chat

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"unicode"

	"github.com/cliffren/toc/internal/app"
	"github.com/cliffren/toc/internal/logging"
	"github.com/cliffren/toc/internal/message"
	"github.com/cliffren/toc/internal/session"
	"github.com/cliffren/toc/internal/tui/components/dialog"
	"github.com/cliffren/toc/internal/tui/layout"
	"github.com/cliffren/toc/internal/tui/styles"
	"github.com/cliffren/toc/internal/tui/theme"
	"github.com/cliffren/toc/internal/tui/util"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type editorCmp struct {
	width        int
	height       int
	app          *app.App
	session      session.Session
	textarea     textarea.Model
	attachments  []message.Attachment
	deleteMode   bool
	inputHistory []string
	historyIdx   int    // -1 = not browsing history
	historyDraft string // stash current input when entering history mode
}

type EditorKeyMaps struct {
	Send       key.Binding
	OpenEditor key.Binding
}

type bluredEditorKeyMaps struct {
	Send       key.Binding
	Focus      key.Binding
	OpenEditor key.Binding
}
type DeleteAttachmentKeyMaps struct {
	AttachmentDeleteMode key.Binding
	Escape               key.Binding
	DeleteAllAttachments key.Binding
}

var editorMaps = EditorKeyMaps{
	Send: key.NewBinding(
		key.WithKeys("enter", "ctrl+s"),
		key.WithHelp("enter", "send message"),
	),
	OpenEditor: key.NewBinding(
		key.WithKeys("ctrl+e"),
		key.WithHelp("ctrl+e", "open editor"),
	),
}

var DeleteKeyMaps = DeleteAttachmentKeyMaps{
	AttachmentDeleteMode: key.NewBinding(
		key.WithKeys("ctrl+r"),
		key.WithHelp("ctrl+r+{i}", "delete attachment at index i"),
	),
	Escape: key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "cancel delete mode"),
	),
	DeleteAllAttachments: key.NewBinding(
		key.WithKeys("r"),
		key.WithHelp("ctrl+r+r", "delete all attchments"),
	),
}

const (
	maxAttachments = 5
)

func (m *editorCmp) openEditor() tea.Cmd {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "nvim"
	}

	tmpfile, err := os.CreateTemp("", "msg_*.md")
	if err != nil {
		return util.ReportError(err)
	}
	tmpfile.Close()
	c := exec.Command(editor, tmpfile.Name()) //nolint:gosec
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return tea.ExecProcess(c, func(err error) tea.Msg {
		if err != nil {
			return util.ReportError(err)
		}
		content, err := os.ReadFile(tmpfile.Name())
		if err != nil {
			return util.ReportError(err)
		}
		if len(content) == 0 {
			return util.ReportWarn("Message is empty")
		}
		os.Remove(tmpfile.Name())
		attachments := m.attachments
		m.attachments = nil
		return SendMsg{
			Text:        string(content),
			Attachments: attachments,
		}
	})
}

func (m *editorCmp) Init() tea.Cmd {
	return textarea.Blink
}

func (m *editorCmp) send() tea.Cmd {
	if m.app.CoderAgent.IsSessionBusy(m.session.ID) {
		return util.ReportWarn("Agent is working, please wait...")
	}

	value := m.textarea.Value()
	m.textarea.Reset()
	attachments := m.attachments

	m.attachments = nil
	if value == "" {
		return nil
	}

	// Record in input history
	m.inputHistory = append(m.inputHistory, value)
	m.historyIdx = -1
	m.historyDraft = ""

	return tea.Batch(
		util.CmdHandler(SendMsg{
			Text:        value,
			Attachments: attachments,
		}),
	)
}

func (m *editorCmp) loadSessionHistory() {
	m.inputHistory = nil
	m.historyIdx = -1
	m.historyDraft = ""

	if m.session.ID == "" {
		return
	}

	msgs, err := m.app.Messages.List(context.Background(), m.session.ID)
	if err != nil {
		return
	}
	for _, msg := range msgs {
		if msg.Role == message.User {
			if text := msg.Content().String(); text != "" {
				m.inputHistory = append(m.inputHistory, text)
			}
		}
	}
}

func (m *editorCmp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.MouseMsg:
		// On left click, temporarily disable TUI mouse so the terminal can
		// handle native text selection in the editor. Other mouse events are
		// discarded — the editor doesn't use scroll.
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			return m, util.CmdHandler(util.DisableMouseForSelectionMsg{})
		}
		return m, nil
	case dialog.ThemeChangedMsg:
		m.textarea = CreateTextArea(&m.textarea)
	case dialog.CompletionSelectedMsg:
		existingValue := m.textarea.Value()
		modifiedValue := strings.Replace(existingValue, msg.SearchString, msg.CompletionValue, 1)

		m.textarea.SetValue(modifiedValue)
		return m, nil
	case InsertEditorTextMsg:
		m.textarea.SetValue(msg.Text)
		m.textarea.CursorEnd()
		return m, nil
	case SessionSelectedMsg:
		if msg.ID != m.session.ID {
			m.session = msg
			m.loadSessionHistory()
		}
		return m, nil
	case dialog.AttachmentAddedMsg:
		if len(m.attachments) >= maxAttachments {
			logging.ErrorPersist(fmt.Sprintf("cannot add more than %d images", maxAttachments))
			return m, cmd
		}
		m.attachments = append(m.attachments, msg.Attachment)
		m.updateTextareaHeight()
	case tea.KeyMsg:
		if isMouseProtocolArtifact(msg) {
			return m, nil
		}
		if key.Matches(msg, DeleteKeyMaps.AttachmentDeleteMode) {
			m.deleteMode = true
			return m, nil
		}
		if key.Matches(msg, DeleteKeyMaps.DeleteAllAttachments) && m.deleteMode {
			m.deleteMode = false
			m.attachments = nil
			m.updateTextareaHeight()
			return m, nil
		}
		if m.deleteMode && len(msg.Runes) > 0 && unicode.IsDigit(msg.Runes[0]) {
			num := int(msg.Runes[0] - '0')
			m.deleteMode = false
			if num < 10 && len(m.attachments) > num {
				if num == 0 {
					m.attachments = m.attachments[num+1:]
				} else {
					m.attachments = slices.Delete(m.attachments, num, num+1)
				}
				m.updateTextareaHeight()
				return m, nil
			}
		}
		if key.Matches(msg, messageKeys.PageUp) || key.Matches(msg, messageKeys.PageDown) ||
			key.Matches(msg, messageKeys.HalfPageUp) || key.Matches(msg, messageKeys.HalfPageDown) {
			return m, nil
		}
		if key.Matches(msg, editorMaps.OpenEditor) {
			if m.app.CoderAgent.IsSessionBusy(m.session.ID) {
				return m, util.ReportWarn("Agent is working, please wait...")
			}
			return m, m.openEditor()
		}
		if key.Matches(msg, DeleteKeyMaps.Escape) {
			m.deleteMode = false
			return m, nil
		}
		// "/" opens command palette (empty editor or just typing first char)
		if m.textarea.Focused() && msg.String() == "/" && (m.textarea.Value() == "" || m.textarea.Value() == "/") {
			m.textarea.SetValue("")
			return m, util.CmdHandler(ShowSlashCompletionMsg{})
		}
		// Handle Up/Down for input history
		if m.textarea.Focused() && msg.String() == "up" && len(m.inputHistory) > 0 {
			if m.historyIdx == -1 {
				// Entering history mode: stash current draft
				m.historyDraft = m.textarea.Value()
				m.historyIdx = len(m.inputHistory) - 1
			} else if m.historyIdx > 0 {
				m.historyIdx--
			}
			m.textarea.SetValue(m.inputHistory[m.historyIdx])
			m.textarea.CursorEnd()
			return m, nil
		}
		if m.textarea.Focused() && msg.String() == "down" && m.historyIdx >= 0 {
			if m.historyIdx < len(m.inputHistory)-1 {
				m.historyIdx++
				m.textarea.SetValue(m.inputHistory[m.historyIdx])
			} else {
				// Back to draft
				m.historyIdx = -1
				m.textarea.SetValue(m.historyDraft)
				m.historyDraft = ""
			}
			m.textarea.CursorEnd()
			return m, nil
		}
		// Handle Enter key
		if m.textarea.Focused() && key.Matches(msg, editorMaps.Send) {
			value := m.textarea.Value()
			if len(value) > 0 && value[len(value)-1] == '\\' {
				// If the last character is a backslash, remove it and add a newline
				m.textarea.SetValue(value[:len(value)-1] + "\n")
				return m, nil
			} else {
				// Otherwise, send the message
				return m, m.send()
			}
		}

	}
	m.textarea, cmd = m.textarea.Update(msg)
	return m, cmd
}

func (m *editorCmp) View() string {
	t := theme.CurrentTheme()

	// Style the prompt with theme colors
	style := lipgloss.NewStyle().
		Padding(0, 0, 0, 1).
		Bold(true).
		Foreground(t.Primary())

	if len(m.attachments) == 0 {
		return lipgloss.JoinHorizontal(lipgloss.Top, style.Render(">"), m.textarea.View())
	}
	return lipgloss.JoinVertical(lipgloss.Top,
		m.attachmentsContent(),
		lipgloss.JoinHorizontal(lipgloss.Top, style.Render(">"),
			m.textarea.View()),
	)
}

func (m *editorCmp) updateTextareaHeight() {
	if len(m.attachments) > 0 {
		m.textarea.SetHeight(m.height - 1)
	} else {
		m.textarea.SetHeight(m.height)
	}
}

func (m *editorCmp) SetSize(width, height int) tea.Cmd {
	m.width = width
	m.height = height
	m.textarea.SetWidth(width - 3) // account for prompt ">" and left padding
	m.updateTextareaHeight()
	return nil
}

func (m *editorCmp) GetSize() (int, int) {
	return m.textarea.Width(), m.textarea.Height()
}

func (m *editorCmp) attachmentsContent() string {
	var styledAttachments []string
	t := theme.CurrentTheme()
	attachmentStyles := styles.BaseStyle().
		MarginLeft(1).
		Background(t.TextMuted()).
		Foreground(t.Text())
	for i, attachment := range m.attachments {
		var filename string
		if len(attachment.FileName) > 10 {
			filename = fmt.Sprintf(" %s %s...", styles.DocumentIcon, attachment.FileName[0:7])
		} else {
			filename = fmt.Sprintf(" %s %s", styles.DocumentIcon, attachment.FileName)
		}
		if m.deleteMode {
			filename = fmt.Sprintf("%d%s", i, filename)
		}
		styledAttachments = append(styledAttachments, attachmentStyles.Render(filename))
	}
	content := lipgloss.JoinHorizontal(lipgloss.Left, styledAttachments...)
	return content
}

func (m *editorCmp) BindingKeys() []key.Binding {
	bindings := []key.Binding{}
	bindings = append(bindings, layout.KeyMapToSlice(editorMaps)...)
	bindings = append(bindings, layout.KeyMapToSlice(DeleteKeyMaps)...)
	return bindings
}

func CreateTextArea(existing *textarea.Model) textarea.Model {
	t := theme.CurrentTheme()
	bgColor := t.Background()
	textColor := t.Text()
	textMutedColor := t.TextMuted()

	ta := textarea.New()
	ta.BlurredStyle.Base = styles.BaseStyle().Background(bgColor).Foreground(textColor)
	ta.BlurredStyle.CursorLine = styles.BaseStyle().Background(bgColor)
	ta.BlurredStyle.Placeholder = styles.BaseStyle().Background(bgColor).Foreground(textMutedColor)
	ta.BlurredStyle.Text = styles.BaseStyle().Background(bgColor).Foreground(textColor)
	ta.FocusedStyle.Base = styles.BaseStyle().Background(bgColor).Foreground(textColor)
	ta.FocusedStyle.CursorLine = styles.BaseStyle().Background(bgColor)
	ta.FocusedStyle.Placeholder = styles.BaseStyle().Background(bgColor).Foreground(textMutedColor)
	ta.FocusedStyle.Text = styles.BaseStyle().Background(bgColor).Foreground(textColor)

	ta.Prompt = " "
	ta.ShowLineNumbers = false
	ta.CharLimit = -1

	if existing != nil {
		ta.SetValue(existing.Value())
		ta.SetWidth(existing.Width())
		ta.SetHeight(existing.Height())
	}

	ta.Focus()
	return ta
}

// EditorCmp is the public interface for the editor component.
type EditorCmp interface {
	tea.Model
	layout.Sizeable
	layout.Bindings
	// CursorPos returns the visible (row, col) of the text cursor within the
	// textarea content area (both 0-indexed). col is unicode-width-aware.
	CursorPos() (row, col int)
}

func (m *editorCmp) CursorPos() (row, col int) {
	li := m.textarea.LineInfo()
	return m.textarea.Line() + li.RowOffset, li.CharOffset
}

func NewEditorCmp(app *app.App) EditorCmp {
	ta := CreateTextArea(nil)
	return &editorCmp{
		app:        app,
		textarea:   ta,
		historyIdx: -1,
	}
}

func isMouseProtocolArtifact(msg tea.KeyMsg) bool {
	rawCandidates := []string{string(msg.Runes), msg.String()}
	for _, raw := range rawCandidates {
		if raw == "" {
			continue
		}
		if containsMouseProtocolArtifact(raw) {
			return true
		}
	}
	return false
}

// containsMouseProtocolArtifact detects SGR mouse escape sequences that leak
// through as KeyMsg. Handles full sequences ([<65;42;29m), concatenated
// sequences from rapid scrolling, and partial fragments.
func containsMouseProtocolArtifact(raw string) bool {
	raw = strings.TrimPrefix(raw, "\x1b")

	// Check for full/concatenated SGR mouse sequences: [<N;N;N(m|M)
	s := raw
	for {
		idx := strings.Index(s, "[<")
		if idx < 0 {
			break
		}
		s = s[idx+2:]
		if matchSGRBody(s) {
			return true
		}
	}

	// Check for partial fragments that result from split parsing.
	// These are the remaining chars after ESC is consumed separately:
	// e.g. "<65;42;29m" or "65;42;29m" or just ";42;29m"
	if len(raw) >= 3 {
		// Starts with < followed by digits and semicolons
		if raw[0] == '<' && matchSGRBody(raw[1:]) {
			return true
		}
		// Just digits;digits;digits(m|M) — the [< was consumed earlier
		if matchSGRBody(raw) {
			return true
		}
	}

	return false
}

func matchSGRBody(s string) bool {
	i := 0
	semicolons := 0
	hasDigit := false
	for i < len(s) {
		ch := s[i]
		if ch >= '0' && ch <= '9' {
			hasDigit = true
			i++
		} else if ch == ';' {
			semicolons++
			i++
		} else if (ch == 'm' || ch == 'M') && semicolons == 2 && hasDigit {
			return true
		} else {
			return false
		}
	}
	return false
}
