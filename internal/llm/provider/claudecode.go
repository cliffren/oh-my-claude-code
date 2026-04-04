package provider

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	toolsPkg "github.com/cliffren/toc/internal/llm/tools"
	"github.com/cliffren/toc/internal/logging"
	"github.com/cliffren/toc/internal/message"
)

// ---------------------------------------------------------------------------
// Types for parsing Claude Code stream-json events
// ---------------------------------------------------------------------------

type claudeEvent struct {
	Type      string         `json:"type"`
	Subtype   string         `json:"subtype,omitempty"`
	SessionID string         `json:"session_id,omitempty"`
	Event     *streamEvent   `json:"event,omitempty"`
	Message   *claudeMessage `json:"message,omitempty"`
	// Init fields (only present when type == "system", subtype == "init")
	SlashCommands     []string `json:"slash_commands,omitempty"`
	Tools             []string `json:"tools,omitempty"`
	Model             string   `json:"model,omitempty"`
	PermissionModeStr string   `json:"permissionMode,omitempty"`
	ClaudeCodeVersion string   `json:"claude_code_version,omitempty"`
	// Result fields (only present when type == "result")
	DurationMs    int64        `json:"duration_ms,omitempty"`
	DurationApiMs int64        `json:"duration_api_ms,omitempty"`
	TotalCostUsd  float64                       `json:"total_cost_usd,omitempty"`
	Usage         *claudeUsage                 `json:"usage,omitempty"`
	ModelUsage    map[string]claudeModelUsage  `json:"modelUsage,omitempty"`
	IsError       bool                         `json:"is_error,omitempty"`
	Result        string                       `json:"result,omitempty"`
	// status fields (only present when type == "system", subtype == "status")
	Status string `json:"status,omitempty"` // "compacting" or null/""
	// api_retry fields (only present when type == "system", subtype == "api_retry")
	Attempt      int    `json:"attempt,omitempty"`
	MaxRetries   int    `json:"max_retries,omitempty"`
	RetryDelayMs int    `json:"retry_delay_ms,omitempty"`
	RetryError   string `json:"error,omitempty"` // error category string, e.g. "rate_limit"
	// control_request fields
	RequestID string                    `json:"request_id,omitempty"`
	Request   *claudeControlRequestBody `json:"request,omitempty"`
}

type streamEvent struct {
	Type  string           `json:"type"`
	Delta *eventDelta      `json:"delta,omitempty"`
	Index int              `json:"index,omitempty"`
	// For error events inside stream_event
	StreamError *streamEventError `json:"error,omitempty"`
}

type streamEventError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

type eventDelta struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	Thinking string `json:"thinking,omitempty"`
}

type claudeMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"` // string or array of content blocks
	Model   string          `json:"model,omitempty"`
	Usage   *claudeUsage    `json:"usage,omitempty"`
}

type claudeContentBlock struct {
	Type  string          `json:"type"`
	Text  string          `json:"text,omitempty"`
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
	// For thinking blocks
	Thinking string `json:"thinking,omitempty"`
	// For tool_result blocks
	ToolUseID string `json:"tool_use_id,omitempty"`
	Content   any    `json:"content,omitempty"`
	IsError   bool   `json:"is_error,omitempty"`
}

type claudeUsage struct {
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens,omitempty"`
}

type claudeModelUsage struct {
	ContextWindow  int64 `json:"contextWindow,omitempty"`
	MaxOutputTokens int64 `json:"maxOutputTokens,omitempty"`
}

// ---------------------------------------------------------------------------
// Client implementation
// ---------------------------------------------------------------------------

// ClaudeCodeClient is the public type alias following the project convention.
type ClaudeCodeClient ProviderClient

// procConn holds a persistent connection to a running claude process.
// The process stays alive across turns so subsequent messages skip the
// startup overhead (hook loading, session file reads, etc.).
type procConn struct {
	cmd     *exec.Cmd
	cancel  context.CancelFunc // cancels procCtx, killing the process
	stdinCh chan []byte
	scanner *bufio.Scanner
	done    chan struct{} // closed when the process exits
}

