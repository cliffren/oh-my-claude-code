package chat

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/cliffren/toc/internal/app"
	"github.com/cliffren/toc/internal/message"
	"github.com/cliffren/toc/internal/pubsub"
	"github.com/cliffren/toc/internal/session"
	"github.com/cliffren/toc/internal/tui/components/dialog"
	"github.com/cliffren/toc/internal/tui/styles"
	"github.com/cliffren/toc/internal/tui/theme"
	"github.com/cliffren/toc/internal/tui/util"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

type cacheItem struct {
	width   int
	content []uiMessage
}
type messagesCmp struct {
	app             *app.App
	width, height   int
	viewport        viewport.Model
	session         session.Session
	messages        []message.Message
	uiMessages      []uiMessage
	currentMsgID    string
	cachedContent   map[string]cacheItem
	expandedBlocks  map[string]bool
	blockParent     map[string]string // blockID → parent message ID, for precise cache invalidation
	scrollbarNormal string            // pre-rendered scrollbar normal char (refreshed on theme change)
	scrollbarThumb  string            // pre-rendered scrollbar thumb char
	cachedHelp      string            // pre-rendered help line (refreshed on busy-state or size change)
	lastHelpBusy    bool
	spinner         spinner.Model
	rendering       bool
	attachments     viewport.Model
	selection       selectionController
	clipboard       clipboardWriter
}

type ToggleExpandMsg struct {
	BlockID string
}
type renderFinishedMsg struct{}

type MessageKeys struct {
	PageDown     key.Binding
	PageUp       key.Binding
	HalfPageUp   key.Binding
	HalfPageDown key.Binding
}

var messageKeys = MessageKeys{
	PageDown: key.NewBinding(
		key.WithKeys("pgdown"),
		key.WithHelp("f/pgdn", "page down"),
	),
	PageUp: key.NewBinding(
		key.WithKeys("pgup"),
		key.WithHelp("b/pgup", "page up"),
	),
	HalfPageUp: key.NewBinding(
		key.WithKeys("ctrl+u"),
		key.WithHelp("ctrl+u", "½ page up"),
	),
	HalfPageDown: key.NewBinding(
		key.WithKeys("ctrl+d", "ctrl+d"),
		key.WithHelp("ctrl+d", "½ page down"),
	),
}

func (m *messagesCmp) Init() tea.Cmd {
	return tea.Batch(m.viewport.Init(), m.spinner.Tick)
}

