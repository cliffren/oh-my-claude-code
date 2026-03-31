package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/cliffren/toc/internal/config"
	"github.com/cliffren/toc/internal/diff"
	"github.com/cliffren/toc/internal/llm/models"
	"github.com/cliffren/toc/internal/llm/prompt"
	"github.com/cliffren/toc/internal/llm/provider"
	"github.com/cliffren/toc/internal/llm/tools"
	"github.com/cliffren/toc/internal/logging"
	"github.com/cliffren/toc/internal/message"
	"github.com/cliffren/toc/internal/permission"
	"github.com/cliffren/toc/internal/pubsub"
	"github.com/cliffren/toc/internal/session"
)

// Common errors
var (
	ErrRequestCancelled = errors.New("request cancelled by user")
	ErrSessionBusy      = errors.New("session is currently processing another request")
)

// Claude Code CLI tool names (PascalCase, distinct from toc's internal lowercase constants).
const (
	ccToolEdit      = "Edit"
	ccToolMultiEdit = "MultiEdit"
	ccToolWrite     = "Write"
	ccToolCreate    = "Create"
	ccToolBash      = "Bash"
	ccToolWebFetch  = "WebFetch"
)

type AgentEventType string

const (
	AgentEventTypeError      AgentEventType = "error"
	AgentEventTypeResponse   AgentEventType = "response"
	AgentEventTypeSummarize  AgentEventType = "summarize"
	AgentEventTypeInit       AgentEventType = "init"
	AgentEventTypeCompacting AgentEventType = "compacting"
)

type AgentEvent struct {
	Type    AgentEventType
	Message message.Message
	Error   error

	// When summarizing
	SessionID string
	Progress  string
	Done      bool

	// From CLI init event
	InitData *provider.InitData
}

type Service interface {
	pubsub.Suscriber[AgentEvent]
	Model() models.Model
	Run(ctx context.Context, sessionID string, content string, attachments ...message.Attachment) (<-chan AgentEvent, error)
	Cancel(sessionID string)
	IsSessionBusy(sessionID string) bool
	IsBusy() bool
	Update(agentName config.AgentName, modelID models.ModelID) (models.Model, error)
	UpdateEffort(agentName config.AgentName, effort string) error
	PermissionMode() string
	UpdatePermissionMode(mode string) error
	Summarize(ctx context.Context, sessionID string) error
}

type agent struct {
	*pubsub.Broker[AgentEvent]
	sessions session.Service
	messages message.Service

	tools      []tools.BaseTool
	provider   provider.Provider
	permissions permission.Service

	titleProvider     provider.Provider
	summarizeProvider provider.Provider

	permissionMode string // current permission mode, default = "default"

	activeRequests sync.Map
}

func NewAgent(
	agentName config.AgentName,
	sessions session.Service,
	messages message.Service,
	agentTools []tools.BaseTool,
	permissionSvc permission.Service,
) (Service, error) {
	agentProvider, err := createAgentProvider(agentName)
	if err != nil {
		return nil, err
	}
	var titleProvider provider.Provider
	// Only generate titles for the coder agent
	if agentName == config.AgentCoder {
		titleProvider, err = createAgentProvider(config.AgentTitle)
		if err != nil {
			return nil, err
		}
	}
	var summarizeProvider provider.Provider
	if agentName == config.AgentCoder {
		summarizeProvider, err = createAgentProvider(config.AgentSummarizer)
		if err != nil {
			return nil, err
		}
	}

	agent := &agent{
		Broker:            pubsub.NewBroker[AgentEvent](),
		provider:          agentProvider,
		messages:          messages,
		sessions:          sessions,
		tools:             agentTools,
		permissions:       permissionSvc,
		titleProvider:     titleProvider,
		summarizeProvider: summarizeProvider,
		activeRequests:    sync.Map{},
	}

	return agent, nil
}

func (a *agent) Model() models.Model {
	return a.provider.Model()
}

func (a *agent) Cancel(sessionID string) {
	// Cancel regular requests
	if cancelFunc, exists := a.activeRequests.LoadAndDelete(sessionID); exists {
		if cancel, ok := cancelFunc.(context.CancelFunc); ok {
			logging.InfoPersist("Interrupted.")
			cancel()
		}
	}

	// Also check for summarize requests
	if cancelFunc, exists := a.activeRequests.LoadAndDelete(sessionID + "-summarize"); exists {
		if cancel, ok := cancelFunc.(context.CancelFunc); ok {
			logging.InfoPersist("Interrupted.")
			cancel()
		}
	}
}

