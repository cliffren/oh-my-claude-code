package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Krontx/oh-my-claude-code/internal/app"
	"github.com/Krontx/oh-my-claude-code/internal/config"
	"github.com/Krontx/oh-my-claude-code/internal/llm/agent"
	"github.com/Krontx/oh-my-claude-code/internal/llm/provider"
	"github.com/Krontx/oh-my-claude-code/internal/logging"
	"github.com/Krontx/oh-my-claude-code/internal/permission"
	"github.com/Krontx/oh-my-claude-code/internal/pubsub"
	"github.com/Krontx/oh-my-claude-code/internal/session"
	"github.com/Krontx/oh-my-claude-code/internal/tui/components/chat"
	"github.com/Krontx/oh-my-claude-code/internal/tui/components/core"
	"github.com/Krontx/oh-my-claude-code/internal/tui/components/dialog"
	"github.com/Krontx/oh-my-claude-code/internal/tui/layout"
	"github.com/Krontx/oh-my-claude-code/internal/tui/page"
	"github.com/Krontx/oh-my-claude-code/internal/tui/styles"
	"github.com/Krontx/oh-my-claude-code/internal/tui/theme"
	"github.com/Krontx/oh-my-claude-code/internal/tui/util"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// physicalCursorPoser is implemented by pages that can report where the
// physical terminal cursor should be placed (for IME window positioning).
type physicalCursorPoser interface {
	PhysicalCursorPos(screenHeight int) (row, col int)
}

type keyMap struct {
	Logs           key.Binding
	Quit           key.Binding
	Help           key.Binding
	SwitchSession  key.Binding
	Commands       key.Binding
	Filepicker     key.Binding
	Models         key.Binding
	Effort         key.Binding
	SwitchTheme    key.Binding
	PermissionMode key.Binding
}

type startCompactSessionMsg struct{}

// reenableMouseMsg is sent on a timer to re-enable TUI mouse after it was
// temporarily disabled to allow native terminal text selection.
type reenableMouseMsg struct{}

const (
	quitKey              = "q"
	mouseSelectionTimeout = 2 * time.Second
)

func reenableMouseAfter(d time.Duration) tea.Cmd {
	return func() tea.Msg {
		time.Sleep(d)
		return reenableMouseMsg{}
	}
}

var keys = keyMap{
	Logs: key.NewBinding(
		key.WithKeys("ctrl+l"),
		key.WithHelp("ctrl+l", "logs"),
	),

	Quit: key.NewBinding(
		key.WithKeys("ctrl+c"),
		key.WithHelp("ctrl+c", "quit"),
	),
	Help: key.NewBinding(
		key.WithKeys("ctrl+_", "ctrl+h"),
		key.WithHelp("ctrl+?", "toggle help"),
	),

	SwitchSession: key.NewBinding(
		key.WithKeys("ctrl+s"),
		key.WithHelp("ctrl+s", "switch session"),
	),

	Commands: key.NewBinding(
		key.WithKeys("ctrl+k", "ctrl+p"),
		key.WithHelp("ctrl+p", "commands"),
	),
	Filepicker: key.NewBinding(
		key.WithKeys("ctrl+f"),
		key.WithHelp("ctrl+f", "select files to upload"),
	),
	Models: key.NewBinding(
		key.WithKeys("ctrl+o"),
		key.WithHelp("ctrl+o", "model selection"),
	),

	Effort: key.NewBinding(
		key.WithKeys("ctrl+x"),
		key.WithHelp("ctrl+x", "effort level"),
	),

	SwitchTheme: key.NewBinding(
		key.WithKeys("ctrl+t"),
		key.WithHelp("ctrl+t", "switch theme"),
	),

	PermissionMode: key.NewBinding(
		key.WithKeys("ctrl+\\"),
		key.WithHelp("ctrl+\\", "permission mode"),
	),
}

var helpEsc = key.NewBinding(
	key.WithKeys("?"),
	key.WithHelp("?", "toggle help"),
)

var returnKey = key.NewBinding(
	key.WithKeys("esc"),
	key.WithHelp("esc", "close"),
)

var logsKeyReturnKey = key.NewBinding(
	key.WithKeys("esc", "backspace", quitKey),
	key.WithHelp("esc/q", "go back"),
)

type appModel struct {
	width, height   int
	currentPage     page.PageID
	previousPage    page.PageID
	pages           map[page.PageID]tea.Model
	loadedPages     map[page.PageID]bool
	status          core.StatusCmp
	app             *app.App
	selectedSession session.Session
	cursorPos       *atomic.Int64 // packed (row<<32|col) shared with imeCursorWriter; nil = disabled

	showPermissions bool
	permissions     dialog.PermissionDialogCmp

	showHelp bool
	help     dialog.HelpCmp

	showQuit bool
	quit     dialog.QuitDialog

	showSessionDialog bool
	sessionDialog     dialog.SessionDialog

	showCommandDialog bool
	commandDialog     dialog.CommandDialog
	commands          []dialog.Command
	slashCommands     []dialog.Command

	showModelDialog bool
	modelDialog     dialog.ModelDialog

	showEffortDialog bool
	effortDialog     dialog.EffortDialog

	showPermissionModeDialog bool
	permissionModeDialog     dialog.PermissionModeDialog

	showInitDialog bool
	initDialog     dialog.InitDialogCmp

	showFilepicker bool
	filepicker     dialog.FilepickerCmp

	showThemeDialog bool
	themeDialog     dialog.ThemeDialog

	showMultiArgumentsDialog bool
	multiArgumentsDialog     dialog.MultiArgumentsDialogCmp

	lastCtrlC    time.Time
	mouseEnabled bool

	isCompacting      bool
	compactingMessage string
	continueSession   bool
}

