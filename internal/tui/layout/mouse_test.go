package layout

import (
	"testing"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

type mouseRecorder struct {
	lastMouse *tea.MouseMsg
	updates   int
	width     int
	height    int
	capture   bool
}

func (m *mouseRecorder) Init() tea.Cmd { return nil }

func (m *mouseRecorder) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	m.updates++
	if mouse, ok := msg.(tea.MouseMsg); ok {
		copied := mouse
		m.lastMouse = &copied
	}
	return m, nil
}

func (m *mouseRecorder) View() string { return "" }

func (m *mouseRecorder) SetSize(width, height int) tea.Cmd {
	m.width = width
	m.height = height
	return nil
}

func (m *mouseRecorder) GetSize() (int, int) { return m.width, m.height }

func (m *mouseRecorder) BindingKeys() []key.Binding { return nil }

func (m *mouseRecorder) CapturesMouse() bool { return m.capture }

func TestContainerTranslatesMouseToContentCoordinates(t *testing.T) {
	recorder := &mouseRecorder{}
	container := NewContainer(recorder, WithPadding(1, 1, 0, 1)).(*container)
	container.SetSize(20, 10)

	_, _ = container.Update(tea.MouseMsg{X: 1, Y: 1, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})

	if recorder.lastMouse == nil {
		t.Fatal("expected child to receive mouse event")
	}
	if recorder.lastMouse.X != 0 || recorder.lastMouse.Y != 0 {
		t.Fatalf("got local coords (%d,%d), want (0,0)", recorder.lastMouse.X, recorder.lastMouse.Y)
	}
}

func TestSplitPaneRoutesMouseOnlyToHoveredPanel(t *testing.T) {
	left := NewContainer(&mouseRecorder{})
	rightRecorder := &mouseRecorder{}
	right := NewContainer(rightRecorder)
	layout := NewSplitPane(WithLeftPanel(left), WithRightPanel(right)).(*splitPaneLayout)
	layout.SetSize(100, 20)

	leftRecorder := left.(*container).content.(*mouseRecorder)
	_, _ = layout.Update(tea.MouseMsg{X: 80, Y: 5, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})

	if leftRecorder.lastMouse != nil {
		t.Fatal("left panel should not receive mouse event for right-panel coordinates")
	}
	if rightRecorder.lastMouse == nil {
		t.Fatal("right panel should receive mouse event")
	}
}

func TestSplitPaneKeepsSendingDragToCapturedPanel(t *testing.T) {
	leftRecorder := &mouseRecorder{capture: true}
	rightRecorder := &mouseRecorder{}
	left := NewContainer(leftRecorder)
	right := NewContainer(rightRecorder)
	layout := NewSplitPane(WithLeftPanel(left), WithRightPanel(right)).(*splitPaneLayout)
	layout.SetSize(100, 20)

	_, _ = layout.Update(tea.MouseMsg{X: 10, Y: 5, Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft})

	if leftRecorder.lastMouse == nil {
		t.Fatal("captured panel should keep receiving drag events")
	}
	if rightRecorder.lastMouse != nil {
		t.Fatal("non-captured panel should not receive drag events")
	}
	if leftRecorder.lastMouse.X != 10 || leftRecorder.lastMouse.Y != 5 {
		t.Fatalf("got local coords (%d,%d), want (10,5)", leftRecorder.lastMouse.X, leftRecorder.lastMouse.Y)
	}

	leftRecorder.capture = false
	_, _ = layout.Update(tea.MouseMsg{X: 80, Y: 5, Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft})

	if rightRecorder.lastMouse == nil {
		t.Fatal("hovered panel should receive drag events after capture releases")
	}
	if rightRecorder.lastMouse.X != 10 || rightRecorder.lastMouse.Y != 5 {
		t.Fatalf("got right-panel local coords (%d,%d), want (10,5)", rightRecorder.lastMouse.X, rightRecorder.lastMouse.Y)
	}
}

func TestSplitPaneTranslatesMouseToCapturedRightPanel(t *testing.T) {
	left := NewContainer(&mouseRecorder{})
	rightRecorder := &mouseRecorder{capture: true}
	right := NewContainer(rightRecorder)
	layout := NewSplitPane(WithLeftPanel(left), WithRightPanel(right)).(*splitPaneLayout)
	layout.SetSize(100, 20)

	_, _ = layout.Update(tea.MouseMsg{X: 80, Y: 5, Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft})

	if rightRecorder.lastMouse == nil {
		t.Fatal("captured right panel should receive mouse events")
	}
	if rightRecorder.lastMouse.X != 10 || rightRecorder.lastMouse.Y != 5 {
		t.Fatalf("got captured right-panel local coords (%d,%d), want (10,5)", rightRecorder.lastMouse.X, rightRecorder.lastMouse.Y)
	}
}

func TestSplitPaneTranslatesMouseToCapturedBottomPanel(t *testing.T) {
	left := NewContainer(&mouseRecorder{})
	bottomRecorder := &mouseRecorder{capture: true}
	bottom := NewContainer(bottomRecorder)
	layout := NewSplitPane(WithLeftPanel(left), WithBottomPanel(bottom)).(*splitPaneLayout)
	layout.SetSize(100, 20)

	_, _ = layout.Update(tea.MouseMsg{X: 10, Y: 19, Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft})

	if bottomRecorder.lastMouse == nil {
		t.Fatal("captured bottom panel should receive mouse events")
	}
	if bottomRecorder.lastMouse.X != 10 || bottomRecorder.lastMouse.Y != 1 {
		t.Fatalf("got captured bottom-panel local coords (%d,%d), want (10,1)", bottomRecorder.lastMouse.X, bottomRecorder.lastMouse.Y)
	}
}