// claudeCodeClient is the private implementation.
type claudeCodeClient struct {
	providerOptions providerClientOptions
	mu              sync.Mutex
	sessionID       string // Claude Code session ID for --resume

	// stdinCh mirrors conn.stdinCh for SendControlResponse.
	// Protected by stdinMu; nil when no process is active.
	stdinCh chan []byte
	stdinMu sync.Mutex

	// pendingInputs stores the raw tool input for each pending control_request
	// so SendControlResponse can echo it back as updatedInput.
	pendingInputs sync.Map // requestID → json.RawMessage

	// conn is the persistent process connection; nil if none is live.
	connMu sync.Mutex
	conn   *procConn
}

func newClaudeCodeClient(opts providerClientOptions) ClaudeCodeClient {
	return &claudeCodeClient{
		providerOptions: opts,
	}
}

// Close implements ProviderCloser. It kills the persistent CLI process.
func (c *claudeCodeClient) Close() {
	c.killConn()
}

// SetResumeSessionID implements SessionResumer. It pre-seeds the session ID
// so the next spawned process receives --resume and continues the prior session.
func (c *claudeCodeClient) SetResumeSessionID(id string) {
	c.mu.Lock()
	c.sessionID = id
	c.mu.Unlock()
}

// DeleteClaudeSession removes the session file for the given Claude Code
// session ID from the ~/.claude/projects/ directory tree.
func DeleteClaudeSession(claudeSessionID string) {
	if claudeSessionID == "" {
		return
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return
	}
	pattern := filepath.Join(homeDir, ".claude", "projects", "*", claudeSessionID+".jsonl")
	matches, _ := filepath.Glob(pattern)
	for _, m := range matches {
		_ = os.Remove(m)
	}
}

// claudeControlRequestBody is the "request" field inside a control_request event.
type claudeControlRequestBody struct {
	Subtype     string          `json:"subtype"`
	ToolName    string          `json:"tool_name"`
	ToolUseID   string          `json:"tool_use_id"`
	Input       json.RawMessage `json:"input"`
	BlockedPath string          `json:"blocked_path,omitempty"`
	Description string          `json:"description,omitempty"`
}

// claudeControlResponse is sent back to Claude Code via stdin.
type claudeControlResponse struct {
	Type      string                    `json:"type"`
	SessionID string                    `json:"session_id"`
	Response  *claudeControlResponseBody `json:"response"`
}

type claudeControlResponseBody struct {
	Subtype   string                    `json:"subtype"`
	RequestID string                    `json:"request_id"`
	Response  *claudeControlResponseData `json:"response,omitempty"`
	Error     string                    `json:"error,omitempty"`
}

type claudeControlResponseData struct {
	Behavior     string          `json:"behavior"`
	UpdatedInput json.RawMessage `json:"updatedInput,omitempty"`
}

// SendControlResponse writes an allow/deny response for a pending control_request
// back to the Claude Code process via stdin.
func (c *claudeCodeClient) SendControlResponse(requestID, sessionID string, allow bool) error {
	// Retrieve and remove the stored tool input for this request.
	var rawInput json.RawMessage
	if v, ok := c.pendingInputs.LoadAndDelete(requestID); ok {
		rawInput = v.(json.RawMessage)
	}

	var resp claudeControlResponse
	if allow {
		resp = claudeControlResponse{
			Type:      "control_response",
			SessionID: sessionID,
			Response: &claudeControlResponseBody{
				Subtype:   "success",
				RequestID: requestID,
				Response:  &claudeControlResponseData{Behavior: "allow", UpdatedInput: rawInput},
			},
		}
	} else {
		resp = claudeControlResponse{
			Type:      "control_response",
			SessionID: sessionID,
			Response: &claudeControlResponseBody{
				Subtype:   "error",
				RequestID: requestID,
				Error:     "Permission denied",
			},
		}
	}

	data, err := json.Marshal(resp)
	if err != nil {
		return err
	}
	data = append(data, '\n')

	// Hold the mutex for the entire nil-check + send to prevent a race where
	// stream() closes stdinCh between the nil check and the send (sending to a
	// closed channel panics).
	c.stdinMu.Lock()
	defer c.stdinMu.Unlock()

	if c.stdinCh == nil {
		return fmt.Errorf("no active Claude Code process")
	}

	select {
	case c.stdinCh <- data:
		return nil
	default:
		return fmt.Errorf("stdin channel full, response dropped")
	}
}