func (a appModel) Init() tea.Cmd {
	var cmds []tea.Cmd
	cmd := a.pages[a.currentPage].Init()
	a.loadedPages[a.currentPage] = true
	cmds = append(cmds, cmd)
	cmd = a.status.Init()
	cmds = append(cmds, cmd)
	cmd = a.quit.Init()
	cmds = append(cmds, cmd)
	cmd = a.help.Init()
	cmds = append(cmds, cmd)
	cmd = a.sessionDialog.Init()
	cmds = append(cmds, cmd)
	cmd = a.commandDialog.Init()
	cmds = append(cmds, cmd)
	cmd = a.modelDialog.Init()
	cmds = append(cmds, cmd)
	cmd = a.initDialog.Init()
	cmds = append(cmds, cmd)
	cmd = a.filepicker.Init()
	cmds = append(cmds, cmd)
	cmd = a.themeDialog.Init()
	cmds = append(cmds, cmd)

	// Auto-continue most recent session if requested
	if a.continueSession {
		cmds = append(cmds, func() tea.Msg {
			ctx := context.Background()
			sessions, err := a.app.Sessions.List(ctx)
			if err != nil || len(sessions) == 0 {
				return nil
			}
			return chat.SessionSelectedMsg(sessions[0])
		})
	}

	// Check if we should show the init dialog
	cmds = append(cmds, func() tea.Msg {
		shouldShow, err := config.ShouldShowInitDialog()
		if err != nil {
			return util.InfoMsg{
				Type: util.InfoTypeError,
				Msg:  "Failed to check init status: " + err.Error(),
			}
		}
		return dialog.ShowInitDialogMsg{Show: shouldShow}
	})

	return tea.Batch(cmds...)
}

