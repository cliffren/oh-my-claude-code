package layout

import (
	"strings"

	"github.com/cliffren/toc/internal/tui/theme"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type SplitPaneLayout interface {
	tea.Model
	Sizeable
	Bindings
	SetLeftPanel(panel Container) tea.Cmd
	SetRightPanel(panel Container) tea.Cmd
	SetBottomPanel(panel Container) tea.Cmd

	ClearLeftPanel() tea.Cmd
	ClearRightPanel() tea.Cmd
	ClearBottomPanel() tea.Cmd
}

type splitPaneLayout struct {
	width            int
	height           int
	ratio            float64
	verticalRatio    float64
	bottomExtraLines int // extra rows added to bottom panel beyond ratio

	rightPanel  Container
	leftPanel   Container
	bottomPanel Container

	// Cached divider string — rebuilt only when height or theme colors change.
	cachedDivider   string
	cachedDividerH  int
	cachedDividerFg lipgloss.AdaptiveColor
	cachedDividerBg lipgloss.AdaptiveColor

	// Cached outer background style — rebuilt only when size or theme changes.
	cachedOuterStyle lipgloss.Style
	cachedOuterW     int
	cachedOuterH     int
	cachedOuterBg    lipgloss.AdaptiveColor
}

type SplitPaneOption func(*splitPaneLayout)

func (s *splitPaneLayout) Init() tea.Cmd {
	var cmds []tea.Cmd

	if s.leftPanel != nil {
		cmds = append(cmds, s.leftPanel.Init())
	}

	if s.rightPanel != nil {
		cmds = append(cmds, s.rightPanel.Init())
	}

	if s.bottomPanel != nil {
		cmds = append(cmds, s.bottomPanel.Init())
	}

	return tea.Batch(cmds...)
}

func (s *splitPaneLayout) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return s, s.SetSize(msg.Width, msg.Height)
	case tea.MouseMsg:
		return s.updateMouse(msg)
	}

	if s.rightPanel != nil {
		u, cmd := s.rightPanel.Update(msg)
		s.rightPanel = u.(Container)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	if s.leftPanel != nil {
		u, cmd := s.leftPanel.Update(msg)
		s.leftPanel = u.(Container)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	if s.bottomPanel != nil {
		u, cmd := s.bottomPanel.Update(msg)
		s.bottomPanel = u.(Container)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	return s, tea.Batch(cmds...)
}

func (s *splitPaneLayout) updateMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if target := s.capturedPanel(); target != nil {
		translated := s.translateMouseForPanel(target, msg)
		updated, cmd := target.Update(translated)
		s.assignUpdatedPanel(target, updated.(Container))
		return s, cmd
	}

	var target Container
	var translated tea.MouseMsg

	topHeight, bottomHeight := s.sectionHeights()
	leftWidth, rightWidth := s.sectionWidths()

	bottomWidth := leftWidth
	if s.rightPanel == nil {
		bottomWidth = s.width
	}
	if s.bottomPanel != nil && msg.Y >= topHeight && msg.Y < topHeight+bottomHeight && msg.X < bottomWidth {
		target = s.bottomPanel
		translated = msg
		translated.Y -= topHeight
	} else if s.leftPanel != nil && msg.X < leftWidth && msg.Y < topHeight {
		target = s.leftPanel
		translated = msg
	} else if s.rightPanel != nil && msg.X >= leftWidth && msg.X < leftWidth+rightWidth {
		target = s.rightPanel
		translated = msg
		translated.X -= leftWidth
	} else {
		return s, nil
	}

	updated, cmd := target.Update(translated)
	s.assignUpdatedPanel(target, updated.(Container))
	return s, cmd
}

func (s *splitPaneLayout) capturedPanel() Container {
	for _, panel := range []Container{s.leftPanel, s.rightPanel, s.bottomPanel} {
		if panel != nil && panel.CapturesMouse() {
			return panel
		}
	}
	return nil
}

func (s *splitPaneLayout) translateMouseForPanel(target Container, msg tea.MouseMsg) tea.MouseMsg {
	translated := msg
	topHeight, _ := s.sectionHeights()
	leftWidth, _ := s.sectionWidths()

	switch target {
	case s.rightPanel:
		translated.X -= leftWidth
	case s.bottomPanel:
		translated.Y -= topHeight
	}

	return translated
}

func (s *splitPaneLayout) View() string {
	var finalView string

	hasLeft := s.leftPanel != nil
	hasRight := s.rightPanel != nil
	hasBottom := s.bottomPanel != nil

	if hasLeft && hasRight && hasBottom {
		// Layout: left column (messages + editor) | divider | right column (sidebar full height)
		leftView := s.leftPanel.View()
		rightView := s.rightPanel.View()
		bottomView := s.bottomPanel.View()

		divider := s.verticalDivider(s.height)
		leftCol := lipgloss.JoinVertical(lipgloss.Left, leftView, bottomView)
		finalView = lipgloss.JoinHorizontal(lipgloss.Top, leftCol, divider, rightView)
	} else {
		// Standard layout: top section (left | divider | right) over bottom
		var topSection string
		if hasLeft && hasRight {
			topHeight, _ := s.sectionHeights()
			divider := s.verticalDivider(topHeight)
			topSection = lipgloss.JoinHorizontal(lipgloss.Top, s.leftPanel.View(), divider, s.rightPanel.View())
		} else if hasLeft {
			topSection = s.leftPanel.View()
		} else if hasRight {
			topSection = s.rightPanel.View()
		}

		if hasBottom && topSection != "" {
			finalView = lipgloss.JoinVertical(lipgloss.Left, topSection, s.bottomPanel.View())
		} else if hasBottom {
			finalView = s.bottomPanel.View()
		} else {
			finalView = topSection
		}
	}

	if finalView != "" {
		t := theme.CurrentTheme()
		bg := t.Background()
		if s.cachedOuterW != s.width || s.cachedOuterH != s.height || s.cachedOuterBg != bg {
			s.cachedOuterStyle = lipgloss.NewStyle().
				Width(s.width).
				Height(s.height).
				Background(bg)
			s.cachedOuterW = s.width
			s.cachedOuterH = s.height
			s.cachedOuterBg = bg
		}
		return s.cachedOuterStyle.Render(finalView)
	}

	return finalView
}

func (s *splitPaneLayout) verticalDivider(height int) string {
	t := theme.CurrentTheme()
	fg := t.BorderNormal()
	bg := t.Background()
	if s.cachedDivider != "" && s.cachedDividerH == height &&
		s.cachedDividerFg == fg && s.cachedDividerBg == bg {
		return s.cachedDivider
	}
	line := lipgloss.NewStyle().Foreground(fg).Background(bg).Render("│")
	lines := make([]string, height)
	for i := range lines {
		lines[i] = line
	}
	s.cachedDivider = strings.Join(lines, "\n")
	s.cachedDividerH = height
	s.cachedDividerFg = fg
	s.cachedDividerBg = bg
	return s.cachedDivider
}

func (s *splitPaneLayout) SetSize(width, height int) tea.Cmd {
	s.width = width
	s.height = height

	topHeight, bottomHeight := s.sectionHeights()
	leftWidth, rightWidth := s.sectionWidths()

	var cmds []tea.Cmd
	if s.leftPanel != nil {
		cmd := s.leftPanel.SetSize(leftWidth, topHeight)
		cmds = append(cmds, cmd)
	}

	if s.rightPanel != nil {
		// When bottom panel exists, right panel extends full height
		rightHeight := topHeight
		if s.bottomPanel != nil {
			rightHeight = height
		}
		cmd := s.rightPanel.SetSize(rightWidth, rightHeight)
		cmds = append(cmds, cmd)
	}

	if s.bottomPanel != nil {
		bottomWidth := leftWidth
		if s.rightPanel == nil {
			bottomWidth = width
		}
		cmd := s.bottomPanel.SetSize(bottomWidth, bottomHeight)
		cmds = append(cmds, cmd)
	}
	return tea.Batch(cmds...)
}

func (s *splitPaneLayout) sectionHeights() (int, int) {
	if s.bottomPanel != nil {
		topHeight := int(float64(s.height)*s.verticalRatio) - s.bottomExtraLines
		return topHeight, s.height - topHeight
	}
	return s.height, 0
}

func (s *splitPaneLayout) sectionWidths() (int, int) {
	if s.leftPanel != nil && s.rightPanel != nil {
		// Reserve 1 column for the vertical divider between panels.
		leftWidth := int(float64(s.width) * s.ratio)
		return leftWidth, s.width - leftWidth - 1
	}
	if s.leftPanel != nil {
		return s.width, 0
	}
	if s.rightPanel != nil {
		return 0, s.width
	}
	return 0, 0
}

func (s *splitPaneLayout) assignUpdatedPanel(target Container, updated Container) {
	switch target {
	case s.leftPanel:
		s.leftPanel = updated
	case s.rightPanel:
		s.rightPanel = updated
	case s.bottomPanel:
		s.bottomPanel = updated
	}
}

func (s *splitPaneLayout) GetSize() (int, int) {
	return s.width, s.height
}

func (s *splitPaneLayout) SetLeftPanel(panel Container) tea.Cmd {
	s.leftPanel = panel
	if s.width > 0 && s.height > 0 {
		return s.SetSize(s.width, s.height)
	}
	return nil
}

func (s *splitPaneLayout) SetRightPanel(panel Container) tea.Cmd {
	s.rightPanel = panel
	if s.width > 0 && s.height > 0 {
		return s.SetSize(s.width, s.height)
	}
	return nil
}

func (s *splitPaneLayout) SetBottomPanel(panel Container) tea.Cmd {
	s.bottomPanel = panel
	if s.width > 0 && s.height > 0 {
		return s.SetSize(s.width, s.height)
	}
	return nil
}

func (s *splitPaneLayout) ClearLeftPanel() tea.Cmd {
	s.leftPanel = nil
	if s.width > 0 && s.height > 0 {
		return s.SetSize(s.width, s.height)
	}
	return nil
}

func (s *splitPaneLayout) ClearRightPanel() tea.Cmd {
	s.rightPanel = nil
	if s.width > 0 && s.height > 0 {
		return s.SetSize(s.width, s.height)
	}
	return nil
}

func (s *splitPaneLayout) ClearBottomPanel() tea.Cmd {
	s.bottomPanel = nil
	if s.width > 0 && s.height > 0 {
		return s.SetSize(s.width, s.height)
	}
	return nil
}

func (s *splitPaneLayout) BindingKeys() []key.Binding {
	keys := []key.Binding{}
	if s.leftPanel != nil {
		if b, ok := s.leftPanel.(Bindings); ok {
			keys = append(keys, b.BindingKeys()...)
		}
	}
	if s.rightPanel != nil {
		if b, ok := s.rightPanel.(Bindings); ok {
			keys = append(keys, b.BindingKeys()...)
		}
	}
	if s.bottomPanel != nil {
		if b, ok := s.bottomPanel.(Bindings); ok {
			keys = append(keys, b.BindingKeys()...)
		}
	}
	return keys
}

func NewSplitPane(options ...SplitPaneOption) SplitPaneLayout {

	layout := &splitPaneLayout{
		ratio:         0.7,
		verticalRatio: 0.9, // Default 90% for top section, 10% for bottom
	}
	for _, option := range options {
		option(layout)
	}
	return layout
}

func WithLeftPanel(panel Container) SplitPaneOption {
	return func(s *splitPaneLayout) {
		s.leftPanel = panel
	}
}

func WithRightPanel(panel Container) SplitPaneOption {
	return func(s *splitPaneLayout) {
		s.rightPanel = panel
	}
}

func WithRatio(ratio float64) SplitPaneOption {
	return func(s *splitPaneLayout) {
		s.ratio = ratio
	}
}

func WithBottomExtraLines(n int) SplitPaneOption {
	return func(s *splitPaneLayout) {
		s.bottomExtraLines = n
	}
}

func WithBottomPanel(panel Container) SplitPaneOption {
	return func(s *splitPaneLayout) {
		s.bottomPanel = panel
	}
}

func WithVerticalRatio(ratio float64) SplitPaneOption {
	return func(s *splitPaneLayout) {
		s.verticalRatio = ratio
	}
}