// buildCommand constructs the exec.Cmd for the claude CLI process.
func (c *claudeCodeClient) buildCommand(ctx context.Context) *exec.Cmd {
	args := []string{
		"--output-format", "stream-json",
		"--input-format", "stream-json",
		"--verbose",
		"--max-turns", "0", // unlimited turns
		"--model", string(c.providerOptions.model.APIModel),
		// Route tool permission prompts through stdin/stdout control protocol
		// so toc can show its own permission dialog. Without this flag the CLI
		// either auto-approves or silently denies tools.
		"--permission-prompt-tool", "stdio",
	}

	if c.providerOptions.reasoningEffort != "" && c.providerOptions.model.CanReason {
		args = append(args, "--effort", c.providerOptions.reasoningEffort)
	}

	permMode := c.providerOptions.permissionMode
	if permMode != "" && permMode != "default" {
		args = append(args, "--permission-mode", permMode)
	}

	if c.providerOptions.systemMessage != "" {
		if c.providerOptions.appendSystemMessage {
			args = append(args, "--append-system-prompt", c.providerOptions.systemMessage)
		} else {
			args = append(args, "--system-prompt", c.providerOptions.systemMessage)
		}
	}

	c.mu.Lock()
	sid := c.sessionID
	c.mu.Unlock()

	if sid != "" {
		args = append(args, "--resume", sid)
	}

	// Allow overriding the claude binary path via environment variable.
	claudePath := "claude"
	if p := os.Getenv("CLAUDE_CODE_PATH"); p != "" {
		claudePath = p
	}

	cmd := exec.CommandContext(ctx, claudePath, args...)

	// CRITICAL: Clear CLAUDECODE env var to prevent nested session detection.
	// Set CLAUDE_CODE_ENTRYPOINT to identify ourselves as an SDK consumer.
	env := os.Environ()
	filtered := make([]string, 0, len(env))
	for _, e := range env {
		if strings.HasPrefix(e, "CLAUDECODE=") {
			continue
		}
		filtered = append(filtered, e)
	}
	filtered = append(filtered, "CLAUDE_CODE_ENTRYPOINT=sdk-go")
	cmd.Env = filtered

	return cmd
}

// spawnConn starts a new claude process and returns a procConn for it.
// The process runs under its own context so it outlives individual stream calls.
func (c *claudeCodeClient) spawnConn() (*procConn, error) {
	procCtx, cancel := context.WithCancel(context.Background())
	cmd := c.buildCommand(procCtx)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start claude: %w", err)
	}

	stdinCh := make(chan []byte, 16)
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)
	done := make(chan struct{})

	// Drain stdinCh → stdin pipe until process context is cancelled.
	go func() {
		defer stdin.Close()
		for {
			select {
			case data, ok := <-stdinCh:
				if !ok {
					return
				}
				if _, err := stdin.Write(data); err != nil {
					logging.Error("failed to write to claude stdin", "error", err)
				}
			case <-procCtx.Done():
				return
			}
		}
	}()

	conn := &procConn{
		cmd:     cmd,
		cancel:  cancel,
		stdinCh: stdinCh,
		scanner: scanner,
		done:    done,
	}

	// Watch for process exit; clean up shared state when it happens.
	go func() {
		defer close(done)
		if err := cmd.Wait(); err != nil {
			logging.Debug("claude process exited", "error", err)
		}
		c.connMu.Lock()
		if c.conn == conn {
			c.conn = nil
		}
		c.connMu.Unlock()
		c.stdinMu.Lock()
		if c.stdinCh == stdinCh {
			c.stdinCh = nil
		}
		c.stdinMu.Unlock()
	}()

	// Mirror stdinCh so SendControlResponse can reach the process.
	c.stdinMu.Lock()
	c.stdinCh = stdinCh
	c.stdinMu.Unlock()

	return conn, nil
}

// getOrCreateConn returns the live connection, spawning a new process if needed.
func (c *claudeCodeClient) getOrCreateConn() (*procConn, error) {
	c.connMu.Lock()
	defer c.connMu.Unlock()

	if c.conn != nil {
		select {
		case <-c.conn.done:
			c.conn = nil // process died unexpectedly
		default:
			return c.conn, nil // reuse live process
		}
	}

	conn, err := c.spawnConn()
	if err != nil {
		return nil, err
	}
	c.conn = conn
	return conn, nil
}

