package chat

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func TestNormalizeSelectionBounds(t *testing.T) {
	start := selectionPoint{Line: 8, Col: 12}
	end := selectionPoint{Line: 3, Col: 4}

	gotStart, gotEnd := normalizeSelectionBounds(start, end)

	if gotStart != (selectionPoint{Line: 3, Col: 4}) {
		t.Fatalf("unexpected start: %#v", gotStart)
	}
	if gotEnd != (selectionPoint{Line: 8, Col: 12}) {
		t.Fatalf("unexpected end: %#v", gotEnd)
	}
}

func TestExtractSelectedTextAcrossLines(t *testing.T) {
	lines := []string{"hello world", "second line", "third"}
	start := selectionPoint{Line: 0, Col: 6}
	end := selectionPoint{Line: 1, Col: 6}

	got := extractSelectedText(lines, start, end)
	want := "world\nsecond"

	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestHitTestRejectsCoordinatesOutsideMessageViewport(t *testing.T) {
	region := selectionRegion{X: 1, Y: 2, Width: 40, Height: 10}
	if _, ok := hitTestSelectionRegion(region, 60, 3); ok {
		t.Fatalf("expected hit test outside region to fail")
	}
}

func TestHitTestAcceptsZeroOriginCoordinates(t *testing.T) {
	region := selectionRegion{X: 0, Y: 0, Width: 40, Height: 10}
	point, ok := hitTestSelectionRegion(region, 0, 0)
	if !ok {
		t.Fatal("expected zero-origin point to be inside region")
	}
	if point != (selectionPoint{Line: 0, Col: 0}) {
		t.Fatalf("got %#v want origin point", point)
	}
}

func TestSelectionControllerLifecycle(t *testing.T) {
	controller := selectionController{}
	region := selectionRegion{X: 1, Y: 2, Width: 40, Height: 10}
	lines := []string{"hello world", "second line"}

	started, selected, copied, err := controller.handleMouse(tea.MouseMsg{
		X:      7,
		Y:      2,
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonLeft,
	}, region, lines, nil)
	if err != nil {
		t.Fatalf("press returned error: %v", err)
	}
	if !started || selected != "" || copied {
		t.Fatalf("unexpected press result: started=%v selected=%q copied=%v", started, selected, copied)
	}

	started, selected, copied, err = controller.handleMouse(tea.MouseMsg{
		X:      7,
		Y:      3,
		Action: tea.MouseActionMotion,
		Button: tea.MouseButtonLeft,
	}, region, lines, nil)
	if err != nil {
		t.Fatalf("motion returned error: %v", err)
	}
	if !started || selected != "" || copied {
		t.Fatalf("unexpected motion result: started=%v selected=%q copied=%v", started, selected, copied)
	}

	started, selected, copied, err = controller.handleMouse(tea.MouseMsg{
		X:      7,
		Y:      3,
		Action: tea.MouseActionRelease,
		Button: tea.MouseButtonLeft,
	}, region, lines, nil)
	if err != nil {
		t.Fatalf("release returned error: %v", err)
	}
	if started {
		t.Fatalf("selection should be finished after release")
	}
	if !copied {
		t.Fatalf("expected release to mark copied text")
	}
	if selected != "world\nsecond" {
		t.Fatalf("got %q want %q", selected, "world\nsecond")
	}
}

func TestHighlightSelectedLines(t *testing.T) {
	style := lipgloss.NewStyle().Background(lipgloss.Color("12")).Foreground(lipgloss.Color("0"))
	lines := []string{"hello world", "second line"}
	start := selectionPoint{Line: 0, Col: 6}
	end := selectionPoint{Line: 1, Col: 6}

	highlighted := highlightSelectedLines(lines, start, end, style)
	if len(highlighted) != len(lines) {
		t.Fatalf("got %d lines want %d", len(highlighted), len(lines))
	}
	if ansi.Strip(highlighted[0]) != lines[0] {
		t.Fatalf("first line text changed: %q", ansi.Strip(highlighted[0]))
	}
	if ansi.Strip(highlighted[1]) != lines[1] {
		t.Fatalf("second line text changed: %q", ansi.Strip(highlighted[1]))
	}
	if highlighted[0] == lines[0] || highlighted[1] == lines[1] {
		t.Fatalf("expected ansi styling to be applied to both lines")
	}
	if !strings.Contains(highlighted[0], "\x1b[") || !strings.Contains(highlighted[1], "\x1b[") {
		t.Fatalf("expected highlighted lines to contain ansi sequences")
	}
}

func TestRenderSelectionSegmentForcesVisibleReverseVideo(t *testing.T) {
	style := lipgloss.NewStyle().Background(lipgloss.Color("8")).Foreground(lipgloss.Color("7"))
	segment := "\x1b[31mhello\x1b[0m"

	rendered := renderSelectionSegment(style, segment)

	if !strings.Contains(rendered, "\x1b[7m") {
		t.Fatalf("expected reverse-video escape sequence, got %q", rendered)
	}
	if ansi.Strip(rendered) != "hello" {
		t.Fatalf("expected stripped content to remain hello, got %q", ansi.Strip(rendered))
	}
	if strings.Contains(rendered, "\x1b[31m") {
		t.Fatalf("expected original inline ansi style to be stripped from selected segment, got %q", rendered)
	}
	if strings.Contains(rendered, "\x1b[4m") || strings.Contains(rendered, "\x1b[1m") {
		t.Fatalf("expected no extra underline/bold styling, got %q", rendered)
	}
}

func TestScrollbarThumbMetrics(t *testing.T) {
	thumb := calculateScrollbarThumb(10, 40, 15)
	if thumb.Size <= 0 {
		t.Fatalf("expected positive thumb size, got %d", thumb.Size)
	}
	if thumb.Offset <= 0 {
		t.Fatalf("expected positive thumb offset, got %d", thumb.Offset)
	}
	if thumb.Offset+thumb.Size > 10 {
		t.Fatalf("thumb overflowed viewport: %#v", thumb)
	}
}

func TestSelectionCopiesToClipboardOnRelease(t *testing.T) {
	controller := selectionController{}
	region := selectionRegion{X: 1, Y: 2, Width: 40, Height: 10}
	lines := []string{"hello world", "second line"}
	var copied string
	writer := clipboardFunc(func(text string) error {
		copied = text
		return nil
	})

	_, _, _, _ = controller.handleMouse(tea.MouseMsg{
		X:      7,
		Y:      2,
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonLeft,
	}, region, lines, writer)
	_, _, _, _ = controller.handleMouse(tea.MouseMsg{
		X:      7,
		Y:      3,
		Action: tea.MouseActionMotion,
		Button: tea.MouseButtonLeft,
	}, region, lines, writer)
	_, selected, copiedFlag, err := controller.handleMouse(tea.MouseMsg{
		X:      7,
		Y:      3,
		Action: tea.MouseActionRelease,
		Button: tea.MouseButtonLeft,
	}, region, lines, writer)
	if err != nil {
		t.Fatalf("release returned error: %v", err)
	}
	if !copiedFlag {
		t.Fatalf("expected copied flag on release")
	}
	if copied != selected {
		t.Fatalf("copied %q want %q", copied, selected)
	}
}

func TestSelectionRemainsVisibleAfterRelease(t *testing.T) {
	controller := selectionController{}
	region := selectionRegion{X: 0, Y: 0, Width: 40, Height: 10}
	lines := []string{"hello world", "second line"}

	_, _, _, _ = controller.handleMouse(tea.MouseMsg{
		X:      6,
		Y:      0,
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonLeft,
	}, region, lines, nil)
	_, _, _, _ = controller.handleMouse(tea.MouseMsg{
		X:      6,
		Y:      1,
		Action: tea.MouseActionMotion,
		Button: tea.MouseButtonLeft,
	}, region, lines, nil)
	_, _, _, err := controller.handleMouse(tea.MouseMsg{
		X:      6,
		Y:      1,
		Action: tea.MouseActionRelease,
		Button: tea.MouseButtonLeft,
	}, region, lines, nil)
	if err != nil {
		t.Fatalf("release returned error: %v", err)
	}
	if !controller.hasSelection() {
		t.Fatal("expected selection to remain visible after release")
	}
}