func (a *agent) IsBusy() bool {
	busy := false
	a.activeRequests.Range(func(key, value interface{}) bool {
		if cancelFunc, ok := value.(context.CancelFunc); ok {
			if cancelFunc != nil {
				busy = true
				return false // Stop iterating
			}
		}
		return true // Continue iterating
	})
	return busy
}

func (a *agent) IsSessionBusy(sessionID string) bool {
	_, busy := a.activeRequests.Load(sessionID)
	return busy
}

func (a *agent) generateTitle(ctx context.Context, sessionID string, content string) error {
	if content == "" {
		return nil
	}
	session, err := a.sessions.Get(ctx, sessionID)
	if err != nil {
		return err
	}

	var title string

	if a.titleProvider != nil {
		ctx = context.WithValue(ctx, tools.SessionIDContextKey, sessionID)
		parts := []message.ContentPart{message.TextContent{Text: content}}
		response, err := a.titleProvider.SendMessages(
			ctx,
			[]message.Message{
				{
					Role:  message.User,
					Parts: parts,
				},
			},
			make([]tools.BaseTool, 0),
		)
		if err == nil && response.Content != "" {
			title = strings.TrimSpace(strings.ReplaceAll(response.Content, "\n", " "))
		}
	}

	// Fallback: use the user's message as the title if the provider
	// returned nothing or an overly long response (e.g. Claude Code CLI
	// doesn't honour the title system prompt properly).
	if title == "" || len([]rune(title)) > 80 {
		title = strings.TrimSpace(strings.ReplaceAll(content, "\n", " "))
	}

	// Hard cap at 50 characters
	runes := []rune(title)
	if len(runes) > 50 {
		title = string(runes[:47]) + "..."
	}

	if title == "" {
		return nil
	}

	session.Title = title
	_, err = a.sessions.Save(ctx, session)
	return err
}

func (a *agent) err(err error) AgentEvent {
	return AgentEvent{
		Type:  AgentEventTypeError,
		Error: err,
	}
}

func (a *agent) Run(ctx context.Context, sessionID string, content string, attachments ...message.Attachment) (<-chan AgentEvent, error) {
	if !a.provider.Model().SupportsAttachments && attachments != nil {
		attachments = nil
	}
	events := make(chan AgentEvent)
	if a.IsSessionBusy(sessionID) {
		return nil, ErrSessionBusy
	}

	genCtx, cancel := context.WithCancel(ctx)

	a.activeRequests.Store(sessionID, cancel)
	go func() {
		logging.Debug("Request started", "sessionID", sessionID)
		defer logging.RecoverPanic("agent.Run", func() {
			events <- a.err(fmt.Errorf("panic while running the agent"))
		})
		var attachmentParts []message.ContentPart
		for _, attachment := range attachments {
			attachmentParts = append(attachmentParts, message.BinaryContent{Path: attachment.FilePath, MIMEType: attachment.MimeType, Data: attachment.Content})
		}
		result := a.processGeneration(genCtx, sessionID, content, attachmentParts)
		if result.Error != nil && !errors.Is(result.Error, ErrRequestCancelled) && !errors.Is(result.Error, context.Canceled) {
			logging.ErrorPersist(result.Error.Error())
		}
		logging.Debug("Request completed", "sessionID", sessionID)
		a.activeRequests.Delete(sessionID)
		cancel()
		a.Publish(pubsub.CreatedEvent, result)
		events <- result
		close(events)
	}()
	return events, nil
}