func (a appModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		msg.Height -= 1 // Make space for the status bar
		a.width, a.height = msg.Width, msg.Height

		s, _ := a.status.Update(msg)
		a.status = s.(core.StatusCmp)
		a.pages[a.currentPage], cmd = a.pages[a.currentPage].Update(msg)
		cmds = append(cmds, cmd)

		prm, permCmd := a.permissions.Update(msg)
		a.permissions = prm.(dialog.PermissionDialogCmp)
		cmds = append(cmds, permCmd)

		help, helpCmd := a.help.Update(msg)
		a.help = help.(dialog.HelpCmp)
		cmds = append(cmds, helpCmd)

		session, sessionCmd := a.sessionDialog.Update(msg)
		a.sessionDialog = session.(dialog.SessionDialog)
		cmds = append(cmds, sessionCmd)

		command, commandCmd := a.commandDialog.Update(msg)
		a.commandDialog = command.(dialog.CommandDialog)
		cmds = append(cmds, commandCmd)

		filepicker, filepickerCmd := a.filepicker.Update(msg)
		a.filepicker = filepicker.(dialog.FilepickerCmp)
		cmds = append(cmds, filepickerCmd)

		a.initDialog.SetSize(msg.Width, msg.Height)

		if a.showMultiArgumentsDialog {
			a.multiArgumentsDialog.SetSize(msg.Width, msg.Height)
			args, argsCmd := a.multiArgumentsDialog.Update(msg)
			a.multiArgumentsDialog = args.(dialog.MultiArgumentsDialogCmp)
			cmds = append(cmds, argsCmd, a.multiArgumentsDialog.Init())
		}

		return a, tea.Batch(cmds...)
	// Status
	case util.InfoMsg:
		s, cmd := a.status.Update(msg)
		a.status = s.(core.StatusCmp)
		cmds = append(cmds, cmd)
		return a, tea.Batch(cmds...)
	case pubsub.Event[logging.LogMessage]:
		if msg.Payload.Persist {
			switch msg.Payload.Level {
			case "error":
				s, cmd := a.status.Update(util.InfoMsg{
					Type: util.InfoTypeError,
					Msg:  msg.Payload.Message,
					TTL:  msg.Payload.PersistTime,
				})
				a.status = s.(core.StatusCmp)
				cmds = append(cmds, cmd)
			case "info":
				s, cmd := a.status.Update(util.InfoMsg{
					Type: util.InfoTypeInfo,
					Msg:  msg.Payload.Message,
					TTL:  msg.Payload.PersistTime,
				})
				a.status = s.(core.StatusCmp)
				cmds = append(cmds, cmd)

			case "warn":
				s, cmd := a.status.Update(util.InfoMsg{
					Type: util.InfoTypeWarn,
					Msg:  msg.Payload.Message,
					TTL:  msg.Payload.PersistTime,
				})

				a.status = s.(core.StatusCmp)
				cmds = append(cmds, cmd)
			default:
				s, cmd := a.status.Update(util.InfoMsg{
					Type: util.InfoTypeInfo,
					Msg:  msg.Payload.Message,
					TTL:  msg.Payload.PersistTime,
				})
				a.status = s.(core.StatusCmp)
				cmds = append(cmds, cmd)
			}
		}
	case util.ClearStatusMsg:
		s, _ := a.status.Update(msg)
		a.status = s.(core.StatusCmp)

	// Permission
	case pubsub.Event[permission.PermissionRequest]:
		a.showPermissions = true
		return a, a.permissions.SetPermissions(msg.Payload)
	case dialog.PermissionResponseMsg:
		var cmd tea.Cmd
		switch msg.Action {
		case dialog.PermissionAllow:
			a.app.Permissions.Grant(msg.Permission)
		case dialog.PermissionAllowForSession:
			a.app.Permissions.GrantPersistant(msg.Permission)
		case dialog.PermissionDeny:
			a.app.Permissions.Deny(msg.Permission)
		}
		a.showPermissions = false
		return a, cmd

	case page.PageChangeMsg:
		return a, a.moveToPage(msg.ID)

	case dialog.CloseQuitMsg:
		a.showQuit = false
		return a, nil

	case util.DisableMouseForSelectionMsg:
		// Editor was clicked — disable TUI mouse so terminal handles selection.
		a.mouseEnabled = false
		return a, tea.Batch(tea.DisableMouse, reenableMouseAfter(mouseSelectionTimeout))

	case reenableMouseMsg:
		if !a.mouseEnabled {
			a.mouseEnabled = true
			return a, tea.EnableMouseCellMotion
		}
		return a, nil

	case dialog.CloseSessionDialogMsg:
		a.showSessionDialog = false
		return a, nil

	case dialog.CloseCommandDialogMsg:
		a.showCommandDialog = false
		return a, nil

	case startCompactSessionMsg:
		// Start compacting the current session
		a.isCompacting = true
		a.compactingMessage = "Starting summarization..."

		if a.selectedSession.ID == "" {
			a.isCompacting = false
			return a, util.ReportWarn("No active session to summarize")
		}

		// Start the summarization process
		return a, func() tea.Msg {
			ctx := context.Background()
			a.app.CoderAgent.Summarize(ctx, a.selectedSession.ID)
			return nil
		}

	case pubsub.Event[agent.AgentEvent]:
		payload := msg.Payload

		if payload.Type == agent.AgentEventTypeInit && payload.InitData != nil {
			a.updateSlashCommandsFromInit(payload.InitData)
			return a, nil
		}

		if payload.Error != nil {
			a.isCompacting = false
			return a, util.ReportError(payload.Error)
		}

		a.compactingMessage = payload.Progress

		if payload.Done && payload.Type == agent.AgentEventTypeSummarize {
			a.isCompacting = false
			return a, util.ReportInfo("Session summarization complete")
		} else if payload.Done && payload.Type == agent.AgentEventTypeResponse && a.selectedSession.ID != "" {
			model := a.app.CoderAgent.Model()
			contextWindow := model.ContextWindow
			tokens := a.selectedSession.CompletionTokens + a.selectedSession.PromptTokens
			if (tokens >= int64(float64(contextWindow)*0.95)) && config.Get().AutoCompact {
				return a, util.CmdHandler(startCompactSessionMsg{})
			}
		}
		// Continue listening for events
		return a, nil

	case dialog.CloseThemeDialogMsg:
		a.showThemeDialog = false
		return a, nil

	case dialog.ThemeChangedMsg:
		styles.InvalidateMarkdownCache()
		a.pages[a.currentPage], cmd = a.pages[a.currentPage].Update(msg)
		a.showThemeDialog = false
		return a, tea.Batch(cmd, util.ReportInfo("Theme changed to: "+msg.ThemeName))

	case dialog.CloseModelDialogMsg:
		a.showModelDialog = false
		return a, nil

	case dialog.ModelSelectedMsg:
		a.showModelDialog = false

		model, err := a.app.CoderAgent.Update(config.AgentCoder, msg.Model.ID)
		if err != nil {
			return a, util.ReportError(err)
		}

		return a, util.ReportInfo(fmt.Sprintf("Model changed to %s", model.Name))

	case dialog.CloseEffortDialogMsg:
		a.showEffortDialog = false
		return a, nil

	case dialog.EffortSelectedMsg:
		a.showEffortDialog = false
		if err := a.app.CoderAgent.UpdateEffort(config.AgentCoder, msg.Effort); err != nil {
			return a, util.ReportError(err)
		}
		return a, util.ReportInfo(fmt.Sprintf("Effort changed to %s", msg.Effort))

	case dialog.ClosePermissionModeDialogMsg:
		a.showPermissionModeDialog = false
		return a, nil

	case dialog.PermissionModeSelectedMsg:
		a.showPermissionModeDialog = false
		if err := a.app.CoderAgent.UpdatePermissionMode(msg.Mode); err != nil {
			return a, util.ReportError(err)
		}
		return a, tea.Batch(
			util.CmdHandler(util.PermissionModeChangedMsg{Mode: msg.Mode}),
			util.ReportInfo(fmt.Sprintf("Permission mode changed to %s", msg.Mode)),
		)

	case chat.ShowSlashCompletionMsg:
		if a.currentPage == page.ChatPage && !a.showCommandDialog {
			// Build slash menu commands: CLI commands insert into editor, TUI commands execute directly.
			slashMenuCmds := make([]dialog.Command, 0, len(a.slashCommands))
			for _, cmd := range a.slashCommands {
				menuCmd := cmd
				menuCmd.Description = ""
				if cmd.Category == "Claude Code" {
					name := strings.TrimPrefix(cmd.Title, "/")
					menuCmd.Handler = func(_ dialog.Command) tea.Cmd {
						return util.CmdHandler(chat.InsertEditorTextMsg{Text: "/" + name + " "})
					}
				}
				slashMenuCmds = append(slashMenuCmds, menuCmd)
			}
			return a, util.CmdHandler(chat.ShowSlashMenuMsg{Commands: slashMenuCmds})
		}
		return a, nil

	case chat.InternalSlashCommandMsg:
		switch msg.Command {
		case "model":
			a.showModelDialog = true
			return a, a.modelDialog.Init()
		case "sessions":
			sessions, err := a.app.Sessions.List(context.Background())
			if err != nil {
				return a, util.ReportError(err)
			}
			if len(sessions) == 0 {
				return a, util.ReportWarn("No sessions available")
			}
			a.sessionDialog.SetSessions(sessions)
			a.showSessionDialog = true
			return a, nil
		case "theme":
			a.showThemeDialog = true
			return a, a.themeDialog.Init()
		case "effort":
			a.showEffortDialog = true
			return a, a.effortDialog.Init()
		case "help":
			a.showHelp = true
			return a, nil
		case "new":
			return a, util.CmdHandler(chat.SessionClearedMsg{})
		case "permissions":
			a.permissionModeDialog.SetCurrentMode(a.app.CoderAgent.PermissionMode())
			a.showPermissionModeDialog = true
			return a, nil
		}
		return a, nil

	case dialog.ShowInitDialogMsg:
		a.showInitDialog = msg.Show
		return a, nil

	case dialog.CloseInitDialogMsg:
		a.showInitDialog = false
		if msg.Initialize {
			// Run the initialization command
			for _, cmd := range a.commands {
				if cmd.ID == "init" {
					// Mark the project as initialized
					if err := config.MarkProjectInitialized(); err != nil {
						return a, util.ReportError(err)
					}
					return a, cmd.Handler(cmd)
				}
			}
		} else {
			// Mark the project as initialized without running the command
			if err := config.MarkProjectInitialized(); err != nil {
				return a, util.ReportError(err)
			}
		}
		return a, nil

	case chat.SessionSelectedMsg:
		a.selectedSession = msg
		a.sessionDialog.SetSelectedSession(msg.ID)

	case pubsub.Event[session.Session]:
		if msg.Type == pubsub.UpdatedEvent && msg.Payload.ID == a.selectedSession.ID {
			a.selectedSession = msg.Payload
		}
		if msg.Type == pubsub.DeletedEvent {
			// Refresh session list in dialog if it's open
			if a.showSessionDialog {
				sessions, err := a.app.Sessions.List(context.Background())
				if err == nil {
					a.sessionDialog.SetSessions(sessions)
				}
			}
			// If the deleted session was the active one, clear the chat
			if msg.Payload.ID == a.selectedSession.ID {
				return a, util.CmdHandler(chat.SessionClearedMsg{})
			}
		}

	case dialog.SessionDeleteMsg:
		err := a.app.Sessions.Delete(context.Background(), msg.SessionID)
		if err != nil {
			return a, util.ReportError(err)
		}
		return a, nil

	case dialog.SessionRenameMsg:
		sess, err := a.app.Sessions.Get(context.Background(), msg.SessionID)
		if err != nil {
			return a, util.ReportError(err)
		}
		sess.Title = msg.NewTitle
		_, err = a.app.Sessions.Save(context.Background(), sess)
		if err != nil {
			return a, util.ReportError(err)
		}
		// Refresh session list in dialog
		if a.showSessionDialog {
			sessions, err := a.app.Sessions.List(context.Background())
			if err == nil {
				a.sessionDialog.SetSessions(sessions)
			}
		}
		return a, nil

	case dialog.SessionSelectedMsg:
		a.showSessionDialog = false
		if a.currentPage == page.ChatPage {
			return a, util.CmdHandler(chat.SessionSelectedMsg(msg.Session))
		}
		return a, nil

	case dialog.CommandSelectedMsg:
		a.showCommandDialog = false
		// Execute the command handler if available
		if msg.Command.Handler != nil {
			return a, msg.Command.Handler(msg.Command)
		}
		return a, util.ReportInfo("Command selected: " + msg.Command.Title)

	case dialog.ShowMultiArgumentsDialogMsg:
		// Show multi-arguments dialog
		a.multiArgumentsDialog = dialog.NewMultiArgumentsDialogCmp(msg.CommandID, msg.Content, msg.ArgNames)
		a.showMultiArgumentsDialog = true
		return a, a.multiArgumentsDialog.Init()

	case dialog.CloseMultiArgumentsDialogMsg:
		// Close multi-arguments dialog
		a.showMultiArgumentsDialog = false

		// If submitted, replace all named arguments and run the command
		if msg.Submit {
			content := msg.Content

			// Replace each named argument with its value
			for name, value := range msg.Args {
				placeholder := "$" + name
				content = strings.ReplaceAll(content, placeholder, value)
			}

			// Execute the command with arguments
			return a, util.CmdHandler(dialog.CommandRunCustomMsg{
				Content: content,
				Args:    msg.Args,
			})
		}
		return a, nil

	case tea.KeyMsg:
		// If multi-arguments dialog is open, let it handle the key press first
		if a.showMultiArgumentsDialog {
			args, cmd := a.multiArgumentsDialog.Update(msg)
			a.multiArgumentsDialog = args.(dialog.MultiArgumentsDialogCmp)
			return a, cmd
		}

		switch {

		case key.Matches(msg, keys.Quit):
			// Close any open dialogs first
			a.showHelp = false
			a.showSessionDialog = false
			a.showCommandDialog = false
			a.showModelDialog = false
			a.showEffortDialog = false
			a.showPermissionModeDialog = false
			a.showMultiArgumentsDialog = false
			if a.showFilepicker {
				a.showFilepicker = false
				a.filepicker.ToggleFilepicker(a.showFilepicker)
			}

			// If agent is busy, first Ctrl+C cancels it
			if a.app.CoderAgent.IsBusy() && a.selectedSession.ID != "" {
				a.app.CoderAgent.Cancel(a.selectedSession.ID)
				a.lastCtrlC = time.Now()
				return a, util.ReportInfo("Interrupted. Press Ctrl+C again to quit.")
			}

			// Double Ctrl+C within 2 seconds = quit immediately
			if time.Since(a.lastCtrlC) < 2*time.Second {
				return a, tea.Quit
			}

			a.lastCtrlC = time.Now()
			a.showQuit = true
			return a, nil
		case key.Matches(msg, keys.SwitchSession):
			if a.currentPage == page.ChatPage && !a.showQuit && !a.showPermissions && !a.showCommandDialog {
				// Load sessions and show the dialog
				sessions, err := a.app.Sessions.List(context.Background())
				if err != nil {
					return a, util.ReportError(err)
				}
				if len(sessions) == 0 {
					return a, util.ReportWarn("No sessions available")
				}
				a.sessionDialog.SetSessions(sessions)
				a.showSessionDialog = true
				return a, nil
			}
			return a, nil
		case key.Matches(msg, keys.Commands):
			if a.currentPage == page.ChatPage && !a.showQuit && !a.showPermissions && !a.showSessionDialog && !a.showThemeDialog && !a.showFilepicker {
				allCmds := make([]dialog.Command, len(a.commands))
				copy(allCmds, a.commands)
				allCmds = append(allCmds, a.slashCommands...)
				a.commandDialog.SetCommands(allCmds)
				a.showCommandDialog = true
				return a, nil
			}
			return a, nil
		case key.Matches(msg, keys.Models):
			if a.showModelDialog {
				a.showModelDialog = false
				return a, nil
			}
			if a.currentPage == page.ChatPage && !a.showQuit && !a.showPermissions && !a.showSessionDialog && !a.showCommandDialog {
				a.showModelDialog = true
				return a, nil
			}
			return a, nil
		case key.Matches(msg, keys.Effort):
			if a.showEffortDialog {
				a.showEffortDialog = false
				return a, nil
			}
			if a.currentPage == page.ChatPage && !a.showQuit && !a.showPermissions && !a.showSessionDialog && !a.showCommandDialog && !a.showModelDialog {
				a.showEffortDialog = true
				return a, a.effortDialog.Init()
			}
			return a, nil
		case key.Matches(msg, keys.PermissionMode):
			if a.showPermissionModeDialog {
				a.showPermissionModeDialog = false
				return a, nil
			}
			if a.currentPage == page.ChatPage && !a.showQuit && !a.showPermissions && !a.showSessionDialog && !a.showCommandDialog && !a.showModelDialog {
				a.permissionModeDialog.SetCurrentMode(a.app.CoderAgent.PermissionMode())
				a.showPermissionModeDialog = true
				return a, nil
			}
			return a, nil
		case key.Matches(msg, keys.SwitchTheme):
			if !a.showQuit && !a.showPermissions && !a.showSessionDialog && !a.showCommandDialog {
				// Show theme switcher dialog
				a.showThemeDialog = true
				// Theme list is dynamically loaded by the dialog component
				return a, a.themeDialog.Init()
			}
			return a, nil
		case key.Matches(msg, returnKey) || key.Matches(msg):
			if msg.String() == quitKey {
				if a.currentPage == page.LogsPage {
					return a, a.moveToPage(page.ChatPage)
				}
			} else if !a.filepicker.IsCWDFocused() {
				if a.showQuit {
					a.showQuit = !a.showQuit
					return a, nil
				}
				if a.showHelp {
					a.showHelp = !a.showHelp
					return a, nil
				}
				if a.showInitDialog {
					a.showInitDialog = false
					// Mark the project as initialized without running the command
					if err := config.MarkProjectInitialized(); err != nil {
						return a, util.ReportError(err)
					}
					return a, nil
				}
				if a.showFilepicker {
					a.showFilepicker = false
					a.filepicker.ToggleFilepicker(a.showFilepicker)
					return a, nil
				}
				if a.currentPage == page.LogsPage {
					return a, a.moveToPage(page.ChatPage)
				}
			}
		case key.Matches(msg, keys.Logs):
			return a, a.moveToPage(page.LogsPage)
		case key.Matches(msg, keys.Help):
			if a.showQuit {
				return a, nil
			}
			a.showHelp = !a.showHelp
			return a, nil
		case key.Matches(msg, helpEsc):
			if a.app.CoderAgent.IsBusy() {
				if a.showQuit {
					return a, nil
				}
				a.showHelp = !a.showHelp
				return a, nil
			}
		case key.Matches(msg, keys.Filepicker):
			a.showFilepicker = !a.showFilepicker
			a.filepicker.ToggleFilepicker(a.showFilepicker)
			return a, nil
		}
	default:
		f, filepickerCmd := a.filepicker.Update(msg)
		a.filepicker = f.(dialog.FilepickerCmp)
		cmds = append(cmds, filepickerCmd)

	}

	if a.showFilepicker {
		f, filepickerCmd := a.filepicker.Update(msg)
		a.filepicker = f.(dialog.FilepickerCmp)
		cmds = append(cmds, filepickerCmd)
		// Only block key messages send all other messages down
		if _, ok := msg.(tea.KeyMsg); ok {
			return a, tea.Batch(cmds...)
		}
	}

	if a.showQuit {
		q, quitCmd := a.quit.Update(msg)
		a.quit = q.(dialog.QuitDialog)
		cmds = append(cmds, quitCmd)
		// Only block key messages send all other messages down
		if _, ok := msg.(tea.KeyMsg); ok {
			return a, tea.Batch(cmds...)
		}
	}
	if a.showPermissions {
		d, permissionsCmd := a.permissions.Update(msg)
		a.permissions = d.(dialog.PermissionDialogCmp)
		cmds = append(cmds, permissionsCmd)
		// Only block key messages send all other messages down
		if _, ok := msg.(tea.KeyMsg); ok {
			return a, tea.Batch(cmds...)
		}
	}

	if a.showSessionDialog {
		d, sessionCmd := a.sessionDialog.Update(msg)
		a.sessionDialog = d.(dialog.SessionDialog)
		cmds = append(cmds, sessionCmd)
		// Only block key messages send all other messages down
		if _, ok := msg.(tea.KeyMsg); ok {
			return a, tea.Batch(cmds...)
		}
	}

	if a.showCommandDialog {
		d, commandCmd := a.commandDialog.Update(msg)
		a.commandDialog = d.(dialog.CommandDialog)
		cmds = append(cmds, commandCmd)
		// Only block key messages send all other messages down
		if _, ok := msg.(tea.KeyMsg); ok {
			return a, tea.Batch(cmds...)
		}
	}

	if a.showModelDialog {
		d, modelCmd := a.modelDialog.Update(msg)
		a.modelDialog = d.(dialog.ModelDialog)
		cmds = append(cmds, modelCmd)
		if _, ok := msg.(tea.KeyMsg); ok {
			return a, tea.Batch(cmds...)
		}
	}

	if a.showEffortDialog {
		d, effortCmd := a.effortDialog.Update(msg)
		a.effortDialog = d.(dialog.EffortDialog)
		cmds = append(cmds, effortCmd)
		if _, ok := msg.(tea.KeyMsg); ok {
			return a, tea.Batch(cmds...)
		}
	}

	if a.showPermissionModeDialog {
		d, pmCmd := a.permissionModeDialog.Update(msg)
		a.permissionModeDialog = d.(dialog.PermissionModeDialog)
		cmds = append(cmds, pmCmd)
		if _, ok := msg.(tea.KeyMsg); ok {
			return a, tea.Batch(cmds...)
		}
	}

	if a.showInitDialog {
		d, initCmd := a.initDialog.Update(msg)
		a.initDialog = d.(dialog.InitDialogCmp)
		cmds = append(cmds, initCmd)
		// Only block key messages send all other messages down
		if _, ok := msg.(tea.KeyMsg); ok {
			return a, tea.Batch(cmds...)
		}
	}

	if a.showThemeDialog {
		d, themeCmd := a.themeDialog.Update(msg)
		a.themeDialog = d.(dialog.ThemeDialog)
		cmds = append(cmds, themeCmd)
		// Only block key messages send all other messages down
		if _, ok := msg.(tea.KeyMsg); ok {
			return a, tea.Batch(cmds...)
		}
	}

	s, _ := a.status.Update(msg)
	a.status = s.(core.StatusCmp)
	a.pages[a.currentPage], cmd = a.pages[a.currentPage].Update(msg)
	cmds = append(cmds, cmd)
	return a, tea.Batch(cmds...)
}

