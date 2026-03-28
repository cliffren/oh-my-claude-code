package dialog

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/Krontx/oh-my-claude-code/internal/session"
	"github.com/Krontx/oh-my-claude-code/internal/tui/layout"
	"github.com/Krontx/oh-my-claude-code/internal/tui/styles"
	"github.com/Krontx/oh-my-claude-code/internal/tui/theme"
	"github.com/Krontx/oh-my-claude-code/internal/tui/util"
)

// SessionSelectedMsg is sent when a session is selected
type SessionSelectedMsg struct {
	Session session.Session
}

// CloseSessionDialogMsg is sent when the session dialog is closed
type CloseSessionDialogMsg struct{}

// SessionDeleteMsg is sent when the user confirms deleting a session
type SessionDeleteMsg struct {
	SessionID string
}

// SessionRenameMsg is sent when the user submits a new session title
type SessionRenameMsg struct {
	SessionID string
	NewTitle  string
}

// SessionDialog interface for the session switching dialog
type SessionDialog interface {
	tea.Model
	layout.Bindings
	SetSessions(sessions []session.Session)
	SetSelectedSession(sessionID string)
}

type sessionMode int

const (
	sessionModeNavigate sessionMode = iota
	sessionModeConfirmDelete
	sessionModeRename
)

type sessionDialogCmp struct {
	allSessions       []session.Session
	filtered          []session.Session
	selectedIdx       int
	width             int
	height            int
	selectedSessionID string
	searchInput       textinput.Model
	renameInput       textinput.Model
	mode              sessionMode
}

type sessionKeyMap struct {
	Up     key.Binding
	Down   key.Binding
	Enter  key.Binding
	Escape key.Binding
	J      key.Binding
	K      key.Binding
	Delete key.Binding
	Rename key.Binding
}

var sessionKeys = sessionKeyMap{
	Up: key.NewBinding(
		key.WithKeys("up"),
		key.WithHelp("↑", "up"),
	),
	Down: key.NewBinding(
		key.WithKeys("down"),
		key.WithHelp("↓", "down"),
	),
	Enter: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "select"),
	),
	Escape: key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "close"),
	),
	J: key.NewBinding(
		key.WithKeys("j"),
		key.WithHelp("j", "down"),
	),
	K: key.NewBinding(
		key.WithKeys("k"),
		key.WithHelp("k", "up"),
	),
	Delete: key.NewBinding(
		key.WithKeys("ctrl+d"),
		key.WithHelp("ctrl+d", "delete"),
	),
	Rename: key.NewBinding(
		key.WithKeys("ctrl+r"),
		key.WithHelp("ctrl+r", "rename"),
	),
}

func (s *sessionDialogCmp) Init() tea.Cmd {
	return textinput.Blink
}

func (s *sessionDialogCmp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch s.mode {
		case sessionModeConfirmDelete:
			switch msg.String() {
			case "y", "Y":
				if len(s.filtered) > 0 {
					id := s.filtered[s.selectedIdx].ID
					s.mode = sessionModeNavigate
					return s, util.CmdHandler(SessionDeleteMsg{SessionID: id})
				}
			}
			// Any other key cancels
			s.mode = sessionModeNavigate
			return s, nil

		case sessionModeRename:
			switch msg.Type {
			case tea.KeyEnter:
				newTitle := strings.TrimSpace(s.renameInput.Value())
				if newTitle != "" && len(s.filtered) > 0 {
					id := s.filtered[s.selectedIdx].ID
					s.mode = sessionModeNavigate
					return s, util.CmdHandler(SessionRenameMsg{SessionID: id, NewTitle: newTitle})
				}
				s.mode = sessionModeNavigate
				return s, nil
			case tea.KeyEsc:
				s.mode = sessionModeNavigate
				return s, nil
			}
			var cmd tea.Cmd
			s.renameInput, cmd = s.renameInput.Update(msg)
			return s, cmd

		default: // sessionModeNavigate
			switch {
			case key.Matches(msg, sessionKeys.Up) || key.Matches(msg, sessionKeys.K):
				if s.selectedIdx > 0 {
					s.selectedIdx--
				}
				return s, nil
			case key.Matches(msg, sessionKeys.Down) || key.Matches(msg, sessionKeys.J):
				if s.selectedIdx < len(s.filtered)-1 {
					s.selectedIdx++
				}
				return s, nil
			case key.Matches(msg, sessionKeys.Enter):
				if len(s.filtered) > 0 {
					return s, util.CmdHandler(SessionSelectedMsg{Session: s.filtered[s.selectedIdx]})
				}
			case key.Matches(msg, sessionKeys.Escape):
				if s.searchInput.Value() != "" {
					s.searchInput.SetValue("")
					s.applyFilter()
					return s, nil
				}
				return s, util.CmdHandler(CloseSessionDialogMsg{})
			case key.Matches(msg, sessionKeys.Delete):
				if len(s.filtered) > 0 {
					s.mode = sessionModeConfirmDelete
					return s, nil
				}
			case key.Matches(msg, sessionKeys.Rename):
				if len(s.filtered) > 0 {
					s.renameInput.SetValue(s.filtered[s.selectedIdx].Title)
					s.renameInput.CursorEnd()
					s.mode = sessionModeRename
					return s, s.renameInput.Focus()
				}
			}
		}

	case tea.WindowSizeMsg:
		s.width = msg.Width
		s.height = msg.Height
	}

	if s.mode == sessionModeNavigate {
		prevValue := s.searchInput.Value()
		var cmd tea.Cmd
		s.searchInput, cmd = s.searchInput.Update(msg)
		if s.searchInput.Value() != prevValue {
			s.applyFilter()
		}
		return s, cmd
	}

	return s, nil
}