func (a *agent) processGeneration(ctx context.Context, sessionID, content string, attachmentParts []message.ContentPart) AgentEvent {
	cfg := config.Get()
	// List existing messages; if none, start title generation asynchronously.
	msgs, err := a.messages.List(ctx, sessionID)
	if err != nil {
		return a.err(fmt.Errorf("failed to list messages: %w", err))
	}
	if len(msgs) == 0 {
		go func() {
			defer logging.RecoverPanic("agent.Run", func() {
				logging.ErrorPersist("panic while generating title")
			})
			titleErr := a.generateTitle(context.Background(), sessionID, content)
			if titleErr != nil {
				logging.ErrorPersist(fmt.Sprintf("failed to generate title: %v", titleErr))
			}
		}()
	}
	session, err := a.sessions.Get(ctx, sessionID)
	if err != nil {
		return a.err(fmt.Errorf("failed to get session: %w", err))
	}
	if session.SummaryMessageID != "" {
		summaryMsgInex := -1
		for i, msg := range msgs {
			if msg.ID == session.SummaryMessageID {
				summaryMsgInex = i
				break
			}
		}
		if summaryMsgInex != -1 {
			msgs = msgs[summaryMsgInex:]
			msgs[0].Role = message.User
		}
	}

	userMsg, err := a.createUserMessage(ctx, sessionID, content, attachmentParts)
	if err != nil {
		return a.err(fmt.Errorf("failed to create user message: %w", err))
	}
	// Append the new user message to the conversation history.
	msgHistory := append(msgs, userMsg)

	for {
		// Check for cancellation before each iteration
		select {
		case <-ctx.Done():
			return a.err(ctx.Err())
		default:
			// Continue processing
		}
		agentMessage, toolResults, err := a.streamAndHandleEvents(ctx, sessionID, msgHistory)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				agentMessage.AddFinish(message.FinishReasonCanceled)
				a.messages.Update(context.Background(), agentMessage)
				return a.err(ErrRequestCancelled)
			}
			return a.err(fmt.Errorf("failed to process events: %w", err))
		}
		if cfg.Debug {
			seqId := (len(msgHistory) + 1) / 2
			toolResultFilepath := logging.WriteToolResultsJson(sessionID, seqId, toolResults)
			logging.Info("Result", "message", agentMessage.FinishReason(), "toolResults", "{}", "filepath", toolResultFilepath)
		} else {
			logging.Info("Result", "message", agentMessage.FinishReason(), "toolResults", toolResults)
		}
		if (agentMessage.FinishReason() == message.FinishReasonToolUse) && toolResults != nil {
			// We are not done, we need to respond with the tool response
			msgHistory = append(msgHistory, agentMessage, *toolResults)
			continue
		}
		return AgentEvent{
			Type:    AgentEventTypeResponse,
			Message: agentMessage,
			Done:    true,
		}
	}
}

func (a *agent) createUserMessage(ctx context.Context, sessionID, content string, attachmentParts []message.ContentPart) (message.Message, error) {
	parts := []message.ContentPart{message.TextContent{Text: content}}
	parts = append(parts, attachmentParts...)
	return a.messages.Create(ctx, sessionID, message.CreateMessageParams{
		Role:  message.User,
		Parts: parts,
	})
}

