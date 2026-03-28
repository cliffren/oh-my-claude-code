package page

import (
	"testing"

	"github.com/Krontx/oh-my-claude-code/internal/tui/layout"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

type mousePanelRecorder struct {
	lastMouse *tea.MouseMsg
	capture   bool
	width     int
	height    int
}

func (m *mousePanelRecorder) Init() tea.Cmd { return nil }

func (m *mousePanelRecorder) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if mouse, ok := msg.(tea.MouseMsg); ok {
		copied := mouse
		m.lastMouse = &copied
	}
	return m, nil
}

func (m *mousePanelRecorder) View() string { return "" }

func (m *mousePanelRecorder) SetSize(width, height int) tea.Cmd {
	m.width = width
	m.height = height
	return nil
}

func (m *mousePanelRecorder) GetSize() (int, int) { return m.width, m.height }

func (m *mousePanelRecorder) BindingKeys() []key.Binding { return nil }

func (m *mousePanelRecorder) CapturesMouse() bool { return m.capture }

func TestLogsPageRoutesMouseOnlyToHoveredPane(t *testing.T) {
	tableRecorder := &mousePanelRecorder{}
	detailsRecorder := &mousePanelRecorder{}
	page := &logsPage{
		table:   layout.NewContainer(tableRecorder),
		details: layout.NewContainer(detailsRecorder),
	}
	page.SetSize(100, 20)

	_, _ = page.Update(tea.MouseMsg{X: 10, Y: 15, Action: tea.MouseActionMotion, Button: tea.MouseButtonWheelDown})

	if tableRecorder.lastMouse != nil {
		t.Fatal("table should not receive mouse events for detail-pane coordinates")
	}
	if detailsRecorder.lastMouse == nil {
		t.Fatal("details pane should receive mouse events")
	}
	if detailsRecorder.lastMouse.Y != 5 {
		t.Fatalf("got local Y %d want 5", detailsRecorder.lastMouse.Y)
	}
}

func TestLogsPageKeepsSendingDragToCapturedPane(t *testing.T) {
	tableRecorder := &mousePanelRecorder{}
	detailsRecorder := &mousePanelRecorder{capture: true}
	page := &logsPage{
		table:   layout.NewContainer(tableRecorder),
		details: layout.NewContainer(detailsRecorder),
	}
	page.SetSize(100, 20)

	_, _ = page.Update(tea.MouseMsg{X: 10, Y: 15, Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft})

	if detailsRecorder.lastMouse == nil {
		t.Fatal("captured details pane should keep receiving drag events")
	}
	if tableRecorder.lastMouse != nil {
		t.Fatal("table should not receive drag events while details pane has capture")
	}
	if detailsRecorder.lastMouse.Y != 5 {
		t.Fatalf("got local Y %d want 5", detailsRecorder.lastMouse.Y)
	}
}