func (s *sessionDialogCmp) applyFilter() {
	query := strings.ToLower(strings.TrimSpace(s.searchInput.Value()))
	if query == "" {
		s.filtered = make([]session.Session, len(s.allSessions))
		copy(s.filtered, s.allSessions)
	} else {
		words := strings.Fields(query)
		s.filtered = s.filtered[:0]
		for _, sess := range s.allSessions {
			haystack := strings.ToLower(sess.Title)
			match := true
			for _, w := range words {
				if !strings.Contains(haystack, w) {
					match = false
					break
				}
			}
			if match {
				s.filtered = append(s.filtered, sess)
			}
		}
	}
	if s.selectedIdx >= len(s.filtered) {
		s.selectedIdx = max(0, len(s.filtered)-1)
	}
}

func (s *sessionDialogCmp) View() string {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()

	dialogWidth := 50
	if s.width > 0 {
		dialogWidth = min(60, max(40, s.width/2))
	}
	innerWidth := dialogWidth - 6 // border(2) + padding(4)

	// Search box
	s.searchInput.Width = innerWidth - 2
	searchBox := baseStyle.
		Border(lipgloss.NormalBorder()).
		BorderForeground(t.TextMuted()).
		Width(innerWidth).
		Render(s.searchInput.View())

	// Session list
	maxVisible := 10
	if s.height > 0 {
		maxVisible = min(10, max(3, s.height/4))
	}

	var listLines []string
	if len(s.filtered) == 0 {
		listLines = []string{
			baseStyle.Foreground(t.TextMuted()).Width(innerWidth).Padding(0, 1).Render("No sessions found"),
		}
	} else {
		startIdx := 0
		if len(s.filtered) > maxVisible {
			half := maxVisible / 2
			if s.selectedIdx >= half && s.selectedIdx < len(s.filtered)-half {
				startIdx = s.selectedIdx - half
			} else if s.selectedIdx >= len(s.filtered)-half {
				startIdx = len(s.filtered) - maxVisible
			}
		}
		endIdx := min(startIdx+maxVisible, len(s.filtered))

		for i := startIdx; i < endIdx; i++ {
			sess := s.filtered[i]
			itemStyle := baseStyle.Width(innerWidth).Padding(0, 1)
			if i == s.selectedIdx {
				itemStyle = itemStyle.
					Background(t.Primary()).
					Foreground(t.Background()).
					Bold(true)
			}
			prefix := "  "
			if sess.ID == s.selectedSessionID {
				prefix = "● "
			}
			listLines = append(listLines, itemStyle.Render(prefix+sess.Title))
		}
	}
	listView := lipgloss.JoinVertical(lipgloss.Left, listLines...)

	// Footer: mode-specific hint
	var footer string
	switch s.mode {
	case sessionModeConfirmDelete:
		footer = baseStyle.
			Foreground(t.Error()).
			Bold(true).
			Width(innerWidth).
			Render("Delete session? y/n")
	case sessionModeRename:
		s.renameInput.Width = innerWidth - 9
		footer = baseStyle.
			Width(innerWidth).
			Render("Rename: " + s.renameInput.View())
	default:
		footer = baseStyle.
			Foreground(t.TextMuted()).
			Width(innerWidth).
			Render("enter select  ctrl+d delete  ctrl+r rename")
	}

	title := baseStyle.
		Foreground(t.Primary()).
		Bold(true).
		Width(innerWidth).
		Render("Sessions")

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		title,
		"",
		searchBox,
		"",
		listView,
		"",
		footer,
	)

	return baseStyle.
		Padding(1, 2).
		Border(lipgloss.RoundedBorder()).
		BorderBackground(t.Background()).
		BorderForeground(t.TextMuted()).
		Render(content)
}

func (s *sessionDialogCmp) BindingKeys() []key.Binding {
	return layout.KeyMapToSlice(sessionKeys)
}

func (s *sessionDialogCmp) SetSessions(sessions []session.Session) {
	s.allSessions = sessions
	s.applyFilter()

	if s.selectedSessionID != "" {
		for i, sess := range s.filtered {
			if sess.ID == s.selectedSessionID {
				s.selectedIdx = i
				return
			}
		}
	}
	s.selectedIdx = 0
}

func (s *sessionDialogCmp) SetSelectedSession(sessionID string) {
	s.selectedSessionID = sessionID
	for i, sess := range s.filtered {
		if sess.ID == sessionID {
			s.selectedIdx = i
			return
		}
	}
}

// NewSessionDialogCmp creates a new session switching dialog
func NewSessionDialogCmp() SessionDialog {
	si := textinput.New()
	si.Placeholder = "Search sessions..."
	si.Focus()

	ri := textinput.New()
	ri.Placeholder = "New name..."

	return &sessionDialogCmp{
		allSessions: []session.Session{},
		filtered:    []session.Session{},
		searchInput: si,
		renameInput: ri,
		mode:        sessionModeNavigate,
	}
}