func (a *agent) streamAndHandleEvents(ctx context.Context, sessionID string, msgHistory []message.Message) (message.Message, *message.Message, error) {
	ctx = context.WithValue(ctx, tools.SessionIDContextKey, sessionID)

	// For providers that support session resume (e.g. Claude Code CLI), seed
	// the stored Claude session ID so the subprocess can pick up where it left off.
	if resumer, ok := a.provider.(provider.SessionResumer); ok {
		if sess, err := a.sessions.Get(ctx, sessionID); err == nil && sess.ClaudeSessionID != "" {
			resumer.SetResumeSessionID(sess.ClaudeSessionID)
		}
	}

	eventChan := a.provider.StreamResponse(ctx, msgHistory, a.tools)

	assistantMsg, err := a.messages.Create(ctx, sessionID, message.CreateMessageParams{
		Role:  message.Assistant,
		Parts: []message.ContentPart{},
		Model: a.provider.Model().ID,
	})
	if err != nil {
		return assistantMsg, nil, fmt.Errorf("failed to create assistant message: %w", err)
	}

	// Add the session and message ID into the context if needed by tools.
	ctx = context.WithValue(ctx, tools.MessageIDContextKey, assistantMsg.ID)

	// Process each event in the stream.
	for event := range eventChan {
		if processErr := a.processEvent(ctx, sessionID, &assistantMsg, event); processErr != nil {
			a.finishMessage(ctx, &assistantMsg, message.FinishReasonCanceled)
			return assistantMsg, nil, processErr
		}
		if ctx.Err() != nil {
			a.finishMessage(context.Background(), &assistantMsg, message.FinishReasonCanceled)
			return assistantMsg, nil, ctx.Err()
		}
	}

	// Only execute tools locally when the provider explicitly signals tool-use
	// finish. For Claude Code CLI the tools are executed internally by the
	// subprocess; the finish reason is always EndTurn, so we skip this loop.
	if assistantMsg.FinishReason() != message.FinishReasonToolUse {
		return assistantMsg, nil, nil
	}

	toolResults := make([]message.ToolResult, len(assistantMsg.ToolCalls()))
	toolCalls := assistantMsg.ToolCalls()
	for i, toolCall := range toolCalls {
		select {
		case <-ctx.Done():
			a.finishMessage(context.Background(), &assistantMsg, message.FinishReasonCanceled)
			// Make all future tool calls cancelled
			for j := i; j < len(toolCalls); j++ {
				toolResults[j] = message.ToolResult{
					ToolCallID: toolCalls[j].ID,
					Content:    "Tool execution canceled by user",
					IsError:    true,
				}
			}
			goto out
		default:
			// Continue processing
			var tool tools.BaseTool
			for _, availableTool := range a.tools {
				if availableTool.Info().Name == toolCall.Name {
					tool = availableTool
					break
				}
				// Monkey patch for Copilot Sonnet-4 tool repetition obfuscation
				// if strings.HasPrefix(toolCall.Name, availableTool.Info().Name) &&
				// 	strings.HasPrefix(toolCall.Name, availableTool.Info().Name+availableTool.Info().Name) {
				// 	tool = availableTool
				// 	break
				// }
			}

			// Tool not found
			if tool == nil {
				toolResults[i] = message.ToolResult{
					ToolCallID: toolCall.ID,
					Content:    fmt.Sprintf("Tool not found: %s", toolCall.Name),
					IsError:    true,
				}
				continue
			}
			toolResult, toolErr := tool.Run(ctx, tools.ToolCall{
				ID:    toolCall.ID,
				Name:  toolCall.Name,
				Input: toolCall.Input,
			})
			if toolErr != nil {
				if errors.Is(toolErr, permission.ErrorPermissionDenied) {
					toolResults[i] = message.ToolResult{
						ToolCallID: toolCall.ID,
						Content:    "Permission denied",
						IsError:    true,
					}
					for j := i + 1; j < len(toolCalls); j++ {
						toolResults[j] = message.ToolResult{
							ToolCallID: toolCalls[j].ID,
							Content:    "Tool execution canceled by user",
							IsError:    true,
						}
					}
					a.finishMessage(ctx, &assistantMsg, message.FinishReasonPermissionDenied)
					break
				}
			}
			toolResults[i] = message.ToolResult{
				ToolCallID: toolCall.ID,
				Content:    toolResult.Content,
				Metadata:   toolResult.Metadata,
				IsError:    toolResult.IsError,
			}
		}
	}
out:
	if len(toolResults) == 0 {
		return assistantMsg, nil, nil
	}
	parts := make([]message.ContentPart, 0)
	for _, tr := range toolResults {
		parts = append(parts, tr)
	}
	msg, err := a.messages.Create(context.Background(), assistantMsg.SessionID, message.CreateMessageParams{
		Role:  message.Tool,
		Parts: parts,
	})
	if err != nil {
		return assistantMsg, nil, fmt.Errorf("failed to create cancelled tool message: %w", err)
	}

	return assistantMsg, &msg, err
}

func (a *agent) finishMessage(ctx context.Context, msg *message.Message, finishReson message.FinishReason) {
	msg.AddFinish(finishReson)
	_ = a.messages.Update(ctx, *msg)
}

