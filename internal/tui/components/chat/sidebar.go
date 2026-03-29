package chat

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/cliffren/toc/internal/config"
	"github.com/cliffren/toc/internal/diff"
	"github.com/cliffren/toc/internal/history"
	"github.com/cliffren/toc/internal/llm/tools"
	"github.com/cliffren/toc/internal/logging"
	"github.com/cliffren/toc/internal/pubsub"
	"github.com/cliffren/toc/internal/session"
	"github.com/cliffren/toc/internal/tui/components/dialog"
	"github.com/cliffren/toc/internal/tui/styles"
	"github.com/cliffren/toc/internal/tui/theme"
	"github.com/cliffren/toc/internal/tui/util"
)

type sidebarCmp struct {
	width, height int
	viewport      viewport.Model
	session       session.Session
	history       history.Service
	todoStore     *tools.TodoStore
	selection     selectionController
	clipboard     clipboardWriter
	modFiles      map[string]struct {
		additions int
		removals  int
	}
	filesCh <-chan pubsub.Event[history.File]
	todoCh  <-chan pubsub.Event[tools.TodoEvent]
}

func (m *sidebarCmp) Init() tea.Cmd {
	var cmds []tea.Cmd
	if m.history != nil {
		ctx := context.Background()
		m.filesCh = m.history.Subscribe(ctx)
		m.modFiles = make(map[string]struct {
			additions int
			removals  int
		})
		m.loadModifiedFiles(ctx)
		cmds = append(cmds, m.waitForFileEvent())
	}
	if m.todoStore != nil {
		ctx := context.Background()
		m.todoCh = m.todoStore.Subscribe(ctx)
		cmds = append(cmds, m.waitForTodoEvent())
	}
	return tea.Batch(cmds...)
}

func (m *sidebarCmp) waitForFileEvent() tea.Cmd {
	ch := m.filesCh
	return func() tea.Msg {
		return <-ch
	}
}

func (m *sidebarCmp) waitForTodoEvent() tea.Cmd {
	ch := m.todoCh
	return func() tea.Msg {
		return <-ch
	}
}

func (m *sidebarCmp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	switch msg := msg.(type) {
	case tea.MouseMsg:
		if msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown {
			u, cmd := m.viewport.Update(msg)
			m.viewport = u
			cmds = append(cmds, cmd)
			return m, tea.Batch(cmds...)
		}
		_, _, _, err := m.selection.handleMouse(msg, m.selectionRegion(), m.visiblePlainLines(), m.clipboard)
		if err != nil {
			cmds = append(cmds, util.ReportError(err))
		}
		if m.selection.capturesMouse() || msg.Action == tea.MouseActionRelease {
			return m, tea.Batch(cmds...)
		}
		return m, tea.Batch(cmds...)
	case dialog.ThemeChangedMsg:
		m.rebuildViewport()
		return m, nil
	case SessionSelectedMsg:
		if msg.ID != m.session.ID {
			m.session = msg
			ctx := context.Background()
			m.loadModifiedFiles(ctx)
			m.rebuildViewport()
		}
	case pubsub.Event[session.Session]:
		if msg.Type == pubsub.UpdatedEvent {
			if m.session.ID == msg.Payload.ID {
				m.session = msg.Payload
				m.rebuildViewport()
			}
		}
	case pubsub.Event[history.File]:
		if msg.Payload.SessionID == m.session.ID {
			ctx := context.Background()
			m.processFileChanges(ctx, msg.Payload)
			m.rebuildViewport()
		}
		// Always re-register on the same channel (no new Subscribe call)
		return m, m.waitForFileEvent()
	case pubsub.Event[tools.TodoEvent]:
		if msg.Payload.SessionID == m.session.ID {
			m.rebuildViewport()
		}
		return m, m.waitForTodoEvent()
	}
	return m, nil
}

func (m *sidebarCmp) rebuildViewport() {
	parts := []string{
		header(m.width),
		" ",
		m.sessionSection(),
		" ",
	}

	// Show tasks section only when there are todos
	if todoSection := m.todoSection(); todoSection != "" {
		parts = append(parts, todoSection, " ")
	}

	parts = append(parts, m.modifiedFiles())

	content := lipgloss.JoinVertical(lipgloss.Top, parts...)
	m.viewport.SetContent(content)
}

func (m *sidebarCmp) View() string {
	t := theme.CurrentTheme()
	visible := strings.Split(m.viewport.View(), "\n")
	for len(visible) < m.viewport.Height {
		visible = append(visible, "")
	}
	if m.selection.hasSelection() {
		start, end := m.selection.bounds()
		visible = highlightSelectedLines(visible, start, end, lipgloss.NewStyle().Background(t.BackgroundSecondary()).Foreground(t.Text()))
	}
	return strings.Join(visible, "\n")
}