func (m *messagesCmp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	switch msg := msg.(type) {
	case dialog.ThemeChangedMsg:
		m.rebuildScrollbarStrings()
		m.rerender()
		return m, nil
	case SessionSelectedMsg:
		if msg.ID != m.session.ID {
			cmd := m.SetSession(msg)
			return m, cmd
		}
		return m, nil
	case ToggleExpandMsg:
		if m.expandedBlocks[msg.BlockID] {
			delete(m.expandedBlocks, msg.BlockID)
		} else {
			m.expandedBlocks[msg.BlockID] = true
		}
		// Precise cache invalidation: blockParent maps individual tool call IDs
		// (and group/thinking block IDs) to their parent message ID.
		if parentID, ok := m.blockParent[msg.BlockID]; ok {
			delete(m.cachedContent, parentID)
		} else {
			// Fallback prefix match for any block not yet in the map.
			for _, mm := range m.messages {
				if strings.HasPrefix(msg.BlockID, mm.ID) {
					delete(m.cachedContent, mm.ID)
					break
				}
			}
		}
		m.renderView()
		return m, nil

	case SessionClearedMsg:
		m.session = session.Session{}
		m.messages = make([]message.Message, 0)
		m.currentMsgID = ""
		m.rendering = false
		return m, nil

	case tea.KeyMsg:
		if key.Matches(msg, messageKeys.PageUp) || key.Matches(msg, messageKeys.PageDown) ||
			key.Matches(msg, messageKeys.HalfPageUp) || key.Matches(msg, messageKeys.HalfPageDown) {
			u, cmd := m.viewport.Update(msg)
			m.viewport = u
			cmds = append(cmds, cmd)
		}
	case tea.MouseMsg:
		if msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown || msg.Button == tea.MouseButtonWheelLeft || msg.Button == tea.MouseButtonWheelRight {
			u, cmd := m.viewport.Update(msg)
			m.viewport = u
			cmds = append(cmds, cmd)
			break
		}

		// Check for click on expand/collapse hints
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			if blockID := m.hitTestExpandCollapse(msg.Y); blockID != "" {
				m.Update(ToggleExpandMsg{BlockID: blockID})
				break
			}
		}

		_, _, copied, err := m.selection.handleMouse(msg, m.selectionRegion(), m.visiblePlainLines(), m.clipboard)
		if err != nil {
			cmds = append(cmds, util.ReportError(err))
		}
		if copied {
			cmds = append(cmds, util.ReportInfo("Copied to clipboard"))
			m.renderView() // refresh to clear selection highlight
		}
		if m.selection.capturesMouse() || msg.Action == tea.MouseActionRelease {
			break
		}
		u, cmd := m.viewport.Update(msg)
		m.viewport = u
		cmds = append(cmds, cmd)

	case renderFinishedMsg:
		m.rendering = false
		m.viewport.GotoBottom()
	case pubsub.Event[session.Session]:
		if msg.Type == pubsub.UpdatedEvent && msg.Payload.ID == m.session.ID {
			m.session = msg.Payload
			if m.session.SummaryMessageID == m.currentMsgID {
				delete(m.cachedContent, m.currentMsgID)
				m.renderView()
			}
		}
	case pubsub.Event[message.Message]:
		needsRerender := false
		if msg.Type == pubsub.CreatedEvent {
			if msg.Payload.SessionID == m.session.ID {

				messageExists := false
				for _, v := range m.messages {
					if v.ID == msg.Payload.ID {
						messageExists = true
						break
					}
				}

				if !messageExists {
					if len(m.messages) > 0 {
						lastMsgID := m.messages[len(m.messages)-1].ID
						delete(m.cachedContent, lastMsgID)
					}

					m.messages = append(m.messages, msg.Payload)
					delete(m.cachedContent, m.currentMsgID)
					m.currentMsgID = msg.Payload.ID
					needsRerender = true
				}
			}
			// There are tool calls from the child task
			for _, v := range m.messages {
				for _, c := range v.ToolCalls() {
					if c.ID == msg.Payload.SessionID {
						delete(m.cachedContent, v.ID)
						needsRerender = true
					}
				}
			}
		} else if msg.Type == pubsub.UpdatedEvent && msg.Payload.SessionID == m.session.ID {
			for i, v := range m.messages {
				if v.ID == msg.Payload.ID {
					m.messages[i] = msg.Payload
					delete(m.cachedContent, msg.Payload.ID)
					needsRerender = true
					break
				}
			}
		}
		if needsRerender {
			wasAtBottom := m.viewport.AtBottom()
			m.renderView()
			if len(m.messages) > 0 && (msg.Type == pubsub.CreatedEvent || wasAtBottom) {
				m.viewport.GotoBottom()
			}
		}
	}

	spinner, cmd := m.spinner.Update(msg)
	m.spinner = spinner
	cmds = append(cmds, cmd)
	return m, tea.Batch(cmds...)
}

func (m *messagesCmp) IsAgentWorking() bool {
	return m.app.CoderAgent.IsSessionBusy(m.session.ID)
}

func formatTimeDifference(unixTime1, unixTime2 int64) string {
	diffSeconds := float64(math.Abs(float64(unixTime2 - unixTime1)))

	if diffSeconds < 60 {
		return fmt.Sprintf("%.1fs", diffSeconds)
	}

	minutes := int(diffSeconds / 60)
	seconds := int(diffSeconds) % 60
	return fmt.Sprintf("%dm%ds", minutes, seconds)
}