func (a *agent) processEvent(ctx context.Context, sessionID string, assistantMsg *message.Message, event provider.ProviderEvent) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		// Continue processing.
	}

	switch event.Type {
	case provider.EventInit:
		if event.InitData != nil {
			// Persist the Claude Code session ID the first time we see it.
			// Never overwrite once set — toc sessions are one-to-one with
			// Claude Code sessions.
			if event.InitData.SessionID != "" {
				if sess, err := a.sessions.Get(ctx, sessionID); err == nil && sess.ClaudeSessionID == "" {
					sess.ClaudeSessionID = event.InitData.SessionID
					_, _ = a.sessions.Save(ctx, sess)
				}
			}
			a.Publish(pubsub.CreatedEvent, AgentEvent{
				Type:     AgentEventTypeInit,
				InitData: event.InitData,
			})
		}
		return nil
	case provider.EventThinkingDelta:
		assistantMsg.AppendReasoningContent(event.Thinking)
		return a.messages.Update(ctx, *assistantMsg)
	case provider.EventContentDelta:
		assistantMsg.AppendContent(event.Content)
		return a.messages.Update(ctx, *assistantMsg)
	case provider.EventToolUseStart:
		assistantMsg.AddToolCall(*event.ToolCall)
		return a.messages.Update(ctx, *assistantMsg)
	// TODO: see how to handle this
	// case provider.EventToolUseDelta:
	// 	tm := time.Unix(assistantMsg.UpdatedAt, 0)
	// 	assistantMsg.AppendToolCallInput(event.ToolCall.ID, event.ToolCall.Input)
	// 	if time.Since(tm) > 1000*time.Millisecond {
	// 		err := a.messages.Update(ctx, *assistantMsg)
	// 		assistantMsg.UpdatedAt = time.Now().Unix()
	// 		return err
	// 	}
	case provider.EventToolUseStop:
		assistantMsg.FinishToolCall(event.ToolCall.ID)
		if err := a.messages.Update(ctx, *assistantMsg); err != nil {
			return err
		}
		// When CLI provides tool result content, store it as a tool message
		// so the TUI can display it via findToolResponse().
		if event.Content != "" {
			_, err := a.messages.Create(ctx, sessionID, message.CreateMessageParams{
				Role: message.Tool,
				Parts: []message.ContentPart{
					message.ToolResult{
						ToolCallID: event.ToolCall.ID,
						Content:    event.Content,
						IsError:    false,
					},
				},
			})
			if err != nil {
				return err
			}
		}
		return nil
	case provider.EventWarning:
		logging.InfoPersist(event.Content)
		return nil
	case provider.EventCompacting:
		done := event.Content != "compacting"
		progress := "Compacting..."
		if done {
			progress = ""
		}
		a.Publish(pubsub.CreatedEvent, AgentEvent{
			Type:     AgentEventTypeCompacting,
			Done:     done,
			Progress: progress,
		})
		return nil
	case provider.EventError:
		if errors.Is(event.Error, context.Canceled) {
			logging.InfoPersist(fmt.Sprintf("Event processing canceled for session: %s", sessionID))
			return context.Canceled
		}
		logging.ErrorPersist(event.Error.Error())
		return event.Error
	case provider.EventPermissionRequest:
		req := event.PermissionRequest
		if req == nil || a.permissions == nil {
			return nil
		}
		cr, ok := a.provider.(provider.ControlResponder)
		if !ok {
			return nil
		}
		go func() {
			params := buildPermissionRequest(sessionID, req)
			allowed := a.permissions.Request(ctx, params)
			if err := cr.SendControlResponse(req.RequestID, req.SessionID, allowed); err != nil {
				logging.Error("failed to send control response", "error", err)
			}
		}()
		return nil

	case provider.EventComplete:
		// Only replace tool calls if the completion event provides them.
		// Some providers return an empty ToolCalls slice at completion even
		// though tool calls were streamed incrementally via EventToolUseStart/Stop.
		// Calling SetToolCalls with an empty slice would wipe the accumulated calls.
		if len(event.Response.ToolCalls) > 0 {
			assistantMsg.SetToolCalls(event.Response.ToolCalls)
		}
		assistantMsg.AddFinish(event.Response.FinishReason)
		if err := a.messages.Update(ctx, *assistantMsg); err != nil {
			return fmt.Errorf("failed to update message: %w", err)
		}
		return a.TrackUsage(ctx, sessionID, a.provider.Model(), event.Response.Usage, event.Response.TotalCostUSD)
	}

	return nil
}