func (m *sidebarCmp) sessionSection() string {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()

	sessionKey := baseStyle.
		Foreground(t.Primary()).
		Bold(true).
		Render("Session")

	sessionValue := baseStyle.
		Foreground(t.Text()).
		Width(m.width - lipgloss.Width(sessionKey)).
		Render(fmt.Sprintf(": %s", m.session.Title))

	return lipgloss.JoinHorizontal(
		lipgloss.Left,
		sessionKey,
		sessionValue,
	)
}

func (m *sidebarCmp) todoSection() string {
	if m.todoStore == nil || m.session.ID == "" {
		return ""
	}
	todos := m.todoStore.Get(m.session.ID)
	if len(todos) == 0 {
		return ""
	}

	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()

	title := baseStyle.
		Width(m.width).
		Foreground(t.Primary()).
		Bold(true).
		Render("Tasks:")

	var items []string
	for _, todo := range todos {
		var icon string
		var fg lipgloss.AdaptiveColor
		switch todo.Status {
		case "completed":
			icon = "✓"
			fg = t.Success()
		case "in_progress":
			icon = "◉"
			fg = t.Warning()
		default:
			icon = "○"
			fg = t.TextMuted()
		}

		prefix := baseStyle.Foreground(fg).Render(icon + " ")
		prefixW := lipgloss.Width(prefix)
		content := todo.Content
		maxContentW := m.width - prefixW
		if maxContentW > 0 {
			content = ansi.Truncate(content, maxContentW, "...")
		}
		line := baseStyle.
			Width(m.width).
			Render(
				lipgloss.JoinHorizontal(lipgloss.Left,
					prefix,
					baseStyle.Foreground(fg).Render(content),
				),
			)
		items = append(items, line)
	}

	return baseStyle.
		Width(m.width).
		Render(
			lipgloss.JoinVertical(lipgloss.Top,
				title,
				lipgloss.JoinVertical(lipgloss.Left, items...),
			),
		)
}

func (m *sidebarCmp) modifiedFile(filePath string, additions, removals int) string {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()

	stats := ""
	if additions > 0 && removals > 0 {
		additionsStr := baseStyle.
			Foreground(t.Success()).
			PaddingLeft(1).
			Render(fmt.Sprintf("+%d", additions))

		removalsStr := baseStyle.
			Foreground(t.Error()).
			PaddingLeft(1).
			Render(fmt.Sprintf("-%d", removals))

		content := lipgloss.JoinHorizontal(lipgloss.Left, additionsStr, removalsStr)
		stats = baseStyle.Width(lipgloss.Width(content)).Render(content)
	} else if additions > 0 {
		additionsStr := fmt.Sprintf(" %s", baseStyle.
			PaddingLeft(1).
			Foreground(t.Success()).
			Render(fmt.Sprintf("+%d", additions)))
		stats = baseStyle.Width(lipgloss.Width(additionsStr)).Render(additionsStr)
	} else if removals > 0 {
		removalsStr := fmt.Sprintf(" %s", baseStyle.
			PaddingLeft(1).
			Foreground(t.Error()).
			Render(fmt.Sprintf("-%d", removals)))
		stats = baseStyle.Width(lipgloss.Width(removalsStr)).Render(removalsStr)
	}

	filePathStr := baseStyle.Render(filePath)

	return baseStyle.
		Width(m.width).
		Render(
			lipgloss.JoinHorizontal(
				lipgloss.Left,
				filePathStr,
				stats,
			),
		)
}

