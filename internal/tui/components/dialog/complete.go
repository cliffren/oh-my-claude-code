package dialog

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/cliffren/toc/internal/logging"
	utilComponents "github.com/cliffren/toc/internal/tui/components/util"
	"github.com/cliffren/toc/internal/tui/layout"
	"github.com/cliffren/toc/internal/tui/styles"
	"github.com/cliffren/toc/internal/tui/theme"
	"github.com/cliffren/toc/internal/tui/util"
)

type CompletionItem struct {
	title string
	Title string
	Value string
}

type CompletionItemI interface {
	utilComponents.SimpleListItem
	GetValue() string
	DisplayValue() string
}

func (ci *CompletionItem) Render(selected bool, width int) string {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()

	itemStyle := baseStyle.
		Width(width).
		Padding(0, 1)

	if selected {
		itemStyle = itemStyle.
			Background(t.Background()).
			Foreground(t.Primary()).
			Bold(true)
	}

	title := itemStyle.Render(
		ci.GetValue(),
	)

	return title
}

func (ci *CompletionItem) DisplayValue() string {
	return ci.Title
}

func (ci *CompletionItem) GetValue() string {
	return ci.Value
}

func NewCompletionItem(completionItem CompletionItem) CompletionItemI {
	return &completionItem
}

type CompletionProvider interface {
	GetId() string
	GetEntry() CompletionItemI
	GetChildEntries(query string) ([]CompletionItemI, error)
}

type CompletionSelectedMsg struct {
	SearchString    string
	CompletionValue string
}

type CompletionDialogCompleteItemMsg struct {
	Value string
}

type CompletionDialogCloseMsg struct{}

type CompletionDialog interface {
	tea.Model
	layout.Bindings
	SetWidth(width int)
}

type completionDialogCmp struct {
	query                string
	searchString         string // actual text typed by user, used as SearchString in CompletionSelectedMsg
	completionProvider   CompletionProvider
	width                int
	height               int
	pseudoSearchTextArea textarea.Model
	listView             utilComponents.SimpleList[CompletionItemI]
}

type completionDialogKeyMap struct {
	Complete key.Binding
	Cancel   key.Binding
}

var completionDialogKeys = completionDialogKeyMap{
	Complete: key.NewBinding(
		key.WithKeys("tab", "enter"),
	),
	Cancel: key.NewBinding(
		key.WithKeys(" ", "esc", "backspace"),
	),
}

func (c *completionDialogCmp) Init() tea.Cmd {
	return nil
}

func (c *completionDialogCmp) complete(item CompletionItemI) tea.Cmd {
	if c.searchString == "" {
		return nil
	}

	return tea.Batch(
		util.CmdHandler(CompletionSelectedMsg{
			SearchString:    c.searchString,
			CompletionValue: item.GetValue(),
		}),
		c.close(),
	)
}

func (c *completionDialogCmp) close() tea.Cmd {
	c.listView.SetItems([]CompletionItemI{})
	c.pseudoSearchTextArea.Reset()
	c.pseudoSearchTextArea.Blur()
	c.searchString = ""
	c.query = ""

	return util.CmdHandler(CompletionDialogCloseMsg{})
}