func (a *agent) TrackUsage(ctx context.Context, sessionID string, model models.Model, usage provider.TokenUsage, totalCostUSD float64) error {
	sess, err := a.sessions.Get(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("failed to get session: %w", err)
	}

	var cost float64
	if totalCostUSD > 0 {
		// Use the provider-reported exact cost when available (e.g. Claude Code result event).
		cost = totalCostUSD
	} else {
		cost = model.CostPer1MInCached/1e6*float64(usage.CacheCreationTokens) +
			model.CostPer1MOutCached/1e6*float64(usage.CacheReadTokens) +
			model.CostPer1MIn/1e6*float64(usage.InputTokens) +
			model.CostPer1MOut/1e6*float64(usage.OutputTokens)
	}

	sess.Cost += cost
	// For providers that report per-call context tokens (e.g. Claude Code),
	// use those for accurate context window display. Otherwise fall back
	// to the cumulative token counts.
	if usage.ContextTokens > 0 {
		sess.PromptTokens = usage.ContextTokens
		sess.CompletionTokens = 0
	} else {
		sess.CompletionTokens = usage.OutputTokens + usage.CacheReadTokens
		sess.PromptTokens = usage.InputTokens + usage.CacheCreationTokens
	}
	if usage.ContextWindow > 0 {
		sess.ContextWindow = usage.ContextWindow
	}

	_, err = a.sessions.Save(ctx, sess)
	if err != nil {
		return fmt.Errorf("failed to save session: %w", err)
	}
	return nil
}

// closeProvider releases resources held by the current provider (e.g. a
// persistent Claude Code subprocess) before it is replaced.
func (a *agent) closeProvider() {
	if c, ok := a.provider.(provider.ProviderCloser); ok {
		c.Close()
	}
}

func (a *agent) Update(agentName config.AgentName, modelID models.ModelID) (models.Model, error) {
	if a.IsBusy() {
		return models.Model{}, fmt.Errorf("cannot change model while processing requests")
	}

	if err := config.UpdateAgentModel(agentName, modelID); err != nil {
		return models.Model{}, fmt.Errorf("failed to update config: %w", err)
	}

	p, err := createAgentProvider(agentName, provider.WithPermissionMode(a.permissionMode))
	if err != nil {
		return models.Model{}, fmt.Errorf("failed to create provider for model %s: %w", modelID, err)
	}

	a.closeProvider()
	a.provider = p

	return a.provider.Model(), nil
}

func (a *agent) UpdateEffort(agentName config.AgentName, effort string) error {
	if a.IsBusy() {
		return fmt.Errorf("cannot change effort while processing requests")
	}
	if err := config.UpdateAgentEffort(agentName, effort); err != nil {
		return err
	}
	p, err := createAgentProvider(agentName, provider.WithPermissionMode(a.permissionMode))
	if err != nil {
		return fmt.Errorf("failed to create provider with new effort: %w", err)
	}
	a.closeProvider()
	a.provider = p
	return nil
}

func (a *agent) PermissionMode() string {
	if a.permissionMode == "" {
		return "default"
	}
	return a.permissionMode
}

func (a *agent) UpdatePermissionMode(mode string) error {
	if a.IsBusy() {
		return fmt.Errorf("cannot change permission mode while processing requests")
	}
	a.permissionMode = mode
	p, err := createAgentProvider(config.AgentCoder, provider.WithPermissionMode(mode))
	if err != nil {
		return fmt.Errorf("failed to create provider with new permission mode: %w", err)
	}
	a.closeProvider()
	a.provider = p
	return nil
}

