package page

import (
	"context"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/Krontx/oh-my-claude-code/internal/app"
	"github.com/Krontx/oh-my-claude-code/internal/completions"
	"github.com/Krontx/oh-my-claude-code/internal/message"
	"github.com/Krontx/oh-my-claude-code/internal/session"
	"github.com/Krontx/oh-my-claude-code/internal/tui/components/chat"
	"github.com/Krontx/oh-my-claude-code/internal/tui/components/dialog"
	"github.com/Krontx/oh-my-claude-code/internal/tui/layout"
	"github.com/Krontx/oh-my-claude-code/internal/tui/util"
)

var ChatPage PageID = "chat"

type chatPage struct {
	app                  *app.App
	editor               layout.Container
	editorCmp            chat.EditorCmp // direct ref for cursor position (always same pointer)
	messages             layout.Container
	layout               layout.SplitPaneLayout
	session              session.Session
	completionDialog     dialog.CompletionDialog
	showCompletionDialog bool
}

// PhysicalCursorPos returns the 1-indexed terminal (row, col) for the text
// cursor, used to position the physical terminal cursor so the IME candidate
// window appears next to the actual typing location.
// screenHeight is the height of the TUI content area (excluding status bar).
func (p *chatPage) PhysicalCursorPos(screenHeight int) (row, col int) {
	_, editorH := p.editor.GetSize()
	// Editor container occupies the bottom editorH rows.
	// First content row (after top border): screenHeight - editorH + 2
	// (+1 to convert 0-indexed layout rows to 1-indexed terminal rows,
	//  +1 for the top border row of the editor container)
	firstContentRow := screenHeight - editorH + 2
	row = firstContentRow + p.editorCmp.CursorRow()
	// Column layout (1-indexed): style-left-pad(1) + ">"(1) + textarea-prompt(1) + CharOffset
	// → col = 3 + CharOffset + 1 = CharOffset + 4
	col = p.editorCmp.CursorCol() + 4
	return
}

type ChatKeyMap struct {
	ShowCompletionDialog key.Binding
	NewSession           key.Binding
	Cancel               key.Binding
}

var keyMap = ChatKeyMap{
	ShowCompletionDialog: key.NewBinding(
		key.WithKeys("@"),
		key.WithHelp("@", "Complete"),
	),
	NewSession: key.NewBinding(
		key.WithKeys("ctrl+n"),
		key.WithHelp("ctrl+n", "new session"),
	),
	Cancel: key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "cancel"),
	),
}

func (p *chatPage) Init() tea.Cmd {
	cmds := []tea.Cmd{
		p.layout.Init(),
		p.completionDialog.Init(),
	}
	return tea.Batch(cmds...)
}

func (p *chatPage) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		cmd := p.layout.SetSize(msg.Width, msg.Height)
		cmds = append(cmds, cmd)
	case dialog.CompletionDialogCloseMsg:
		p.showCompletionDialog = false
	case chat.SendMsg:
		cmd := p.sendMessage(msg.Text, msg.Attachments)
		if cmd != nil {
			return p, cmd
		}
	case dialog.CommandRunCustomMsg:
		// Check if the agent is busy before executing custom commands
		if p.app.CoderAgent.IsBusy() {
			return p, util.ReportWarn("Agent is busy, please wait before executing a command...")
		}
		
		// Process the command content with arguments if any
		content := msg.Content
		if msg.Args != nil {
			// Replace all named arguments with their values
			for name, value := range msg.Args {
				placeholder := "$" + name
				content = strings.ReplaceAll(content, placeholder, value)
			}
		}
		
		// Handle custom command execution
		cmd := p.sendMessage(content, nil)
		if cmd != nil {
			return p, cmd
		}
	case chat.SessionSelectedMsg:
		if p.session.ID == "" {
			cmd := p.setSidebar()
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		p.session = msg
	case chat.SessionClearedMsg:
		p.session = session.Session{}
		cmds = append(cmds, p.clearSidebar())
		// Continue propagation to child components (list, status, etc.)
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, keyMap.ShowCompletionDialog):
			p.showCompletionDialog = true
			// Continue sending keys to layout->chat
		case key.Matches(msg, keyMap.NewSession):
			return p, util.CmdHandler(chat.SessionClearedMsg{})
		case key.Matches(msg, keyMap.Cancel):
			if p.session.ID != "" {
				// Cancel the current session's generation process
				// This allows users to interrupt long-running operations
				p.app.CoderAgent.Cancel(p.session.ID)
				return p, nil
			}
		}
	}
	if p.showCompletionDialog {
		context, contextCmd := p.completionDialog.Update(msg)
		p.completionDialog = context.(dialog.CompletionDialog)
		cmds = append(cmds, contextCmd)

		// Doesn't forward event if enter key is pressed
		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			if keyMsg.String() == "enter" {
				return p, tea.Batch(cmds...)
			}
		}
	}

	u, cmd := p.layout.Update(msg)
	cmds = append(cmds, cmd)
	p.layout = u.(layout.SplitPaneLayout)

	return p, tea.Batch(cmds...)
}

