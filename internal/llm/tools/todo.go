package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/cliffren/toc/internal/pubsub"
)

const (
	TodoToolName    = "todowrite"
	todoDescription = `Use this tool to create and manage a structured task list for your current coding session. This helps you track progress and organize complex tasks.

Use this tool when:
- Complex multi-step tasks (3+ steps)
- User provides multiple tasks
- After receiving new instructions

Do NOT use when:
- Single straightforward task
- Trivial task completed in < 3 steps
- Purely conversational or informational

Task states: "pending", "in_progress", "completed"
- Only ONE task should be in_progress at a time
- Mark tasks complete immediately after finishing`
)

type TodoItem struct {
	Content string `json:"content"`
	Status  string `json:"status"` // pending, in_progress, completed
}

type TodoParams struct {
	Todos []TodoItem `json:"todos"`
}

type TodoEvent struct {
	SessionID string
	Todos     []TodoItem
}

type TodoStore struct {
	mu    sync.RWMutex
	todos map[string][]TodoItem
	*pubsub.Broker[TodoEvent]
}

func NewTodoStore() *TodoStore {
	return &TodoStore{
		todos:  make(map[string][]TodoItem),
		Broker: pubsub.NewBroker[TodoEvent](),
	}
}

func (s *TodoStore) Set(sessionID string, todos []TodoItem) {
	s.mu.Lock()
	s.todos[sessionID] = todos
	s.mu.Unlock()
	s.Publish(pubsub.UpdatedEvent, TodoEvent{
		SessionID: sessionID,
		Todos:     todos,
	})
}

func (s *TodoStore) Get(sessionID string) []TodoItem {
	s.mu.RLock()
	defer s.mu.RUnlock()
	src := s.todos[sessionID]
	if len(src) == 0 {
		return nil
	}
	dst := make([]TodoItem, len(src))
	copy(dst, src)
	return dst
}

type todoTool struct {
	store *TodoStore
}

func NewTodoTool(store *TodoStore) BaseTool {
	return &todoTool{store: store}
}

func (t *todoTool) Info() ToolInfo {
	return ToolInfo{
		Name:        TodoToolName,
		Description: todoDescription,
		Parameters: map[string]any{
			"todos": map[string]any{
				"type":        "array",
				"description": "The updated todo list",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"content": map[string]any{
							"type":        "string",
							"description": "Task description",
						},
						"status": map[string]any{
							"type":        "string",
							"enum":        []string{"pending", "in_progress", "completed"},
							"description": "Task status",
						},
					},
					"required": []string{"content", "status"},
				},
			},
		},
		Required: []string{"todos"},
	}
}

func (t *todoTool) Run(ctx context.Context, call ToolCall) (ToolResponse, error) {
	var params TodoParams
	if err := json.Unmarshal([]byte(call.Input), &params); err != nil {
		return NewTextErrorResponse(fmt.Sprintf("error parsing parameters: %s", err)), nil
	}

	sessionID, _ := GetContextValues(ctx)
	if sessionID == "" {
		return NewTextErrorResponse("no session context"), nil
	}

	// Validate statuses
	for _, todo := range params.Todos {
		switch todo.Status {
		case "pending", "in_progress", "completed":
		default:
			return NewTextErrorResponse(fmt.Sprintf("invalid status %q for task %q", todo.Status, todo.Content)), nil
		}
	}

	t.store.Set(sessionID, params.Todos)

	// Build summary
	var pending, inProgress, completed int
	for _, todo := range params.Todos {
		switch todo.Status {
		case "pending":
			pending++
		case "in_progress":
			inProgress++
		case "completed":
			completed++
		}
	}

	parts := []string{fmt.Sprintf("Updated %d tasks", len(params.Todos))}
	if completed > 0 {
		parts = append(parts, fmt.Sprintf("%d completed", completed))
	}
	if inProgress > 0 {
		parts = append(parts, fmt.Sprintf("%d in progress", inProgress))
	}
	if pending > 0 {
		parts = append(parts, fmt.Sprintf("%d pending", pending))
	}

	return NewTextResponse(strings.Join(parts, ", ")), nil
}