func (m *messagesCmp) renderView() {
	m.uiMessages = make([]uiMessage, 0)
	pos := 0
	baseStyle := styles.BaseStyle()

	if m.width == 0 {
		return
	}
	contentWidth := max(0, m.viewport.Width)
	m.blockParent = make(map[string]string)
	for inx, msg := range m.messages {
		switch msg.Role {
		case message.User:
			if cache, ok := m.cachedContent[msg.ID]; ok && cache.width == contentWidth {
				m.uiMessages = append(m.uiMessages, cache.content...)
				continue
			}
			userMsg := renderUserMessage(
				msg,
				msg.ID == m.currentMsgID,
				contentWidth,
				pos,
			)
			m.uiMessages = append(m.uiMessages, userMsg)
			m.cachedContent[msg.ID] = cacheItem{
				width:   contentWidth,
				content: []uiMessage{userMsg},
			}
			pos += userMsg.height + 1 // + 1 for spacing
		case message.Assistant:
			var assistantMessages []uiMessage
			if cache, ok := m.cachedContent[msg.ID]; ok && cache.width == contentWidth {
				assistantMessages = cache.content
			} else {
				isSummary := m.session.SummaryMessageID == msg.ID
				assistantMessages = renderAssistantMessage(
					msg,
					inx,
					m.messages,
					m.app.Messages,
					m.currentMsgID,
					isSummary,
					contentWidth,
					pos,
					m.expandedBlocks,
				)
				m.cachedContent[msg.ID] = cacheItem{
					width:   contentWidth,
					content: assistantMessages,
				}
			}
			for _, uiMsg := range assistantMessages {
				m.uiMessages = append(m.uiMessages, uiMsg)
				pos += uiMsg.height + 1 // + 1 for spacing
				// Map this uiMessage and its subBlocks to the parent message ID
				// so ToggleExpandMsg can invalidate precisely without wiping all cache.
				m.blockParent[uiMsg.ID] = msg.ID
				for _, sub := range uiMsg.subBlocks {
					m.blockParent[sub.id] = msg.ID
				}
			}
		}
	}

	messages := make([]string, 0)
	for _, v := range m.uiMessages {
		messages = append(messages, lipgloss.JoinVertical(lipgloss.Left, v.content),
			baseStyle.
				Width(contentWidth).
				Render(
					"",
				),
		)
	}

	m.viewport.SetContent(
		baseStyle.
			Width(contentWidth).
			Render(
				lipgloss.JoinVertical(
					lipgloss.Top,
					messages...,
				),
			),
	)
}

func (m *messagesCmp) View() string {
	baseStyle := styles.BaseStyle()

	if m.rendering {
		return baseStyle.
			Width(m.width).
			Render(
				lipgloss.JoinVertical(
					lipgloss.Top,
					"Loading...",
					m.working(),
					m.help(),
				),
			)
	}
	if len(m.messages) == 0 {
		content := baseStyle.
			Width(m.width).
			Height(m.height - 1).
			Render(
				m.initialScreen(),
			)

		return baseStyle.
			Width(m.width).
			Render(
				lipgloss.JoinVertical(
					lipgloss.Top,
					content,
					"",
					m.help(),
				),
			)
	}

	return baseStyle.
		Width(m.width).
		Render(
			lipgloss.JoinVertical(
				lipgloss.Top,
				m.renderViewport(),
				m.working(),
				m.help(),
			),
		)
}

func hasToolsWithoutResponse(messages []message.Message) bool {
	toolCalls := make([]message.ToolCall, 0)
	toolResults := make([]message.ToolResult, 0)
	for _, m := range messages {
		toolCalls = append(toolCalls, m.ToolCalls()...)
		toolResults = append(toolResults, m.ToolResults()...)
	}

	for _, v := range toolCalls {
		found := false
		for _, r := range toolResults {
			if v.ID == r.ToolCallID {
				found = true
				break
			}
		}
		if !found && v.Finished {
			return true
		}
	}
	return false
}

func hasUnfinishedToolCalls(messages []message.Message) bool {
	toolCalls := make([]message.ToolCall, 0)
	for _, m := range messages {
		toolCalls = append(toolCalls, m.ToolCalls()...)
	}
	for _, v := range toolCalls {
		if !v.Finished {
			return true
		}
	}
	return false
}

