package provider

import (
	"context"
	"fmt"
	"os"

	"github.com/cliffren/toc/internal/llm/models"
	"github.com/cliffren/toc/internal/llm/tools"
	"github.com/cliffren/toc/internal/message"
)

type EventType string

const maxRetries = 8

const (
	EventContentStart     EventType = "content_start"
	EventToolUseStart     EventType = "tool_use_start"
	EventToolUseDelta     EventType = "tool_use_delta"
	EventToolUseStop      EventType = "tool_use_stop"
	EventContentDelta     EventType = "content_delta"
	EventThinkingDelta    EventType = "thinking_delta"
	EventContentStop      EventType = "content_stop"
	EventComplete         EventType = "complete"
	EventInit             EventType = "init"
	EventError            EventType = "error"
	EventWarning           EventType = "warning"
	EventPermissionRequest EventType = "permission_request"
	EventCompacting        EventType = "compacting"
)

// ProviderPermissionRequest carries a permission request from Claude Code CLI.
type ProviderPermissionRequest struct {
	RequestID   string
	SessionID   string
	ToolName    string
	Input       map[string]any
	BlockedPath string
	Description string
}

// ControlResponder is implemented by providers that support sending control
// responses back to the underlying process (e.g. Claude Code CLI stdin).
type ControlResponder interface {
	SendControlResponse(requestID, sessionID string, allow bool) error
}

type TokenUsage struct {
	InputTokens         int64
	OutputTokens        int64
	CacheCreationTokens int64
	CacheReadTokens     int64
}

type ProviderResponse struct {
	Content      string
	ToolCalls    []message.ToolCall
	Usage        TokenUsage
	FinishReason message.FinishReason
	TotalCostUSD float64 // non-zero when the provider reports exact cost (e.g. Claude Code result event)
}

type InitData struct {
	SlashCommands  []string
	Tools          []string
	Model          string
	PermissionMode string
	Version        string
	SessionID      string // Claude Code CLI session ID (for --resume on restart)
}

// SessionResumer is implemented by providers that support resuming a prior
// external session (e.g. Claude Code CLI --resume).
type SessionResumer interface {
	SetResumeSessionID(id string)
}

type ProviderEvent struct {
	Type EventType

	Content           string
	Thinking          string
	Response          *ProviderResponse
	ToolCall          *message.ToolCall
	InitData          *InitData
	Error             error
	PermissionRequest *ProviderPermissionRequest
}
type Provider interface {
	SendMessages(ctx context.Context, messages []message.Message, tools []tools.BaseTool) (*ProviderResponse, error)

	StreamResponse(ctx context.Context, messages []message.Message, tools []tools.BaseTool) <-chan ProviderEvent

	Model() models.Model
}

type providerClientOptions struct {
	apiKey              string
	model               models.Model
	maxTokens           int64
	systemMessage       string
	appendSystemMessage bool
	reasoningEffort     string
	permissionMode      string

	anthropicOptions []AnthropicOption
	openaiOptions    []OpenAIOption
	geminiOptions    []GeminiOption
	bedrockOptions   []BedrockOption
	copilotOptions   []CopilotOption
}

type ProviderClientOption func(*providerClientOptions)

type ProviderClient interface {
	send(ctx context.Context, messages []message.Message, tools []tools.BaseTool) (*ProviderResponse, error)
	stream(ctx context.Context, messages []message.Message, tools []tools.BaseTool) <-chan ProviderEvent
}

type baseProvider[C ProviderClient] struct {
	options providerClientOptions
	client  C
}

func NewProvider(providerName models.ModelProvider, opts ...ProviderClientOption) (Provider, error) {
	clientOptions := providerClientOptions{}
	for _, o := range opts {
		o(&clientOptions)
	}
	switch providerName {
	case models.ProviderCopilot:
		return &baseProvider[CopilotClient]{
			options: clientOptions,
			client:  newCopilotClient(clientOptions),
		}, nil
	case models.ProviderAnthropic:
		return &baseProvider[AnthropicClient]{
			options: clientOptions,
			client:  newAnthropicClient(clientOptions),
		}, nil
	case models.ProviderOpenAI:
		return &baseProvider[OpenAIClient]{
			options: clientOptions,
			client:  newOpenAIClient(clientOptions),
		}, nil
	case models.ProviderGemini:
		return &baseProvider[GeminiClient]{
			options: clientOptions,
			client:  newGeminiClient(clientOptions),
		}, nil
	case models.ProviderBedrock:
		return &baseProvider[BedrockClient]{
			options: clientOptions,
			client:  newBedrockClient(clientOptions),
		}, nil
	case models.ProviderGROQ:
		clientOptions.openaiOptions = append(clientOptions.openaiOptions,
			WithOpenAIBaseURL("https://api.groq.com/openai/v1"),
		)
		return &baseProvider[OpenAIClient]{
			options: clientOptions,
			client:  newOpenAIClient(clientOptions),
		}, nil
	case models.ProviderAzure:
		return &baseProvider[AzureClient]{
			options: clientOptions,
			client:  newAzureClient(clientOptions),
		}, nil
	case models.ProviderVertexAI:
		return &baseProvider[VertexAIClient]{
			options: clientOptions,
			client:  newVertexAIClient(clientOptions),
		}, nil
	case models.ProviderOpenRouter:
		clientOptions.openaiOptions = append(clientOptions.openaiOptions,
			WithOpenAIBaseURL("https://openrouter.ai/api/v1"),
			WithOpenAIExtraHeaders(map[string]string{
				"HTTP-Referer": "https://github.com/cliffren/toc",
				"X-Title":      "toc",
			}),
		)
		return &baseProvider[OpenAIClient]{
			options: clientOptions,
			client:  newOpenAIClient(clientOptions),
		}, nil
	case models.ProviderXAI:
		clientOptions.openaiOptions = append(clientOptions.openaiOptions,
			WithOpenAIBaseURL("https://api.x.ai/v1"),
		)
		return &baseProvider[OpenAIClient]{
			options: clientOptions,
			client:  newOpenAIClient(clientOptions),
		}, nil
	case models.ProviderLocal:
		clientOptions.openaiOptions = append(clientOptions.openaiOptions,
			WithOpenAIBaseURL(os.Getenv("LOCAL_ENDPOINT")),
		)
		return &baseProvider[OpenAIClient]{
			options: clientOptions,
			client:  newOpenAIClient(clientOptions),
		}, nil
	case models.ProviderClaudeCode:
		return &baseProvider[ClaudeCodeClient]{
			options: clientOptions,
			client:  newClaudeCodeClient(clientOptions),
		}, nil
	case models.ProviderMock:
		// TODO: implement mock client for test
		panic("not implemented")
	}
	return nil, fmt.Errorf("provider not supported: %s", providerName)
}