func (a *agent) Summarize(ctx context.Context, sessionID string) error {
	if a.summarizeProvider == nil {
		return fmt.Errorf("summarize provider not available")
	}

	// Check if session is busy
	if a.IsSessionBusy(sessionID) {
		return ErrSessionBusy
	}

	// Create a new context with cancellation
	summarizeCtx, cancel := context.WithCancel(ctx)

	// Store the cancel function in activeRequests to allow cancellation
	a.activeRequests.Store(sessionID+"-summarize", cancel)

	go func() {
		defer a.activeRequests.Delete(sessionID + "-summarize")
		defer cancel()
		event := AgentEvent{
			Type:     AgentEventTypeSummarize,
			Progress: "Starting summarization...",
		}

		a.Publish(pubsub.CreatedEvent, event)
		// Get all messages from the session
		msgs, err := a.messages.List(summarizeCtx, sessionID)
		if err != nil {
			event = AgentEvent{
				Type:  AgentEventTypeError,
				Error: fmt.Errorf("failed to list messages: %w", err),
				Done:  true,
			}
			a.Publish(pubsub.CreatedEvent, event)
			return
		}
		summarizeCtx = context.WithValue(summarizeCtx, tools.SessionIDContextKey, sessionID)

		if len(msgs) == 0 {
			event = AgentEvent{
				Type:  AgentEventTypeError,
				Error: fmt.Errorf("no messages to summarize"),
				Done:  true,
			}
			a.Publish(pubsub.CreatedEvent, event)
			return
		}

		event = AgentEvent{
			Type:     AgentEventTypeSummarize,
			Progress: "Analyzing conversation...",
		}
		a.Publish(pubsub.CreatedEvent, event)

		// Add a system message to guide the summarization
		summarizePrompt := "Provide a detailed but concise summary of our conversation above. Focus on information that would be helpful for continuing the conversation, including what we did, what we're doing, which files we're working on, and what we're going to do next."

		// Create a new message with the summarize prompt
		promptMsg := message.Message{
			Role:  message.User,
			Parts: []message.ContentPart{message.TextContent{Text: summarizePrompt}},
		}

		// Append the prompt to the messages
		msgsWithPrompt := append(msgs, promptMsg)

		event = AgentEvent{
			Type:     AgentEventTypeSummarize,
			Progress: "Generating summary...",
		}

		a.Publish(pubsub.CreatedEvent, event)

		// Send the messages to the summarize provider
		response, err := a.summarizeProvider.SendMessages(
			summarizeCtx,
			msgsWithPrompt,
			make([]tools.BaseTool, 0),
		)
		if err != nil {
			event = AgentEvent{
				Type:  AgentEventTypeError,
				Error: fmt.Errorf("failed to summarize: %w", err),
				Done:  true,
			}
			a.Publish(pubsub.CreatedEvent, event)
			return
		}

		summary := strings.TrimSpace(response.Content)
		if summary == "" {
			event = AgentEvent{
				Type:  AgentEventTypeError,
				Error: fmt.Errorf("empty summary returned"),
				Done:  true,
			}
			a.Publish(pubsub.CreatedEvent, event)
			return
		}
		event = AgentEvent{
			Type:     AgentEventTypeSummarize,
			Progress: "Creating new session...",
		}

		a.Publish(pubsub.CreatedEvent, event)
		oldSession, err := a.sessions.Get(summarizeCtx, sessionID)
		if err != nil {
			event = AgentEvent{
				Type:  AgentEventTypeError,
				Error: fmt.Errorf("failed to get session: %w", err),
				Done:  true,
			}

			a.Publish(pubsub.CreatedEvent, event)
			return
		}
		// Create a message in the new session with the summary
		msg, err := a.messages.Create(summarizeCtx, oldSession.ID, message.CreateMessageParams{
			Role: message.Assistant,
			Parts: []message.ContentPart{
				message.TextContent{Text: summary},
				message.Finish{
					Reason: message.FinishReasonEndTurn,
					Time:   time.Now().Unix(),
				},
			},
			Model: a.summarizeProvider.Model().ID,
		})
		if err != nil {
			event = AgentEvent{
				Type:  AgentEventTypeError,
				Error: fmt.Errorf("failed to create summary message: %w", err),
				Done:  true,
			}

			a.Publish(pubsub.CreatedEvent, event)
			return
		}
		oldSession.SummaryMessageID = msg.ID
		oldSession.CompletionTokens = response.Usage.OutputTokens
		oldSession.PromptTokens = 0
		model := a.summarizeProvider.Model()
		usage := response.Usage
		cost := model.CostPer1MInCached/1e6*float64(usage.CacheCreationTokens) +
			model.CostPer1MOutCached/1e6*float64(usage.CacheReadTokens) +
			model.CostPer1MIn/1e6*float64(usage.InputTokens) +
			model.CostPer1MOut/1e6*float64(usage.OutputTokens)
		oldSession.Cost += cost
		_, err = a.sessions.Save(summarizeCtx, oldSession)
		if err != nil {
			event = AgentEvent{
				Type:  AgentEventTypeError,
				Error: fmt.Errorf("failed to save session: %w", err),
				Done:  true,
			}
			a.Publish(pubsub.CreatedEvent, event)
		}

		event = AgentEvent{
			Type:      AgentEventTypeSummarize,
			SessionID: oldSession.ID,
			Progress:  "Summary complete",
			Done:      true,
		}
		a.Publish(pubsub.CreatedEvent, event)
		// Send final success event with the new session ID
	}()

	return nil
}