func (m *messagesCmp) working() string {
	text := ""
	if m.IsAgentWorking() && len(m.messages) > 0 {
		t := theme.CurrentTheme()
		baseStyle := styles.BaseStyle()

		task := "Thinking..."
		lastMessage := m.messages[len(m.messages)-1]
		if hasToolsWithoutResponse(m.messages) {
			task = "Waiting for tool response..."
		} else if hasUnfinishedToolCalls(m.messages) {
			task = "Building tool call..."
		} else if !lastMessage.IsFinished() {
			task = "Generating..."
		}
		if task != "" {
			text += baseStyle.
				Width(m.width).
				Foreground(t.Primary()).
				Bold(true).
				Render(fmt.Sprintf("%s %s ", m.spinner.View(), task))
		}
	}
	return text
}

func (m *messagesCmp) help() string {
	busy := m.app.CoderAgent.IsBusy()
	if m.cachedHelp != "" && busy == m.lastHelpBusy {
		return m.cachedHelp
	}
	m.lastHelpBusy = busy

	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()
	var text string
	if busy {
		text = lipgloss.JoinHorizontal(
			lipgloss.Left,
			baseStyle.Foreground(t.TextMuted()).Bold(true).Render("press "),
			baseStyle.Foreground(t.Text()).Bold(true).Render("esc"),
			baseStyle.Foreground(t.TextMuted()).Bold(true).Render(" to exit cancel"),
		)
	} else {
		text = lipgloss.JoinHorizontal(
			lipgloss.Left,
			baseStyle.Foreground(t.TextMuted()).Bold(true).Render("press "),
			baseStyle.Foreground(t.Text()).Bold(true).Render("enter"),
			baseStyle.Foreground(t.TextMuted()).Bold(true).Render(" to send the message,"),
			baseStyle.Foreground(t.TextMuted()).Bold(true).Render(" write"),
			baseStyle.Foreground(t.Text()).Bold(true).Render(" \\"),
			baseStyle.Foreground(t.TextMuted()).Bold(true).Render(" and enter to add a new line"),
		)
	}
	m.cachedHelp = baseStyle.Width(m.width).Render(text)
	return m.cachedHelp
}

func (m *messagesCmp) initialScreen() string {
	baseStyle := styles.BaseStyle()

	return baseStyle.Width(m.width).Render(
		lipgloss.JoinVertical(
			lipgloss.Top,
			header(m.width),
			// "", // LSP disabled
			// lspsConfigured(m.width), // LSP disabled
		),
	)
}

func (m *messagesCmp) rebuildScrollbarStrings() {
	t := theme.CurrentTheme()
	m.scrollbarNormal = lipgloss.NewStyle().Foreground(t.BorderNormal()).Render("│")
	m.scrollbarThumb = lipgloss.NewStyle().Foreground(t.Primary()).Render("█")
	m.cachedHelp = "" // theme changed — invalidate help cache
}

func (m *messagesCmp) rerender() {
	m.cachedHelp = "" // size or theme changed
	for _, msg := range m.messages {
		delete(m.cachedContent, msg.ID)
	}
	m.renderView()
}

func (m *messagesCmp) SetSize(width, height int) tea.Cmd {
	if m.width == width && m.height == height {
		return nil
	}
	m.width = width
	m.height = height
	m.viewport.Width = max(0, width-1)
	m.viewport.Height = height - 2
	m.attachments.Width = width + 40
	m.attachments.Height = 3
	m.rerender()
	return nil
}

func (m *messagesCmp) GetSize() (int, int) {
	return m.width, m.height
}

func (m *messagesCmp) SetSession(session session.Session) tea.Cmd {
	if m.session.ID == session.ID {
		return nil
	}
	m.session = session
	messages, err := m.app.Messages.List(context.Background(), session.ID)
	if err != nil {
		return util.ReportError(err)
	}
	m.messages = messages
	if len(m.messages) > 0 {
		m.currentMsgID = m.messages[len(m.messages)-1].ID
	}
	delete(m.cachedContent, m.currentMsgID)
	m.rendering = true
	return func() tea.Msg {
		m.renderView()
		return renderFinishedMsg{}
	}
}

func (m *messagesCmp) BindingKeys() []key.Binding {
	return []key.Binding{
		m.viewport.KeyMap.PageDown,
		m.viewport.KeyMap.PageUp,
		m.viewport.KeyMap.HalfPageUp,
		m.viewport.KeyMap.HalfPageDown,
	}
}