func (m *sidebarCmp) modifiedFiles() string {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()

	modifiedFiles := baseStyle.
		Width(m.width).
		Foreground(t.Primary()).
		Bold(true).
		Render("Modified Files:")

	// If no modified files, show a placeholder message
	if m.modFiles == nil || len(m.modFiles) == 0 {
		message := "No modified files"
		remainingWidth := m.width - lipgloss.Width(message)
		if remainingWidth > 0 {
			message += strings.Repeat(" ", remainingWidth)
		}
		return baseStyle.
			Width(m.width).
			Render(
				lipgloss.JoinVertical(
					lipgloss.Top,
					modifiedFiles,
					baseStyle.Foreground(t.TextMuted()).Render(message),
				),
			)
	}

	// Sort file paths alphabetically for consistent ordering
	var paths []string
	for path := range m.modFiles {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	// Create views for each file in sorted order
	var fileViews []string
	for _, path := range paths {
		stats := m.modFiles[path]
		fileViews = append(fileViews, m.modifiedFile(path, stats.additions, stats.removals))
	}

	return baseStyle.
		Width(m.width).
		Render(
			lipgloss.JoinVertical(
				lipgloss.Top,
				modifiedFiles,
				lipgloss.JoinVertical(
					lipgloss.Left,
					fileViews...,
				),
			),
		)
}

func (m *sidebarCmp) SetSize(width, height int) tea.Cmd {
	m.width = width
	m.height = height
	m.viewport.Width = width
	m.viewport.Height = height
	m.rebuildViewport()
	return nil
}

func (m *sidebarCmp) CapturesMouse() bool {
	return m.selection.capturesMouse()
}

func (m *sidebarCmp) selectionRegion() selectionRegion {
	return selectionRegion{
		X:      0,
		Y:      0,
		Width:  m.viewport.Width,
		Height: m.viewport.Height,
	}
}

func (m *sidebarCmp) visiblePlainLines() []string {
	visible := strings.Split(m.viewport.View(), "\n")
	lines := make([]string, len(visible))
	for i, line := range visible {
		lines[i] = ansi.Strip(line)
	}
	return lines
}

func (m *sidebarCmp) GetSize() (int, int) {
	return m.width, m.height
}

func NewSidebarCmp(session session.Session, history history.Service, todoStore *tools.TodoStore) tea.Model {
	vp := viewport.New(0, 0)
	return &sidebarCmp{
		viewport:  vp,
		session:   session,
		history:   history,
		todoStore: todoStore,
		clipboard: newClipboardWriter(),
	}
}

func (m *sidebarCmp) loadModifiedFiles(ctx context.Context) {
	if m.history == nil || m.session.ID == "" {
		return
	}

	// Get all latest files for this session
	latestFiles, err := m.history.ListLatestSessionFiles(ctx, m.session.ID)
	if err != nil {
		logging.Error(fmt.Sprintf("sidebar: failed to list latest session files: %v", err))
		return
	}

	// Get all files for this session (to find initial versions)
	allFiles, err := m.history.ListBySession(ctx, m.session.ID)
	if err != nil {
		logging.Error(fmt.Sprintf("sidebar: failed to list session files: %v", err))
		return
	}

	// Clear the existing map to rebuild it
	m.modFiles = make(map[string]struct {
		additions int
		removals  int
	})

	// Process each latest file
	for _, file := range latestFiles {
		// Skip if this is the initial version (no changes to show)
		if file.Version == history.InitialVersion {
			continue
		}

		// Find the initial version for this specific file
		var initialVersion history.File
		for _, v := range allFiles {
			if v.Path == file.Path && v.Version == history.InitialVersion {
				initialVersion = v
				break
			}
		}

		// Skip if we can't find the initial version
		if initialVersion.ID == "" {
			continue
		}
		if initialVersion.Content == file.Content {
			continue
		}

		// Calculate diff between initial and latest version
		_, additions, removals := diff.GenerateDiff(initialVersion.Content, file.Content, file.Path)

		// Only add to modified files if there are changes
		if additions > 0 || removals > 0 {
			// Remove working directory prefix from file path
			displayPath := file.Path
			workingDir := config.WorkingDirectory()
			displayPath = strings.TrimPrefix(displayPath, workingDir)
			displayPath = strings.TrimPrefix(displayPath, "/")

			m.modFiles[displayPath] = struct {
				additions int
				removals  int
			}{
				additions: additions,
				removals:  removals,
			}
		}
	}
}

func (m *sidebarCmp) processFileChanges(ctx context.Context, file history.File) {
	// Skip if this is the initial version (no changes to show)
	if file.Version == history.InitialVersion {
		return
	}

	// Find the initial version for this file
	initialVersion, err := m.findInitialVersion(ctx, file.Path)
	if err != nil || initialVersion.ID == "" {
		return
	}

	// Skip if content hasn't changed
	if initialVersion.Content == file.Content {
		// If this file was previously modified but now matches the initial version,
		// remove it from the modified files list
		displayPath := getDisplayPath(file.Path)
		delete(m.modFiles, displayPath)
		return
	}

	// Calculate diff between initial and latest version
	_, additions, removals := diff.GenerateDiff(initialVersion.Content, file.Content, file.Path)

	// Only add to modified files if there are changes
	if additions > 0 || removals > 0 {
		displayPath := getDisplayPath(file.Path)
		m.modFiles[displayPath] = struct {
			additions int
			removals  int
		}{
			additions: additions,
			removals:  removals,
		}
	} else {
		// If no changes, remove from modified files
		displayPath := getDisplayPath(file.Path)
		delete(m.modFiles, displayPath)
	}
}

// Helper function to find the initial version of a file
func (m *sidebarCmp) findInitialVersion(ctx context.Context, path string) (history.File, error) {
	// Get all versions of this file for the session
	fileVersions, err := m.history.ListBySession(ctx, m.session.ID)
	if err != nil {
		return history.File{}, err
	}

	// Find the initial version
	for _, v := range fileVersions {
		if v.Path == path && v.Version == history.InitialVersion {
			return v, nil
		}
	}

	return history.File{}, fmt.Errorf("initial version not found")
}

// Helper function to get the display path for a file
func getDisplayPath(path string) string {
	workingDir := config.WorkingDirectory()
	displayPath := strings.TrimPrefix(path, workingDir)
	return strings.TrimPrefix(displayPath, "/")
}