func createAgentProvider(agentName config.AgentName, extraOpts ...provider.ProviderClientOption) (provider.Provider, error) {
	cfg := config.Get()
	agentConfig, ok := cfg.Agents[agentName]
	if !ok {
		return nil, fmt.Errorf("agent %s not found", agentName)
	}
	model, ok := models.SupportedModels[agentConfig.Model]
	if !ok {
		return nil, fmt.Errorf("model %s not supported", agentConfig.Model)
	}

	providerCfg, ok := cfg.Providers[model.Provider]
	if !ok {
		return nil, fmt.Errorf("provider %s not supported", model.Provider)
	}
	if providerCfg.Disabled {
		return nil, fmt.Errorf("provider %s is not enabled", model.Provider)
	}
	maxTokens := model.DefaultMaxTokens
	if agentConfig.MaxTokens > 0 {
		maxTokens = agentConfig.MaxTokens
	}
	opts := []provider.ProviderClientOption{
		provider.WithAPIKey(providerCfg.APIKey),
		provider.WithModel(model),
		provider.WithSystemMessage(prompt.GetAgentPrompt(agentName, model.Provider)),
		provider.WithMaxTokens(maxTokens),
	}
	// For Claude Code provider: coder/task agents append to CLI's own system
	// prompt; title/summarizer agents replace it entirely.
	if model.Provider == models.ProviderClaudeCode {
		if agentName == config.AgentCoder || agentName == config.AgentTask {
			opts = append(opts, provider.WithAppendSystemMessage(true))
		}
	}
	if model.Provider == models.ProviderOpenAI || model.Provider == models.ProviderLocal && model.CanReason {
		opts = append(
			opts,
			provider.WithOpenAIOptions(
				provider.WithReasoningEffort(agentConfig.ReasoningEffort),
			),
		)
	} else if model.Provider == models.ProviderAnthropic && model.CanReason && agentName == config.AgentCoder {
		opts = append(
			opts,
			provider.WithAnthropicOptions(
				provider.WithAnthropicShouldThinkFn(provider.DefaultShouldThinkFn),
			),
		)
	} else if model.Provider == models.ProviderClaudeCode && model.CanReason {
		opts = append(opts, provider.WithEffort(agentConfig.ReasoningEffort))
	}
	opts = append(opts, extraOpts...)
	agentProvider, err := provider.NewProvider(
		model.Provider,
		opts...,
	)
	if err != nil {
		return nil, fmt.Errorf("could not create provider: %v", err)
	}

	return agentProvider, nil
}

// buildPermissionRequest converts a Claude Code control_request into a
// permission.CreatePermissionRequest, populating Params with typed structs
// so the permission dialog can render diffs/previews.
func buildPermissionRequest(sessionID string, req *provider.ProviderPermissionRequest) permission.CreatePermissionRequest {
	inp := req.Input
	path := req.BlockedPath
	if path == "" {
		if fp, ok := inp["file_path"].(string); ok {
			path = fp
		}
	}

	base := permission.CreatePermissionRequest{
		SessionID:   sessionID,
		ToolName:    req.ToolName,
		Action:      "run",
		Description: req.Description,
		Path:        path,
	}

	switch req.ToolName {
	case ccToolEdit, ccToolMultiEdit:
		filePath, _ := inp["file_path"].(string)
		oldStr, _ := inp["old_string"].(string)
		newStr, _ := inp["new_string"].(string)
		d, _, _ := diff.GenerateDiff(oldStr, newStr, filePath)
		base.ToolName = tools.EditToolName
		base.Params = tools.EditPermissionsParams{FilePath: filePath, Diff: d}

	case ccToolWrite, ccToolCreate:
		filePath, _ := inp["file_path"].(string)
		content, _ := inp["content"].(string)
		oldContent := ""
		if data, err := os.ReadFile(filePath); err == nil {
			oldContent = string(data)
		}
		d, _, _ := diff.GenerateDiff(oldContent, content, filePath)
		base.ToolName = tools.WriteToolName
		base.Params = tools.WritePermissionsParams{FilePath: filePath, Diff: d}

	case ccToolBash:
		command, _ := inp["command"].(string)
		base.ToolName = tools.BashToolName
		base.Params = tools.BashPermissionsParams{Command: command}

	case ccToolWebFetch:
		url, _ := inp["url"].(string)
		base.ToolName = tools.FetchToolName
		base.Params = tools.FetchPermissionsParams{URL: url}
	}

	return base
}