func (m *messagesCmp) CapturesMouse() bool {
	return m.selection.capturesMouse()
}

func NewMessagesCmp(app *app.App) tea.Model {
	s := spinner.New()
	s.Spinner = spinner.Pulse
	vp := viewport.New(0, 0)
	attachmets := viewport.New(0, 0)
	vp.KeyMap.PageUp = messageKeys.PageUp
	vp.KeyMap.PageDown = messageKeys.PageDown
	vp.KeyMap.HalfPageUp = messageKeys.HalfPageUp
	vp.KeyMap.HalfPageDown = messageKeys.HalfPageDown
	cmp := &messagesCmp{
		app:            app,
		cachedContent:  make(map[string]cacheItem),
		expandedBlocks: make(map[string]bool),
		blockParent:    make(map[string]string),
		viewport:       vp,
		spinner:        s,
		attachments:    attachmets,
		clipboard:      newClipboardWriter(),
	}
	cmp.rebuildScrollbarStrings()
	return cmp
}

// hitTestExpandCollapse checks if a viewport Y coordinate is on an expand/collapse hint line.
// Returns the block ID to toggle, or empty string if not on a hint.
func (m *messagesCmp) hitTestExpandCollapse(viewportY int) string {
	// Convert viewport Y to content line
	contentY := viewportY + m.viewport.YOffset
	lines := strings.Split(m.viewport.View(), "\n")
	if viewportY < 0 || viewportY >= len(lines) {
		return ""
	}
	line := ansi.Strip(lines[viewportY])
	// Strip the left border character (┃ from ThickBorder, │ from NormalBorder) and
	// any leading spaces added by PaddingLeft, then check for expand/collapse prefix.
	trimmedLine := strings.TrimLeft(line, "┃│ \t")
	isExpandHint := strings.HasPrefix(trimmedLine, "▶ ") || strings.HasPrefix(trimmedLine, "▼ ")
	if !isExpandHint {
		return ""
	}

	// Find which uiMessage this line belongs to
	lineOffset := 0
	for _, uiMsg := range m.uiMessages {
		msgEnd := lineOffset + uiMsg.height
		if contentY >= lineOffset && contentY < msgEnd {
			// Check subBlocks for a more specific match (e.g. individual tool calls within a group)
			localLine := contentY - lineOffset
			for _, sub := range uiMsg.subBlocks {
				if localLine == sub.lineOffset {
					return sub.id
				}
			}
			return uiMsg.ID
		}
		lineOffset = msgEnd + 1 // +1 for spacing
	}
	return ""
}

func (m *messagesCmp) selectionRegion() selectionRegion {
	return selectionRegion{
		X:      0,
		Y:      0,
		Width:  m.viewport.Width,
		Height: m.viewport.Height,
	}
}

func (m *messagesCmp) visiblePlainLines() []string {
	visible := strings.Split(m.viewport.View(), "\n")
	lines := make([]string, len(visible))
	for i, line := range visible {
		lines[i] = ansi.Strip(line)
	}
	return lines
}

func (m *messagesCmp) renderViewport() string {
	visible := strings.Split(m.viewport.View(), "\n")
	for len(visible) < m.viewport.Height {
		visible = append(visible, "")
	}
	if m.selection.hasSelection() {
		t := theme.CurrentTheme()
		start, end := m.selection.bounds()
		visible = highlightSelectedLines(visible, start, end, lipgloss.NewStyle().Background(t.BackgroundSecondary()).Foreground(t.Text()))
	}
	thumb := calculateScrollbarThumb(m.viewport.Height, m.viewport.TotalLineCount(), m.viewport.YOffset)
	scrollbar := renderScrollbar(m.viewport.Height, thumb, m.scrollbarNormal, m.scrollbarThumb)
	// Direct string concatenation instead of lipgloss.JoinHorizontal:
	// viewport content is rendered at a fixed width, so ANSI width measurement is unnecessary.
	var b strings.Builder
	b.Grow(len(visible) * (m.viewport.Width + 4))
	for i, line := range visible {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(line)
		b.WriteString(scrollbar[i])
	}
	return b.String()
}