// RegisterCommand adds a command to the command dialog
func (a *appModel) RegisterCommand(cmd dialog.Command) {
	a.commands = append(a.commands, cmd)
}

func (a *appModel) findCommand(id string) (dialog.Command, bool) {
	for _, cmd := range a.commands {
		if cmd.ID == id {
			return cmd, true
		}
	}
	return dialog.Command{}, false
}

func (a *appModel) moveToPage(pageID page.PageID) tea.Cmd {
	if a.app.CoderAgent.IsBusy() {
		// For now we don't move to any page if the agent is busy
		return util.ReportWarn("Agent is busy, please wait...")
	}

	var cmds []tea.Cmd
	if _, ok := a.loadedPages[pageID]; !ok {
		cmd := a.pages[pageID].Init()
		cmds = append(cmds, cmd)
		a.loadedPages[pageID] = true
	}
	a.previousPage = a.currentPage
	a.currentPage = pageID
	if sizable, ok := a.pages[a.currentPage].(layout.Sizeable); ok {
		cmd := sizable.SetSize(a.width, a.height)
		cmds = append(cmds, cmd)
	}

	return tea.Batch(cmds...)
}

func (a appModel) View() string {
	components := []string{
		a.pages[a.currentPage].View(),
	}

	components = append(components, a.status.View())

	appView := lipgloss.JoinVertical(lipgloss.Top, components...)

	if a.showPermissions {
		overlay := a.permissions.View()
		row := lipgloss.Height(appView) / 2
		row -= lipgloss.Height(overlay) / 2
		col := lipgloss.Width(appView) / 2
		col -= lipgloss.Width(overlay) / 2
		appView = layout.PlaceOverlay(
			col,
			row,
			overlay,
			appView,
			true,
		)
	}

	if a.showFilepicker {
		overlay := a.filepicker.View()
		row := lipgloss.Height(appView) / 2
		row -= lipgloss.Height(overlay) / 2
		col := lipgloss.Width(appView) / 2
		col -= lipgloss.Width(overlay) / 2
		appView = layout.PlaceOverlay(
			col,
			row,
			overlay,
			appView,
			true,
		)

	}

	// Show compacting status overlay
	if a.isCompacting {
		t := theme.CurrentTheme()
		style := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(t.BorderFocused()).
			BorderBackground(t.Background()).
			Padding(1, 2).
			Background(t.Background()).
			Foreground(t.Text())

		overlay := style.Render("Summarizing\n" + a.compactingMessage)
		row := lipgloss.Height(appView) / 2
		row -= lipgloss.Height(overlay) / 2
		col := lipgloss.Width(appView) / 2
		col -= lipgloss.Width(overlay) / 2
		appView = layout.PlaceOverlay(
			col,
			row,
			overlay,
			appView,
			true,
		)
	}

	if a.showHelp {
		bindings := layout.KeyMapToSlice(keys)
		if p, ok := a.pages[a.currentPage].(layout.Bindings); ok {
			bindings = append(bindings, p.BindingKeys()...)
		}
		if a.showPermissions {
			bindings = append(bindings, a.permissions.BindingKeys()...)
		}
		if a.currentPage == page.LogsPage {
			bindings = append(bindings, logsKeyReturnKey)
		}
		if !a.app.CoderAgent.IsBusy() {
			bindings = append(bindings, helpEsc)
		}
		a.help.SetBindings(bindings)

		overlay := a.help.View()
		row := lipgloss.Height(appView) / 2
		row -= lipgloss.Height(overlay) / 2
		col := lipgloss.Width(appView) / 2
		col -= lipgloss.Width(overlay) / 2
		appView = layout.PlaceOverlay(
			col,
			row,
			overlay,
			appView,
			true,
		)
	}

	if a.showQuit {
		overlay := a.quit.View()
		row := lipgloss.Height(appView) / 2
		row -= lipgloss.Height(overlay) / 2
		col := lipgloss.Width(appView) / 2
		col -= lipgloss.Width(overlay) / 2
		appView = layout.PlaceOverlay(
			col,
			row,
			overlay,
			appView,
			true,
		)
	}

	if a.showSessionDialog {
		overlay := a.sessionDialog.View()
		row := lipgloss.Height(appView) / 2
		row -= lipgloss.Height(overlay) / 2
		col := lipgloss.Width(appView) / 2
		col -= lipgloss.Width(overlay) / 2
		appView = layout.PlaceOverlay(
			col,
			row,
			overlay,
			appView,
			true,
		)
	}

	if a.showModelDialog {
		overlay := a.modelDialog.View()
		row := lipgloss.Height(appView) / 2
		row -= lipgloss.Height(overlay) / 2
		col := lipgloss.Width(appView) / 2
		col -= lipgloss.Width(overlay) / 2
		appView = layout.PlaceOverlay(
			col,
			row,
			overlay,
			appView,
			true,
		)
	}

	if a.showEffortDialog {
		overlay := a.effortDialog.View()
		row := lipgloss.Height(appView) / 2
		row -= lipgloss.Height(overlay) / 2
		col := lipgloss.Width(appView) / 2
		col -= lipgloss.Width(overlay) / 2
		appView = layout.PlaceOverlay(
			col,
			row,
			overlay,
			appView,
			true,
		)
	}

	if a.showPermissionModeDialog {
		overlay := a.permissionModeDialog.View()
		row := lipgloss.Height(appView) / 2
		row -= lipgloss.Height(overlay) / 2
		col := lipgloss.Width(appView) / 2
		col -= lipgloss.Width(overlay) / 2
		appView = layout.PlaceOverlay(
			col,
			row,
			overlay,
			appView,
			true,
		)
	}

	if a.showCommandDialog {
		overlay := a.commandDialog.View()
		row := lipgloss.Height(appView) / 2
		row -= lipgloss.Height(overlay) / 2
		col := lipgloss.Width(appView) / 2
		col -= lipgloss.Width(overlay) / 2
		appView = layout.PlaceOverlay(
			col,
			row,
			overlay,
			appView,
			true,
		)
	}

	if a.showInitDialog {
		overlay := a.initDialog.View()
		appView = layout.PlaceOverlay(
			a.width/2-lipgloss.Width(overlay)/2,
			a.height/2-lipgloss.Height(overlay)/2,
			overlay,
			appView,
			true,
		)
	}

	if a.showThemeDialog {
		overlay := a.themeDialog.View()
		row := lipgloss.Height(appView) / 2
		row -= lipgloss.Height(overlay) / 2
		col := lipgloss.Width(appView) / 2
		col -= lipgloss.Width(overlay) / 2
		appView = layout.PlaceOverlay(
			col,
			row,
			overlay,
			appView,
			true,
		)
	}

	if a.showMultiArgumentsDialog {
		overlay := a.multiArgumentsDialog.View()
		row := lipgloss.Height(appView) / 2
		row -= lipgloss.Height(overlay) / 2
		col := lipgloss.Width(appView) / 2
		col -= lipgloss.Width(overlay) / 2
		appView = layout.PlaceOverlay(
			col,
			row,
			overlay,
			appView,
			true,
		)
	}

	// Update physical cursor position (row+col) for IME candidate window positioning.
	// View() and Write() run in the same renderer goroutine, so this value is
	// always fresh when imeCursorWriter intercepts the frame's cursor escape.
	if a.cursorPos != nil {
		if p, ok := a.pages[a.currentPage].(physicalCursorPoser); ok {
			row, col := p.PhysicalCursorPos(a.height)
			a.cursorPos.Store(int64(row)<<32 | int64(col))
		}
	}

	return appView
}

