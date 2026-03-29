package layout

import (
	"github.com/cliffren/oh-my-claude-code/internal/tui/theme"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type Container interface {
	tea.Model
	Sizeable
	Bindings
	MouseCapturer
}
type container struct {
	width  int
	height int

	content tea.Model

	// Style options
	paddingTop    int
	paddingRight  int
	paddingBottom int
	paddingLeft   int

	borderTop    bool
	borderRight  bool
	borderBottom bool
	borderLeft   bool
	borderStyle  lipgloss.Border
}

func (c *container) Init() tea.Cmd {
	return c.content.Init()
}

func (c *container) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if mouseMsg, ok := msg.(tea.MouseMsg); ok {
		translated, hit := c.translateMouse(mouseMsg, c.CapturesMouse())
		if !hit {
			return c, nil
		}
		msg = translated
	}
	u, cmd := c.content.Update(msg)
	c.content = u
	return c, cmd
}

func (c *container) View() string {
	t := theme.CurrentTheme()
	style := lipgloss.NewStyle()
	width := c.width
	height := c.height

	style = style.Background(t.Background())

	// Apply border if any side is enabled
	if c.borderTop || c.borderRight || c.borderBottom || c.borderLeft {
		// Adjust width and height for borders
		if c.borderTop {
			height--
		}
		if c.borderBottom {
			height--
		}
		if c.borderLeft {
			width--
		}
		if c.borderRight {
			width--
		}
		style = style.Border(c.borderStyle, c.borderTop, c.borderRight, c.borderBottom, c.borderLeft)
		style = style.BorderBackground(t.Background()).BorderForeground(t.BorderNormal())
	}
	style = style.
		Width(width).
		Height(height).
		PaddingTop(c.paddingTop).
		PaddingRight(c.paddingRight).
		PaddingBottom(c.paddingBottom).
		PaddingLeft(c.paddingLeft)

	return style.Render(c.content.View())
}

func (c *container) SetSize(width, height int) tea.Cmd {
	c.width = width
	c.height = height

	// If the content implements Sizeable, adjust its size to account for padding and borders
	if sizeable, ok := c.content.(Sizeable); ok {
		// Calculate horizontal space taken by padding and borders
		horizontalSpace := c.paddingLeft + c.paddingRight
		if c.borderLeft {
			horizontalSpace++
		}
		if c.borderRight {
			horizontalSpace++
		}

		// Calculate vertical space taken by padding and borders
		verticalSpace := c.paddingTop + c.paddingBottom
		if c.borderTop {
			verticalSpace++
		}
		if c.borderBottom {
			verticalSpace++
		}

		// Set content size with adjusted dimensions
		contentWidth := max(0, width-horizontalSpace)
		contentHeight := max(0, height-verticalSpace)
		return sizeable.SetSize(contentWidth, contentHeight)
	}
	return nil
}

func (c *container) GetSize() (int, int) {
	return c.width, c.height
}

func (c *container) translateMouse(msg tea.MouseMsg, capture bool) (tea.MouseMsg, bool) {
	xOffset := c.paddingLeft
	yOffset := c.paddingTop
	contentWidth := max(0, c.width-c.paddingLeft-c.paddingRight)
	contentHeight := max(0, c.height-c.paddingTop-c.paddingBottom)

	if c.borderLeft {
		xOffset++
		contentWidth--
	}
	if c.borderRight {
		contentWidth--
	}
	if c.borderTop {
		yOffset++
		contentHeight--
	}
	if c.borderBottom {
		contentHeight--
	}

	if !capture && (msg.X < xOffset || msg.X >= xOffset+contentWidth || msg.Y < yOffset || msg.Y >= yOffset+contentHeight) {
		return tea.MouseMsg{}, false
	}

	msg.X -= xOffset
	msg.Y -= yOffset
	return msg, true
}

func (c *container) BindingKeys() []key.Binding {
	if b, ok := c.content.(Bindings); ok {
		return b.BindingKeys()
	}
	return []key.Binding{}
}

func (c *container) CapturesMouse() bool {
	if capturer, ok := c.content.(MouseCapturer); ok {
		return capturer.CapturesMouse()
	}
	return false
}

type ContainerOption func(*container)

func NewContainer(content tea.Model, options ...ContainerOption) Container {

	c := &container{
		content:     content,
		borderStyle: lipgloss.NormalBorder(),
	}

	for _, option := range options {
		option(c)
	}

	return c
}

// Padding options
func WithPadding(top, right, bottom, left int) ContainerOption {
	return func(c *container) {
		c.paddingTop = top
		c.paddingRight = right
		c.paddingBottom = bottom
		c.paddingLeft = left
	}
}

func WithPaddingAll(padding int) ContainerOption {
	return WithPadding(padding, padding, padding, padding)
}

func WithPaddingHorizontal(padding int) ContainerOption {
	return func(c *container) {
		c.paddingLeft = padding
		c.paddingRight = padding
	}
}

func WithPaddingVertical(padding int) ContainerOption {
	return func(c *container) {
		c.paddingTop = padding
		c.paddingBottom = padding
	}
}

func WithBorder(top, right, bottom, left bool) ContainerOption {
	return func(c *container) {
		c.borderTop = top
		c.borderRight = right
		c.borderBottom = bottom
		c.borderLeft = left
	}
}

func WithBorderAll() ContainerOption {
	return WithBorder(true, true, true, true)
}

func WithBorderHorizontal() ContainerOption {
	return WithBorder(true, false, true, false)
}

func WithBorderVertical() ContainerOption {
	return WithBorder(false, true, false, true)
}

func WithBorderStyle(style lipgloss.Border) ContainerOption {
	return func(c *container) {
		c.borderStyle = style
	}
}

func WithRoundedBorder() ContainerOption {
	return WithBorderStyle(lipgloss.RoundedBorder())
}

func WithThickBorder() ContainerOption {
	return WithBorderStyle(lipgloss.ThickBorder())
}

func WithDoubleBorder() ContainerOption {
	return WithBorderStyle(lipgloss.DoubleBorder())
}
