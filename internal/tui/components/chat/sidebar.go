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
	"github.com/Krontx/oh-my-claude-code/internal/config"
	"github.com/Krontx/oh-my-claude-code/internal/diff"
	"github.com/Krontx/oh-my-claude-code/internal/history"
	"github.com/Krontx/oh-my-claude-code/internal/pubsub"
	"github.com/Krontx/oh-my-claude-code/internal/session"
	"github.com/Krontx/oh-my-claude-code/internal/tui/styles"
	"github.com/Krontx/oh-my-claude-code/internal/tui/theme"
	"github.com/Krontx/oh-my-claude-code/internal/tui/util"
)

type sidebarCmp struct {
	width, height int
	viewport      viewport.Model
	session       session.Session
	history       history.Service
	selection     selectionController
	clipboard     clipboardWriter
	modFiles      map[string]struct {
		additions int
		removals  int
	}
}

func (m *sidebarCmp) Init() tea.Cmd {
	if m.history != nil {
		ctx := context.Background()
		// Subscribe to file events
		filesCh := m.history.Subscribe(ctx)

		// Initialize the modified files map
		m.modFiles = make(map[string]struct {
			additions int
			removals  int
		})

		// Load initial files and calculate diffs
		m.loadModifiedFiles(ctx)

		// Return a command that will send file events to the Update method
		return func() tea.Msg {
			return <-filesCh
		}
	}
	return nil
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
	case SessionSelectedMsg:
		if msg.ID != m.session.ID {
			m.session = msg
			ctx := context.Background()
			m.loadModifiedFiles(ctx)
		}
	case pubsub.Event[session.Session]:
		if msg.Type == pubsub.UpdatedEvent {
			if m.session.ID == msg.Payload.ID {
				m.session = msg.Payload
			}
		}
	case pubsub.Event[history.File]:
		if msg.Payload.SessionID == m.session.ID {
			// Process the individual file change instead of reloading all files
			ctx := context.Background()
			m.processFileChanges(ctx, msg.Payload)

			// Return a command to continue receiving events
			return m, func() tea.Msg {
				ctx := context.Background()
				filesCh := m.history.Subscribe(ctx)
				return <-filesCh
			}
		}
	}
	return m, nil
}

func (m *sidebarCmp) View() string {
	// Build the sidebar content and set it in the viewport
	content := lipgloss.JoinVertical(
		lipgloss.Top,
		header(m.width),
		" ",
		m.sessionSection(),
		" ",
		lspsConfigured(m.width),
		" ",
		m.modifiedFiles(),
	)
	m.viewport.SetContent(content)

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

func NewSidebarCmp(session session.Session, history history.Service) tea.Model {
	vp := viewport.New(0, 0)
	return &sidebarCmp{
		viewport:  vp,
		session:   session,
		history:   history,
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
		return
	}

	// Get all files for this session (to find initial versions)
	allFiles, err := m.history.ListBySession(ctx, m.session.ID)
	if err != nil {
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