type Option func(*appModel)

func WithContinueSession() Option {
	return func(m *appModel) {
		m.continueSession = true
	}
}

// WithCursorPos passes a shared atomic that the tui model updates each frame
// with the packed cursor position (row<<32|col). imeCursorWriter reads this to
// position the physical terminal cursor at the actual typing location.
func WithCursorPos(pos *atomic.Int64) Option {
	return func(m *appModel) {
		m.cursorPos = pos
	}
}

func New(app *app.App, opts ...Option) tea.Model {
	startPage := page.ChatPage
	model := &appModel{
		currentPage:   startPage,
		loadedPages:   make(map[page.PageID]bool),
		status:        core.NewStatusCmp(), // LSP disabled: was NewStatusCmp(app.LSPClients)
		help:          dialog.NewHelpCmp(),
		quit:          dialog.NewQuitCmp(),
		sessionDialog: dialog.NewSessionDialogCmp(),
		commandDialog: dialog.NewCommandDialogCmp(),
		modelDialog:   dialog.NewModelDialogCmp(),
		effortDialog:         dialog.NewEffortDialogCmp(),
		permissionModeDialog: dialog.NewPermissionModeDialog(),
		permissions:          dialog.NewPermissionDialogCmp(),
		initDialog:    dialog.NewInitDialogCmp(),
		themeDialog:   dialog.NewThemeDialogCmp(),
		app:           app,
		commands:      []dialog.Command{},
		mouseEnabled:  true,
		pages: map[page.PageID]tea.Model{
			page.ChatPage: page.NewChatPage(app),
			page.LogsPage: page.NewLogsPage(),
		},
		filepicker: dialog.NewFilepickerCmp(app),
	}

	for _, opt := range opts {
		opt(model)
	}

	model.RegisterCommand(dialog.Command{
		ID:          "init",
		Title:       "Initialize Project",
		Description: "Create/Update the CLAUDE.md memory file",
		Handler: func(cmd dialog.Command) tea.Cmd {
			prompt := `Please analyze this codebase and create a CLAUDE.md file containing:
1. Build/lint/test commands - especially for running a single test
2. Code style guidelines including imports, formatting, types, naming conventions, error handling, etc.

The file you create will be given to agentic coding agents (such as yourself) that operate in this repository. Make it about 20 lines long.
If there's already a CLAUDE.md, improve it.
If there are Cursor rules (in .cursor/rules/ or .cursorrules) or Copilot rules (in .github/copilot-instructions.md), make sure to include them.`
			return tea.Batch(
				util.CmdHandler(chat.SendMsg{
					Text: prompt,
				}),
			)
		},
	})

	model.RegisterCommand(dialog.Command{
		ID:          "compact",
		Title:       "Compact Session",
		Description: "Summarize the current session and create a new one with the summary",
		Handler: func(cmd dialog.Command) tea.Cmd {
			return func() tea.Msg {
				return startCompactSessionMsg{}
			}
		},
	})
	// Load custom commands
	customCommands, err := dialog.LoadCustomCommands()
	if err != nil {
		logging.Warn("Failed to load custom commands", "error", err)
	} else {
		for _, cmd := range customCommands {
			model.RegisterCommand(cmd)
		}
	}

	// Load slash commands: cached from previous session + internal commands.
	// Will be updated dynamically when init event arrives.
	model.slashCommands = internalSlashCommands()
	if cached := loadCachedInitData(); cached != nil {
		model.updateSlashCommandsFromInit(cached)
	}

	return model
}