// killConn cancels the live process and waits for it to exit.
func (c *claudeCodeClient) killConn() {
	c.connMu.Lock()
	conn := c.conn
	c.conn = nil
	c.connMu.Unlock()

	if conn == nil {
		return
	}

	// Nil out stdinCh BEFORE cancelling so SendControlResponse can't write
	// to a channel whose receiver goroutine is already shutting down.
	c.stdinMu.Lock()
	c.stdinCh = nil
	c.stdinMu.Unlock()

	conn.cancel()
	<-conn.done
}

// stream sends a user message to a persistent claude process and streams
// the resulting events. The process is kept alive after a successful turn
// so the next call reuses it without spawning overhead.
func (c *claudeCodeClient) stream(ctx context.Context, messages []message.Message, tools []toolsPkg.BaseTool) <-chan ProviderEvent {
	eventChan := make(chan ProviderEvent, 64)

	go func() {
		defer close(eventChan)

		conn, err := c.getOrCreateConn()
		if err != nil {
			eventChan <- ProviderEvent{Type: EventError, Error: err}
			return
		}

		// Send the user message via stdin.
		userMsg := c.extractUserPrompt(messages)
		inputMsg := map[string]any{
			"type": "user",
			"message": map[string]any{
				"role":    "user",
				"content": userMsg,
			},
			"parent_tool_use_id": nil,
		}
		msgBytes, _ := json.Marshal(inputMsg)
		msgBytes = append(msgBytes, '\n')
		select {
		case conn.stdinCh <- msgBytes:
		case <-ctx.Done():
			return
		}

		// Process events until result or error.
		// keepAlive is false if the call was cancelled mid-turn.
		keepAlive := c.processStream(ctx, conn.scanner, eventChan)
		if !keepAlive {
			c.killConn()
		}
	}()

	return eventChan
}

// extractUserPrompt finds the last user message in the conversation history.
func (c *claudeCodeClient) extractUserPrompt(messages []message.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == message.User {
			return messages[i].Content().String()
		}
	}
	return ""
}