func (p *baseProvider[C]) cleanMessages(messages []message.Message) (cleaned []message.Message) {
	for _, msg := range messages {
		// The message has no content
		if len(msg.Parts) == 0 {
			continue
		}
		cleaned = append(cleaned, msg)
	}
	return
}

func (p *baseProvider[C]) SendMessages(ctx context.Context, messages []message.Message, tools []tools.BaseTool) (*ProviderResponse, error) {
	messages = p.cleanMessages(messages)
	return p.client.send(ctx, messages, tools)
}

func (p *baseProvider[C]) Model() models.Model {
	return p.options.model
}

// SendControlResponse forwards to the underlying client if it implements
// ControlResponder. This allows agent.go to type-assert on the Provider.
func (p *baseProvider[C]) SendControlResponse(requestID, sessionID string, allow bool) error {
	if cr, ok := any(p.client).(ControlResponder); ok {
		return cr.SendControlResponse(requestID, sessionID, allow)
	}
	return fmt.Errorf("provider does not support control responses")
}

// SetResumeSessionID forwards to the underlying client if it implements
// SessionResumer.
func (p *baseProvider[C]) SetResumeSessionID(id string) {
	if sr, ok := any(p.client).(SessionResumer); ok {
		sr.SetResumeSessionID(id)
	}
}

func (p *baseProvider[C]) StreamResponse(ctx context.Context, messages []message.Message, tools []tools.BaseTool) <-chan ProviderEvent {
	messages = p.cleanMessages(messages)
	return p.client.stream(ctx, messages, tools)
}

func WithAPIKey(apiKey string) ProviderClientOption {
	return func(options *providerClientOptions) {
		options.apiKey = apiKey
	}
}

func WithModel(model models.Model) ProviderClientOption {
	return func(options *providerClientOptions) {
		options.model = model
	}
}

func WithMaxTokens(maxTokens int64) ProviderClientOption {
	return func(options *providerClientOptions) {
		options.maxTokens = maxTokens
	}
}

func WithSystemMessage(systemMessage string) ProviderClientOption {
	return func(options *providerClientOptions) {
		options.systemMessage = systemMessage
	}
}

func WithAppendSystemMessage(append bool) ProviderClientOption {
	return func(options *providerClientOptions) {
		options.appendSystemMessage = append
	}
}

func WithEffort(effort string) ProviderClientOption {
	return func(options *providerClientOptions) {
		options.reasoningEffort = effort
	}
}

func WithPermissionMode(mode string) ProviderClientOption {
	return func(options *providerClientOptions) {
		options.permissionMode = mode
	}
}

func WithAnthropicOptions(anthropicOptions ...AnthropicOption) ProviderClientOption {
	return func(options *providerClientOptions) {
		options.anthropicOptions = anthropicOptions
	}
}

func WithOpenAIOptions(openaiOptions ...OpenAIOption) ProviderClientOption {
	return func(options *providerClientOptions) {
		options.openaiOptions = openaiOptions
	}
}

func WithGeminiOptions(geminiOptions ...GeminiOption) ProviderClientOption {
	return func(options *providerClientOptions) {
		options.geminiOptions = geminiOptions
	}
}

func WithBedrockOptions(bedrockOptions ...BedrockOption) ProviderClientOption {
	return func(options *providerClientOptions) {
		options.bedrockOptions = bedrockOptions
	}
}

func WithCopilotOptions(copilotOptions ...CopilotOption) ProviderClientOption {
	return func(options *providerClientOptions) {
		options.copilotOptions = copilotOptions
	}
}