func internalSlashCommands() []dialog.Command {
	return []dialog.Command{
		{ID: "model", Title: "/model", Description: "Switch AI model", Category: "TUI", Handler: func(cmd dialog.Command) tea.Cmd {
			return util.CmdHandler(chat.InternalSlashCommandMsg{Command: "model"})
		}},
		{ID: "sessions", Title: "/sessions", Description: "Switch session", Category: "TUI", Handler: func(cmd dialog.Command) tea.Cmd {
			return util.CmdHandler(chat.InternalSlashCommandMsg{Command: "sessions"})
		}},
		{ID: "theme", Title: "/theme", Description: "Switch theme", Category: "TUI", Handler: func(cmd dialog.Command) tea.Cmd {
			return util.CmdHandler(chat.InternalSlashCommandMsg{Command: "theme"})
		}},
		{ID: "effort", Title: "/effort", Description: "Set reasoning effort level", Category: "TUI", Handler: func(cmd dialog.Command) tea.Cmd {
			return util.CmdHandler(chat.InternalSlashCommandMsg{Command: "effort"})
		}},
		{ID: "new", Title: "/new", Description: "Start a new session", Category: "TUI", Handler: func(cmd dialog.Command) tea.Cmd {
			return util.CmdHandler(chat.InternalSlashCommandMsg{Command: "new"})
		}},
		{ID: "help", Title: "/help", Description: "Show help", Category: "TUI", Handler: func(cmd dialog.Command) tea.Cmd {
			return util.CmdHandler(chat.InternalSlashCommandMsg{Command: "help"})
		}},
		{ID: "permissions", Title: "/permissions", Description: "Switch permission mode (ctrl+\\)", Category: "TUI", Handler: func(cmd dialog.Command) tea.Cmd {
			return util.CmdHandler(chat.InternalSlashCommandMsg{Command: "permissions"})
		}},
	}
}