// processStream reads newline-delimited JSON from scanner and emits
// ProviderEvent values on eventChan. Returns true if the turn completed
// normally (result received) so the caller can keep the process alive,
// or false if the context was cancelled or an error occurred.
func (c *claudeCodeClient) processStream(ctx context.Context, scanner *bufio.Scanner, eventChan chan<- ProviderEvent) bool {
	var fullContent strings.Builder
	var totalUsage TokenUsage
	hadStreamDeltas := false
	// needsSeparator is set after a tool-result round so the next text
	// segment starts on a new line instead of running into the prior text.
	needsSeparator := false
	// Track the latest API call's actual context window utilization
	// (from assistant message usage, not the cumulative result usage).
	var lastContextTokens int64

	// Track active tool calls for proper start/stop events.
	activeToolCalls := make(map[string]bool)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return false
		default:
		}

		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var event claudeEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			logging.Debug("failed to parse claude event", "line", line, "error", err)
			continue
		}

		switch event.Type {
		case "system":
			if event.Subtype == "init" {
				if event.SessionID != "" {
					c.mu.Lock()
					c.sessionID = event.SessionID
					c.mu.Unlock()
				}
				eventChan <- ProviderEvent{
					Type: EventInit,
					InitData: &InitData{
						SlashCommands:  event.SlashCommands,
						Tools:          event.Tools,
						Model:          event.Model,
						PermissionMode: event.PermissionModeStr,
						Version:        event.ClaudeCodeVersion,
						SessionID:      event.SessionID,
					},
				}
			} else if event.Subtype == "api_retry" {
				msg := fmt.Sprintf("API retry %d/%d (delay %dms, reason: %s)",
					event.Attempt, event.MaxRetries, event.RetryDelayMs, event.RetryError)
				eventChan <- ProviderEvent{
					Type:    EventWarning,
					Content: msg,
				}
			} else if event.Subtype == "status" {
				eventChan <- ProviderEvent{
					Type:    EventCompacting,
					Content: event.Status, // "compacting" = started, "" = done
				}
			}

		case "stream_event":
			if event.Event == nil {
				continue
			}
			hadStreamDeltas = true
			if needsSeparator {
				needsSeparator = false
				if fullContent.Len() > 0 && !strings.HasSuffix(fullContent.String(), "\n") {
					fullContent.WriteString("\n")
					eventChan <- ProviderEvent{Type: EventContentDelta, Content: "\n"}
				}
			}
			c.handleStreamEvent(event.Event, eventChan, &fullContent)

		case "assistant":
			if event.Message == nil {
				continue
			}
			// Track the latest API call's context window utilization.
			if event.Message.Usage != nil {
				u := event.Message.Usage
				lastContextTokens = u.InputTokens + u.CacheCreationInputTokens + u.CacheReadInputTokens
			}
			c.handleAssistantMessage(event.Message, eventChan, activeToolCalls, &fullContent, hadStreamDeltas)

		case "user":
			if event.Message == nil {
				continue
			}
			c.handleToolResults(event.Message, eventChan, activeToolCalls)
			needsSeparator = true

		case "control_request":
			if event.Request != nil && event.Request.Subtype == "can_use_tool" {
				var inputMap map[string]any
				if len(event.Request.Input) > 0 {
					_ = json.Unmarshal(event.Request.Input, &inputMap)
				}
				// Store raw input so SendControlResponse can echo it as updatedInput.
				if event.RequestID != "" && len(event.Request.Input) > 0 {
					c.pendingInputs.Store(event.RequestID, event.Request.Input)
				}
				eventChan <- ProviderEvent{
					Type: EventPermissionRequest,
					PermissionRequest: &ProviderPermissionRequest{
						RequestID:   event.RequestID,
						SessionID:   event.SessionID,
						ToolName:    event.Request.ToolName,
						Input:       inputMap,
						BlockedPath: event.Request.BlockedPath,
						Description: event.Request.Description,
					},
				}
			}

		case "result":
			if event.Usage != nil {
				totalUsage = TokenUsage{
					InputTokens:         event.Usage.InputTokens,
					OutputTokens:        event.Usage.OutputTokens,
					CacheCreationTokens: event.Usage.CacheCreationInputTokens,
					CacheReadTokens:     event.Usage.CacheReadInputTokens,
					ContextTokens:       lastContextTokens,
				}
			}
			// Extract the real context window from modelUsage (varies by plan).
			for _, mu := range event.ModelUsage {
				if mu.ContextWindow > 0 {
					totalUsage.ContextWindow = mu.ContextWindow
					break
				}
			}
			if event.SessionID != "" {
				c.mu.Lock()
				c.sessionID = event.SessionID
				c.mu.Unlock()
			}
			if event.IsError {
				errMsg := event.Result
				if errMsg == "" {
					errMsg = "claude code reported an error result"
				}
				eventChan <- ProviderEvent{
					Type:  EventError,
					Error: errors.New(errMsg),
				}
				return false
			}
			eventChan <- ProviderEvent{
				Type: EventComplete,
				Response: &ProviderResponse{
					Content:      fullContent.String(),
					ToolCalls:    nil, // Never expose tool calls — the agent loop must NOT re-execute them
					Usage:        totalUsage,
					FinishReason: message.FinishReasonEndTurn,
					TotalCostUSD: event.TotalCostUsd,
				},
			}
			return true
		}
	}
	return false
}

// handleStreamEvent processes inner stream events from stream_event.event.
func (c *claudeCodeClient) handleStreamEvent(event *streamEvent, eventChan chan<- ProviderEvent, fullContent *strings.Builder) {
	switch event.Type {
	case "error":
		// API error surfaced mid-stream (e.g. overloaded, invalid request).
		if event.StreamError != nil {
			eventChan <- ProviderEvent{
				Type:  EventError,
				Error: fmt.Errorf("stream error (%s): %s", event.StreamError.Type, event.StreamError.Message),
			}
		}
	case "content_block_delta":
		if event.Delta == nil {
			return
		}
		switch event.Delta.Type {
		case "text_delta":
			if event.Delta.Text != "" {
				fullContent.WriteString(event.Delta.Text)
				eventChan <- ProviderEvent{
					Type:    EventContentDelta,
					Content: event.Delta.Text,
				}
			}
		case "thinking_delta":
			if event.Delta.Thinking != "" {
				eventChan <- ProviderEvent{
					Type:     EventThinkingDelta,
					Thinking: event.Delta.Thinking,
				}
			}
		}
	}
}

