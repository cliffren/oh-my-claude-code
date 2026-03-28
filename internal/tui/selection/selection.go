package selection

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

type Point struct {
	Line int
	Col  int
}

type Region struct {
	X      int
	Y      int
	Width  int
	Height int
}

type ScrollbarThumb struct {
	Offset int
	Size   int
}

type Controller struct {
	active bool
	anchor Point
	end    Point
}

type ClipboardWriter interface {
	Write(text string) error
}

type ClipboardFunc func(text string) error

func (f ClipboardFunc) Write(text string) error {
	return f(text)
}

func NormalizeBounds(start, end Point) (Point, Point) {
	if end.Line < start.Line || (end.Line == start.Line && end.Col < start.Col) {
		return end, start
	}
	return start, end
}

func ExtractText(lines []string, start, end Point) string {
	start, end = NormalizeBounds(start, end)
	if len(lines) == 0 || start.Line >= len(lines) || end.Line < 0 {
		return ""
	}
	start.Line = max(0, start.Line)
	end.Line = min(len(lines)-1, end.Line)

	parts := make([]string, 0, end.Line-start.Line+1)
	for lineIdx := start.Line; lineIdx <= end.Line; lineIdx++ {
		line := lines[lineIdx]
		left := 0
		right := ansi.StringWidth(line)
		if lineIdx == start.Line {
			left = clamp(start.Col, 0, right)
		}
		if lineIdx == end.Line {
			right = clamp(end.Col, 0, right)
		}
		if right < left {
			right = left
		}
		parts = append(parts, ansi.Cut(line, left, right))
	}
	return strings.Join(parts, "\n")
}

func HitTestRegion(region Region, x, y int) (Point, bool) {
	if region.Width <= 0 || region.Height <= 0 {
		return Point{}, false
	}
	if x < region.X || x >= region.X+region.Width || y < region.Y || y >= region.Y+region.Height {
		return Point{}, false
	}
	return Point{Line: y - region.Y, Col: x - region.X}, true
}

func ClampPoint(region Region, x, y int) Point {
	line := clamp(y-region.Y, 0, max(0, region.Height-1))
	col := clamp(x-region.X, 0, max(0, region.Width))
	return Point{Line: line, Col: col}
}

func (c *Controller) HandleMouse(msg tea.MouseMsg, region Region, lines []string, writer ClipboardWriter) (bool, string, bool, error) {
	switch msg.Action {
	case tea.MouseActionPress:
		if msg.Button != tea.MouseButtonLeft {
			return c.active, "", false, nil
		}
		point, ok := HitTestRegion(region, msg.X, msg.Y)
		if !ok {
			c.Clear()
			return false, "", false, nil
		}
		c.active = true
		c.anchor = point
		c.end = point
		return true, "", false, nil
	case tea.MouseActionMotion:
		if !c.active {
			return false, "", false, nil
		}
		c.end = ClampPoint(region, msg.X, msg.Y)
		return true, "", false, nil
	case tea.MouseActionRelease:
		if !c.active {
			return false, "", false, nil
		}
		c.end = ClampPoint(region, msg.X, msg.Y)
		selected := ExtractText(lines, c.anchor, c.end)
		c.active = false
		if selected == "" {
			c.Clear()
			return false, "", false, nil
		}
		if writer != nil {
			if err := writer.Write(selected); err != nil {
				return false, selected, false, err
			}
		}
		c.Clear() // clear selection highlight after copy
		return false, selected, true, nil
	}
	return c.active, "", false, nil
}

func (c Controller) HasSelection() bool {
	return c.anchor != c.end || c.active
}

func (c Controller) CapturesMouse() bool {
	return c.active
}

func (c Controller) Bounds() (Point, Point) {
	return c.anchor, c.end
}

func (c *Controller) Clear() {
	c.active = false
	c.anchor = Point{}
	c.end = Point{}
}

func HighlightLines(lines []string, start, end Point, style lipgloss.Style) []string {
	start, end = NormalizeBounds(start, end)
	out := append([]string(nil), lines...)
	if len(out) == 0 || start == end {
		return out
	}
	start.Line = max(0, start.Line)
	end.Line = min(len(out)-1, end.Line)
	for lineIdx := start.Line; lineIdx <= end.Line; lineIdx++ {
		line := out[lineIdx]
		width := ansi.StringWidth(line)
		left := 0
		right := width
		if lineIdx == start.Line {
			left = clamp(start.Col, 0, width)
		}
		if lineIdx == end.Line {
			right = clamp(end.Col, 0, width)
		}
		if right <= left {
			continue
		}
		prefix := ansi.Cut(line, 0, left)
		middle := ansi.Cut(line, left, right)
		suffix := ansi.Cut(line, right, width)
		out[lineIdx] = prefix + RenderSegment(style, middle) + suffix
	}
	return out
}

func RenderSegment(style lipgloss.Style, segment string) string {
	if segment == "" {
		return ""
	}
	plain := ansi.Strip(segment)
	_ = style
	return "\x1b[7m" + plain + "\x1b[0m"
}

func CalculateScrollbarThumb(viewHeight, contentHeight, scrollOffset int) ScrollbarThumb {
	if viewHeight <= 0 {
		return ScrollbarThumb{}
	}
	if contentHeight <= viewHeight {
		return ScrollbarThumb{Size: viewHeight}
	}
	maxOffset := contentHeight - viewHeight
	scrollOffset = clamp(scrollOffset, 0, maxOffset)
	size := max(1, int(float64(viewHeight)*float64(viewHeight)/float64(contentHeight)))
	travel := viewHeight - size
	offset := 0
	if travel > 0 && maxOffset > 0 {
		offset = int(float64(scrollOffset) / float64(maxOffset) * float64(travel))
	}
	return ScrollbarThumb{Offset: offset, Size: size}
}

func RenderScrollbar(viewHeight int, thumb ScrollbarThumb, track, handle string) []string {
	lines := make([]string, max(0, viewHeight))
	for i := range lines {
		lines[i] = track
		if i >= thumb.Offset && i < thumb.Offset+thumb.Size {
			lines[i] = handle
		}
	}
	return lines
}

func clamp(value, low, high int) int {
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}
