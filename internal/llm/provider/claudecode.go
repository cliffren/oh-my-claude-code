package provider

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"

	toolsPkg "github.com/Krontx/oh-my-claude-code/internal/llm/tools"
	"github.com/Krontx/oh-my-claude-code/internal/logging"
	"github.com/Krontx/oh-my-claude-code/internal/message"
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
	// Result fields (only present when type == "result")
	DurationMs    int64        `json:"duration_ms,omitempty"`
	DurationApiMs int64        `json:"duration_api_ms,omitempty"`
	TotalCostUsd  float64      `json:"total_cost_usd,omitempty"`
	Usage         *claudeUsage `json:"usage,omitempty"`
	IsError       bool         `json:"is_error,omitempty"`
	Result        string       `json:"result,omitempty"`
}

type streamEvent struct {
	Type  string      `json:"type"`
	Delta *eventDelta `json:"delta,omitempty"`
	Index int         `json:"index,omitempty"`
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

// ---------------------------------------------------------------------------
// Client implementation
// ---------------------------------------------------------------------------

// ClaudeCodeClient is the public type alias following the project convention.
type ClaudeCodeClient ProviderClient

// claudeCodeClient is the private implementation.
type claudeCodeClient struct {
	providerOptions providerClientOptions
	mu              sync.Mutex
	sessionID       string // Claude Code session ID for --resume
}

func newClaudeCodeClient(opts providerClientOptions) ClaudeCodeClient {
	return &claudeCodeClient{
		providerOptions: opts,
	}
}

// buildCommand constructs the exec.Cmd for the claude CLI process.
func (c *claudeCodeClient) buildCommand(ctx context.Context) *exec.Cmd {
	args := []string{
		"--print",
		"--output-format", "stream-json",
		"--input-format", "stream-json",
		"--verbose",
		"--max-turns", "0", // unlimited turns
		"--model", string(c.providerOptions.model.APIModel),
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

// stream spawns a claude CLI process and maps its stream-json output to
// ProviderEvent values on the returned channel.
func (c *claudeCodeClient) stream(ctx context.Context, messages []message.Message, tools []toolsPkg.BaseTool) <-chan ProviderEvent {
	eventChan := make(chan ProviderEvent, 64)

	go func() {
		defer close(eventChan)

		cmd := c.buildCommand(ctx)

		stdin, err := cmd.StdinPipe()
		if err != nil {
			eventChan <- ProviderEvent{Type: EventError, Error: fmt.Errorf("stdin pipe: %w", err)}
			return
		}

		stdout, err := cmd.StdoutPipe()
		if err != nil {
			eventChan <- ProviderEvent{Type: EventError, Error: fmt.Errorf("stdout pipe: %w", err)}
			return
		}

		if err := cmd.Start(); err != nil {
			eventChan <- ProviderEvent{Type: EventError, Error: fmt.Errorf("start claude: %w", err)}
			return
		}

		// Send the user message via stdin (newline-delimited JSON).
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
		if _, err := stdin.Write(msgBytes); err != nil {
			logging.Error("failed to write to claude stdin", "error", err)
		}
		stdin.Close() // Signal end of input

		// Read and process stdout events.
		c.processStream(ctx, stdout, eventChan)

		// Wait for process to exit.
		if err := cmd.Wait(); err != nil {
			logging.Debug("claude process exited", "error", err)
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

// processStream reads newline-delimited JSON from stdout and emits
// ProviderEvent values on eventChan.
func (c *claudeCodeClient) processStream(ctx context.Context, stdout io.Reader, eventChan chan<- ProviderEvent) {
	scanner := bufio.NewScanner(stdout)
	// Increase buffer size for large tool outputs (1 MB).
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	var fullContent strings.Builder
	var totalUsage TokenUsage

	// Track active tool calls for proper start/stop events.
	activeToolCalls := make(map[string]bool)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
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
			if event.Subtype == "init" && event.SessionID != "" {
				c.mu.Lock()
				c.sessionID = event.SessionID
				c.mu.Unlock()
			}

		case "stream_event":
			if event.Event == nil {
				continue
			}
			c.handleStreamEvent(event.Event, eventChan, &fullContent)

		case "assistant":
			if event.Message == nil {
				continue
			}
			c.handleAssistantMessage(event.Message, eventChan, activeToolCalls)

		case "user":
			if event.Message == nil {
				continue
			}
			c.handleToolResults(event.Message, eventChan, activeToolCalls)

		case "result":
			if event.Usage != nil {
				totalUsage = TokenUsage{
					InputTokens:         event.Usage.InputTokens,
					OutputTokens:        event.Usage.OutputTokens,
					CacheCreationTokens: event.Usage.CacheCreationInputTokens,
					CacheReadTokens:     event.Usage.CacheReadInputTokens,
				}
			}
			if event.SessionID != "" {
				c.mu.Lock()
				c.sessionID = event.SessionID
				c.mu.Unlock()
			}
			eventChan <- ProviderEvent{
				Type: EventComplete,
				Response: &ProviderResponse{
					Content:      fullContent.String(),
					ToolCalls:    nil, // Never expose tool calls — the agent loop must NOT re-execute them
					Usage:        totalUsage,
					FinishReason: message.FinishReasonEndTurn,
				},
			}
		}
	}
}

// handleStreamEvent processes content_block_delta events (text and thinking).
func (c *claudeCodeClient) handleStreamEvent(event *streamEvent, eventChan chan<- ProviderEvent, fullContent *strings.Builder) {
	if event.Type != "content_block_delta" || event.Delta == nil {
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

// handleAssistantMessage processes full assistant messages, emitting
// EventToolUseStart for any tool_use content blocks.
func (c *claudeCodeClient) handleAssistantMessage(msg *claudeMessage, eventChan chan<- ProviderEvent, activeToolCalls map[string]bool) {
	blocks := c.parseContentBlocks(msg.Content)
	for _, block := range blocks {
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
			eventChan <- ProviderEvent{
				Type: EventToolUseStop,
				ToolCall: &message.ToolCall{
					ID:       block.ToolUseID,
					Finished: true,
				},
			}
		}
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