func (p *chatPage) setSidebar() tea.Cmd {
	sidebarContainer := layout.NewContainer(
		chat.NewSidebarCmp(p.session, p.app.History),
		layout.WithPadding(1, 1, 1, 1),
	)
	return tea.Batch(p.layout.SetRightPanel(sidebarContainer), sidebarContainer.Init())
}

func (p *chatPage) clearSidebar() tea.Cmd {
	return p.layout.ClearRightPanel()
}

func (p *chatPage) sendMessage(text string, attachments []message.Attachment) tea.Cmd {
	// Intercept internal slash commands
	if strings.HasPrefix(text, "/") && len(attachments) == 0 {
		cmd := strings.TrimPrefix(text, "/")
		cmd = strings.TrimSpace(cmd)
		if completions.InternalCommands[cmd] {
			return util.CmdHandler(chat.InternalSlashCommandMsg{Command: cmd})
		}
	}

	var cmds []tea.Cmd
	if p.session.ID == "" {
		session, err := p.app.Sessions.Create(context.Background(), "New Session")
		if err != nil {
			return util.ReportError(err)
		}

		p.session = session
		cmd := p.setSidebar()
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
		cmds = append(cmds, util.CmdHandler(chat.SessionSelectedMsg(session)))
	}

	_, err := p.app.CoderAgent.Run(context.Background(), p.session.ID, text, attachments...)
	if err != nil {
		return util.ReportError(err)
	}
	return tea.Batch(cmds...)
}

func (p *chatPage) SetSize(width, height int) tea.Cmd {
	return p.layout.SetSize(width, height)
}

func (p *chatPage) GetSize() (int, int) {
	return p.layout.GetSize()
}

func (p *chatPage) View() string {
	layoutView := p.layout.View()

	if p.showCompletionDialog {
		_, layoutHeight := p.layout.GetSize()
		editorWidth, editorHeight := p.editor.GetSize()

		p.completionDialog.SetWidth(editorWidth)
		overlay := p.completionDialog.View()

		layoutView = layout.PlaceOverlay(
			0,
			layoutHeight-editorHeight-lipgloss.Height(overlay),
			overlay,
			layoutView,
			false,
		)
	}

	return layoutView
}

func (p *chatPage) BindingKeys() []key.Binding {
	bindings := layout.KeyMapToSlice(keyMap)
	bindings = append(bindings, p.messages.BindingKeys()...)
	bindings = append(bindings, p.editor.BindingKeys()...)
	return bindings
}

func NewChatPage(app *app.App) tea.Model {
	cg := completions.NewFileAndFolderContextGroup()
	completionDialog := dialog.NewCompletionDialogCmp(cg)

	messagesContainer := layout.NewContainer(
		chat.NewMessagesCmp(app),
		layout.WithPadding(1, 1, 0, 1),
	)
	editorCmpInstance := chat.NewEditorCmp(app)
	editorContainer := layout.NewContainer(
		editorCmpInstance,
		layout.WithBorder(true, false, false, false),
	)
	return &chatPage{
		app:              app,
		editor:           editorContainer,
		editorCmp:        editorCmpInstance,
		messages:         messagesContainer,
		completionDialog: completionDialog,
		layout: layout.NewSplitPane(
			layout.WithLeftPanel(messagesContainer),
			layout.WithBottomPanel(editorContainer),
		),
	}
}