var cliCommandDescriptions = map[string]string{
	"compact":            "Compact conversation context",
	"review":             "Review code changes",
	"cost":               "Show session cost and token usage",
	"context":            "Show context window usage",
	"init":               "Initialize CLAUDE.md for project",
	"pr-comments":        "Review PR comments",
	"security-review":    "Security review of code",
	"release-notes":      "Generate release notes",
	"insights":           "Show usage insights",
	"heapdump":           "Dump heap for debugging",
	"extra-usage":        "Show extra usage info",
	"update-config":      "Update Claude Code configuration",
	"debug":              "Debug current issue step by step",
	"simplify":           "Review and simplify code changes",
	"batch":              "Run batch operations on files",
	"loop":               "Run iterative refinement loop",
	"schedule":           "Schedule a recurring task",
	"claude-api":         "Work with the Claude API",
	"ui-ux-pro-max":      "UI/UX design and improvement",
	"theme-factory":      "Create or customize themes",
	"doc-coauthoring":    "Co-author documentation",
	"codebase-research":  "Deep dive into codebase structure",
	"xlsx":               "Work with Excel spreadsheets",
	"memory":             "Manage conversation memory",
	"doctor":             "Diagnose environment issues",
	"bug":                "Report a bug",
	"terminal-setup":     "Configure terminal settings",
	"login":              "Log in to Claude",
	"logout":             "Log out of Claude",
	"status":             "Show account status",
}