func (c *completionDialogCmp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if c.pseudoSearchTextArea.Focused() {

			if !key.Matches(msg, completionDialogKeys.Complete) {

				oldPseudo := c.pseudoSearchTextArea.Value()
				var cmd tea.Cmd
				c.pseudoSearchTextArea, cmd = c.pseudoSearchTextArea.Update(msg)
				cmds = append(cmds, cmd)

				var query string
				query = c.pseudoSearchTextArea.Value()
				if query != "" {
					query = query[1:]
				}

				if query != c.query {
					// Apply the same edit delta to searchString (which tracks the real
					// editor content) rather than using the pseudo-textarea value
					// directly, since the pseudo-textarea may have a navigation prefix
					// injected by directory selection that is not in the real editor.
					newPseudo := c.pseudoSearchTextArea.Value()
					if strings.HasPrefix(newPseudo, oldPseudo) {
						// Chars appended at end
						c.searchString = c.searchString + newPseudo[len(oldPseudo):]
					} else if strings.HasPrefix(oldPseudo, newPseudo) {
						// Chars deleted from end
						deleted := len(oldPseudo) - len(newPseudo)
						if len(c.searchString) >= deleted {
							c.searchString = c.searchString[:len(c.searchString)-deleted]
						}
					}
					// Complex edits (paste, mid-string delete): keep existing
					// searchString — these are rare in a completion search box.
					logging.Info("Query", query)
					items, err := c.completionProvider.GetChildEntries(query)
					if err != nil {
						logging.Error("Failed to get child entries", err)
					}

					c.listView.SetItems(items)
					c.query = query
				}

				u, cmd := c.listView.Update(msg)
				c.listView = u.(utilComponents.SimpleList[CompletionItemI])

				cmds = append(cmds, cmd)
			}

			switch {
			case key.Matches(msg, completionDialogKeys.Complete):
				item, i := c.listView.GetSelectedItem()
				if i == -1 {
					return c, nil
				}

				// Directory: navigate into it instead of completing
				if strings.HasSuffix(item.GetValue(), "/") {
					query := item.GetValue()
					c.pseudoSearchTextArea.SetValue("@" + query)
					c.query = query
					items, err := c.completionProvider.GetChildEntries(query)
					if err != nil {
						logging.Error("Failed to get child entries", err)
					}
					c.listView.SetItems(items)
					return c, nil
				}

				cmd := c.complete(item)

				return c, cmd
			case key.Matches(msg, completionDialogKeys.Cancel):
				// Only close on backspace when there are no characters left
				if msg.String() != "backspace" || len(c.pseudoSearchTextArea.Value()) <= 0 {
					return c, c.close()
				}
			}

			return c, tea.Batch(cmds...)
		} else {
			items, err := c.completionProvider.GetChildEntries("")
			if err != nil {
				logging.Error("Failed to get child entries", err)
			}

			c.listView.SetItems(items)
			c.pseudoSearchTextArea.SetValue(msg.String())
			c.searchString = msg.String()
			return c, c.pseudoSearchTextArea.Focus()
		}
	case tea.WindowSizeMsg:
		c.width = msg.Width
		c.height = msg.Height
	}

	return c, tea.Batch(cmds...)
}

func (c *completionDialogCmp) View() string {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()

	maxWidth := 40

	completions := c.listView.GetItems()

	for _, cmd := range completions {
		title := cmd.DisplayValue()
		if len(title) > maxWidth-4 {
			maxWidth = len(title) + 4
		}
	}

	c.listView.SetMaxWidth(maxWidth)

	return baseStyle.Padding(0, 0).
		Border(lipgloss.NormalBorder()).
		BorderBottom(false).
		BorderRight(false).
		BorderLeft(false).
		BorderBackground(t.Background()).
		BorderForeground(t.TextMuted()).
		Width(c.width).
		Render(c.listView.View())
}

func (c *completionDialogCmp) SetWidth(width int) {
	c.width = width
}

func (c *completionDialogCmp) BindingKeys() []key.Binding {
	return layout.KeyMapToSlice(completionDialogKeys)
}

func NewCompletionDialogCmp(completionProvider CompletionProvider, fallbackMsgs ...string) CompletionDialog {
	ti := textarea.New()

	fallbackMsg := "No file matches found"
	if len(fallbackMsgs) > 0 {
		fallbackMsg = fallbackMsgs[0]
	}

	li := utilComponents.NewSimpleList(
		[]CompletionItemI{},
		7,
		fallbackMsg,
		false,
	)

	return &completionDialogCmp{
		query:                "",
		completionProvider:   completionProvider,
		pseudoSearchTextArea: ti,
		listView:             li,
	}
}