// handleAssistantMessage processes full assistant messages, emitting
// EventContentDelta for text blocks and EventToolUseStart for tool_use blocks.
func (c *claudeCodeClient) handleAssistantMessage(msg *claudeMessage, eventChan chan<- ProviderEvent, activeToolCalls map[string]bool, fullContent *strings.Builder, hadStreamDeltas bool) {
	blocks := c.parseContentBlocks(msg.Content)
	for _, block := range blocks {
		// Only emit text from assistant messages if no stream_event deltas
		// were received for this turn, to avoid double-counting.
		if block.Type == "text" && block.Text != "" && !hadStreamDeltas {
			text := block.Text
			// Separate text from different internal turns with a newline
			// so they don't run together in the chat display.
			if fullContent.Len() > 0 && !strings.HasSuffix(fullContent.String(), "\n") {
				text = "\n" + text
			}
			fullContent.WriteString(text)
			eventChan <- ProviderEvent{
				Type:    EventContentDelta,
				Content: text,
			}
		}
		if block.Type == "thinking" && block.Thinking != "" {
			eventChan <- ProviderEvent{
				Type:     EventThinkingDelta,
				Thinking: block.Thinking,
			}
		}
		if block.Type == "tool_use" && block.ID != "" && !activeToolCalls[block.ID] {
			activeToolCalls[block.ID] = true
			inputStr := "{}"
			if block.Input != nil {
				inputStr = string(block.Input)
			}
			eventChan <- ProviderEvent{
				Type: EventToolUseStart,
				ToolCall: &message.ToolCall{
					ID:    block.ID,
					Name:  block.Name,
					Input: inputStr,
					Type:  "tool_use",
				},
			}
		}
	}
}

// handleToolResults processes user messages containing tool_result blocks,
// emitting EventToolUseStop for each completed tool call.
func (c *claudeCodeClient) handleToolResults(msg *claudeMessage, eventChan chan<- ProviderEvent, activeToolCalls map[string]bool) {
	blocks := c.parseContentBlocks(msg.Content)
	for _, block := range blocks {
		if block.Type == "tool_result" && block.ToolUseID != "" {
			delete(activeToolCalls, block.ToolUseID)

			// Extract tool result content
			resultContent := extractToolResultContent(block.Content)

			eventChan <- ProviderEvent{
				Type:    EventToolUseStop,
				Content: resultContent,
				ToolCall: &message.ToolCall{
					ID:       block.ToolUseID,
					Finished: true,
				},
			}
		}
	}
}

// extractToolResultContent extracts text from a tool_result content field,
// which can be a string, an array of content blocks, or nil.
func extractToolResultContent(content any) string {
	if content == nil {
		return ""
	}
	switch v := content.(type) {
	case string:
		return v
	case []any:
		var parts []string
		for _, item := range v {
			if m, ok := item.(map[string]any); ok {
				if t, ok := m["text"].(string); ok {
					parts = append(parts, t)
				}
			}
		}
		return strings.Join(parts, "\n")
	default:
		// Try JSON marshal as fallback
		b, err := json.Marshal(content)
		if err != nil {
			return fmt.Sprintf("%v", content)
		}
		return string(b)
	}
}

// parseContentBlocks unmarshals content as either a JSON array of content
// blocks or a plain string.
func (c *claudeCodeClient) parseContentBlocks(raw json.RawMessage) []claudeContentBlock {
	var blocks []claudeContentBlock
	if err := json.Unmarshal(raw, &blocks); err == nil {
		return blocks
	}
	// Fall back to plain string content.
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return []claudeContentBlock{{Type: "text", Text: s}}
	}
	return nil
}

// send collects all events from stream into a single synchronous response.
func (c *claudeCodeClient) send(ctx context.Context, messages []message.Message, tools []toolsPkg.BaseTool) (*ProviderResponse, error) {
	ch := c.stream(ctx, messages, tools)

	var response *ProviderResponse
	var lastErr error
	var content strings.Builder

	for event := range ch {
		switch event.Type {
		case EventContentDelta:
			content.WriteString(event.Content)
		case EventError:
			lastErr = event.Error
		case EventComplete:
			response = event.Response
		}
	}

	if lastErr != nil {
		return nil, lastErr
	}
	if response == nil {
		return &ProviderResponse{
			Content:      content.String(),
			FinishReason: message.FinishReasonEndTurn,
		}, nil
	}
	return response, nil
}