func (a *appModel) updateSlashCommandsFromInit(initData *provider.InitData) {
	internalIDs := map[string]bool{
		"model": true, "sessions": true, "theme": true,
		"effort": true, "new": true, "help": true,
		"permissions": true,
	}

	cmds := internalSlashCommands()

	for _, cmdName := range initData.SlashCommands {
		if internalIDs[cmdName] {
			continue
		}
		name := cmdName
		desc := cliCommandDescriptions[name]
		if desc == "" {
			desc = "claude: /" + name
		}
		cmds = append(cmds, dialog.Command{
			ID:          "cc-" + name,
			Title:       "/" + name,
			Description: desc,
			Category:    "Claude Code",
			Handler: func(cmd dialog.Command) tea.Cmd {
				return util.CmdHandler(chat.SendMsg{Text: "/" + name})
			},
		})
	}

	a.slashCommands = cmds

	// Cache for next startup
	saveCachedInitData(initData)
}

const initCacheFile = "init_cache.json"

func initCachePath() string {
	return filepath.Join(config.WorkingDirectory(), ".omcc", initCacheFile)
}

func loadCachedInitData() (result *provider.InitData) {
	defer func() {
		if r := recover(); r != nil {
			result = nil
		}
	}()
	data, err := os.ReadFile(initCachePath())
	if err != nil {
		return nil
	}
	var initData provider.InitData
	if err := json.Unmarshal(data, &initData); err != nil {
		return nil
	}
	return &initData
}

func saveCachedInitData(initData *provider.InitData) {
	path := initCachePath()
	os.MkdirAll(filepath.Dir(path), 0755)
	data, err := json.Marshal(initData)
	if err != nil {
		return
	}
	os.WriteFile(path, data, 0644)
}
