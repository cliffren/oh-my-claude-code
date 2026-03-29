package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"regexp"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	runewidth "github.com/mattn/go-runewidth"
	"github.com/Krontx/oh-my-claude-code/internal/config"
	"github.com/Krontx/oh-my-claude-code/internal/diff"
	"github.com/Krontx/oh-my-claude-code/internal/llm/agent"
	"github.com/Krontx/oh-my-claude-code/internal/llm/models"
	"github.com/Krontx/oh-my-claude-code/internal/llm/tools"
	"github.com/Krontx/oh-my-claude-code/internal/message"
	"github.com/Krontx/oh-my-claude-code/internal/tui/styles"
	"github.com/Krontx/oh-my-claude-code/internal/tui/theme"
)

type uiMessageType int

const (
	userMessageType uiMessageType = iota
	assistantMessageType
	toolMessageType

	maxResultHeight = 10
)

// subBlock maps a line offset within a uiMessage to a toggleable block ID.
type subBlock struct {
	lineOffset int // line offset within the parent uiMessage content
	id         string
}

type uiMessage struct {
	ID          string
	messageType uiMessageType
	position    int
	height      int
	content     string
	subBlocks   []subBlock // optional: nested expand/collapse targets
}

func toMarkdown(content string, focused bool, width int) string {
	content = compactMarkdownTables(content)
	r := styles.GetMarkdownRenderer(width)
	rendered, _ := r.Render(content)
	return rendered
}

// compactMarkdownTables converts markdown tables to a compact text format
// so glamour doesn't stretch columns to fill the full width.
func compactMarkdownTables(content string) string {
	lines := strings.Split(content, "\n")
	var result []string
	i := 0
	for i < len(lines) {
		// Detect table: line with | that is followed by a separator line (|---|)
		if isTableRow(lines[i]) && i+1 < len(lines) && isTableSeparator(lines[i+1]) {
			// Collect all table rows
			tableStart := i
			i++ // skip header
			i++ // skip separator
			for i < len(lines) && isTableRow(lines[i]) {
				i++
			}
			// Convert table to compact format
			compacted := compactTable(lines[tableStart:i])
			result = append(result, compacted...)
		} else {
			result = append(result, lines[i])
			i++
		}
	}
	return strings.Join(result, "\n")
}

func isTableRow(line string) bool {
	trimmed := strings.TrimSpace(line)
	return strings.HasPrefix(trimmed, "|") && strings.HasSuffix(trimmed, "|") && strings.Count(trimmed, "|") >= 3
}

func isTableSeparator(line string) bool {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "|") {
		return false
	}
	// Must contain only |, -, :, and spaces
	for _, ch := range trimmed {
		if ch != '|' && ch != '-' && ch != ':' && ch != ' ' {
			return false
		}
	}
	return strings.Contains(trimmed, "---")
}

func compactTable(rows []string) []string {
	// Parse cells
	var parsed [][]string
	for i, row := range rows {
		if i == 1 { // skip separator row
			continue
		}
		cells := parseTableCells(row)
		parsed = append(parsed, cells)
	}
	if len(parsed) == 0 {
		return rows
	}

	// Strip markdown from cells (bold/italic won't render in code blocks)
	for i, row := range parsed {
		for j, cell := range row {
			parsed[i][j] = stripInlineMarkdown(cell)
		}
	}

	// Calculate column widths using display width (CJK = 2 columns)
	numCols := len(parsed[0])
	widths := make([]int, numCols)
	for _, row := range parsed {
		for j := 0; j < len(row) && j < numCols; j++ {
			w := runewidth.StringWidth(row[j])
			if w > widths[j] {
				widths[j] = w
			}
		}
	}

	// Render compact: use code block style to prevent glamour re-expansion
	var result []string
	result = append(result, "```")
	for i, row := range parsed {
		var line string
		for j := 0; j < numCols; j++ {
			cell := ""
			if j < len(row) {
				cell = row[j]
			}
			displayW := runewidth.StringWidth(cell)
			padding := widths[j] - displayW
			if padding < 0 {
				padding = 0
			}
			line += cell + strings.Repeat(" ", padding) + "  "
		}
		result = append(result, strings.TrimRight(line, " "))
		if i == 0 {
			// Add a thin separator after header
			var sep string
			for j := 0; j < numCols; j++ {
				sep += strings.Repeat("─", widths[j]) + "  "
			}
			result = append(result, strings.TrimRight(sep, " "))
		}
	}
	result = append(result, "```")
	return result
}

func parseTableCells(row string) []string {
	trimmed := strings.TrimSpace(row)
	trimmed = strings.TrimPrefix(trimmed, "|")
	trimmed = strings.TrimSuffix(trimmed, "|")
	parts := strings.Split(trimmed, "|")
	cells := make([]string, len(parts))
	for i, p := range parts {
		cells[i] = strings.TrimSpace(p)
	}
	return cells
}

var mdInlineRe = regexp.MustCompile(`(\*\*|__|\*|_|~~|` + "`" + `)`)

// stripInlineMarkdown removes inline markdown markers (bold, italic, code, strikethrough)
// from a cell string so they render cleanly in a plain code block.
func stripInlineMarkdown(s string) string {
	return mdInlineRe.ReplaceAllString(s, "")
}

func renderMessage(msg string, isUser bool, isFocused bool, width int, info ...string) string {
	t := theme.CurrentTheme()

	style := styles.BaseStyle().
		Width(width - 1).
		BorderLeft(true).
		Foreground(t.TextMuted()).
		BorderForeground(t.Primary()).
		BorderStyle(lipgloss.ThickBorder())

	if isUser {
		style = style.BorderForeground(t.Secondary())
	}

	// Apply markdown formatting; reduce width to account for border
	parts := []string{
		styles.ForceReplaceBackgroundWithLipgloss(toMarkdown(msg, isFocused, width-3), t.Background()),
	}

	// Remove newline at the end
	parts[0] = strings.TrimSuffix(parts[0], "\n")
	if len(info) > 0 {
		parts = append(parts, info...)
	}

	rendered := style.Render(
		lipgloss.JoinVertical(
			lipgloss.Left,
			parts...,
		),
	)

	return rendered
}

func renderUserMessage(msg message.Message, isFocused bool, width int, position int) uiMessage {
	var styledAttachments []string
	t := theme.CurrentTheme()
	attachmentStyles := styles.BaseStyle().
		MarginLeft(1).
		Background(t.TextMuted()).
		Foreground(t.Text())
	for _, attachment := range msg.BinaryContent() {
		file := filepath.Base(attachment.Path)
		var filename string
		if len(file) > 10 {
			filename = fmt.Sprintf(" %s %s...", styles.DocumentIcon, file[0:7])
		} else {
			filename = fmt.Sprintf(" %s %s", styles.DocumentIcon, file)
		}
		styledAttachments = append(styledAttachments, attachmentStyles.Render(filename))
	}
	content := ""
	if len(styledAttachments) > 0 {
		attachmentContent := styles.BaseStyle().Width(width).Render(lipgloss.JoinHorizontal(lipgloss.Left, styledAttachments...))
		content = renderMessage(msg.Content().String(), true, isFocused, width, attachmentContent)
	} else {
		content = renderMessage(msg.Content().String(), true, isFocused, width)
	}
	userMsg := uiMessage{
		ID:          msg.ID,
		messageType: userMessageType,
		position:    position,
		height:      lipgloss.Height(content),
		content:     content,
	}
	return userMsg
}

// Returns multiple uiMessages because of the tool calls
func renderAssistantMessage(
	msg message.Message,
	msgIndex int,
	allMessages []message.Message, // we need this to get tool results and the user message
	messagesService message.Service, // We need this to get the task tool messages
	focusedUIMessageId string,
	isSummary bool,
	width int,
	position int,
	expandedBlocks map[string]bool,
) []uiMessage {
	messages := []uiMessage{}
	content := msg.Content().String()
	thinkingContent := msg.ReasoningContent().Thinking
	finished := msg.IsFinished()
	finishData := msg.FinishPart()
	info := []string{}

	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()

	// Add finish info if available
	if finished {
		switch finishData.Reason {
		case message.FinishReasonEndTurn:
			took := formatTimestampDiff(msg.CreatedAt, finishData.Time)
			info = append(info, baseStyle.
				Width(width-1).
				Foreground(t.TextMuted()).
				Render(fmt.Sprintf(" %s (%s)", models.SupportedModels[msg.Model].Name, took)),
			)
		case message.FinishReasonCanceled:
			info = append(info, baseStyle.
				Width(width-1).
				Foreground(t.TextMuted()).
				Render(fmt.Sprintf(" %s (%s)", models.SupportedModels[msg.Model].Name, "canceled")),
			)
		case message.FinishReasonError:
			info = append(info, baseStyle.
				Width(width-1).
				Foreground(t.TextMuted()).
				Render(fmt.Sprintf(" %s (%s)", models.SupportedModels[msg.Model].Name, "error")),
			)
		case message.FinishReasonPermissionDenied:
			info = append(info, baseStyle.
				Width(width-1).
				Foreground(t.TextMuted()).
				Render(fmt.Sprintf(" %s (%s)", models.SupportedModels[msg.Model].Name, "permission denied")),
			)
		}
	}
	// Render thinking content as a muted block before the main content
	if thinkingContent != "" {
		thinkingBlockID := msg.ID + "-thinking"
		expanded := expandedBlocks[thinkingBlockID]

		displayContent := thinkingContent
		const maxThinkingLines = 20
		lines := strings.Split(thinkingContent, "\n")
		totalLines := len(lines)
		needsTruncation := totalLines > maxThinkingLines

		if !expanded && needsTruncation {
			displayContent = strings.Join(lines[:maxThinkingLines], "\n") + fmt.Sprintf("\n▶ Show all (%d lines)", totalLines)
		} else if expanded && needsTruncation {
			displayContent = thinkingContent + "\n▼ Collapse"
		}

		thinkingStyle := styles.BaseStyle().
			Width(width - 1).
			BorderLeft(true).
			Foreground(t.TextMuted()).
			BorderForeground(t.TextMuted()).
			BorderStyle(lipgloss.ThickBorder())

		thinkingBlock := thinkingStyle.Foreground(t.TextMuted()).Render("Thinking...\n" + displayContent)

		messages = append(messages, uiMessage{
			ID:          thinkingBlockID,
			messageType: assistantMessageType,
			position:    position,
			height:      lipgloss.Height(thinkingBlock),
			content:     thinkingBlock,
		})
		position += lipgloss.Height(thinkingBlock)
		position++
	}

	hasToolCalls := len(msg.ToolCalls()) > 0

	// Tool calls render BEFORE text content (tools execute first, then the reply)
	toolCalls := msg.ToolCalls()
	if len(toolCalls) > 0 {
		// Attach model/timing info to the group when there is no text content
		var groupInfo []string
		if content == "" {
			groupInfo = info
		}
		groupMsg := renderToolCallsGroup(
			msg.ID,
			toolCalls,
			allMessages,
			messagesService,
			focusedUIMessageId,
			width,
			position,
			expandedBlocks,
			groupInfo,
		)
		messages = append(messages, groupMsg)
		position += groupMsg.height
		position++ // for the space
	}

	if content != "" || (finished && finishData.Reason == message.FinishReasonEndTurn && !hasToolCalls && thinkingContent == "") {
		if content == "" {
			content = "*Finished without output*"
		}
		if isSummary {
			info = append(info, baseStyle.Width(width-1).Foreground(t.TextMuted()).Render(" (summary)"))
		}

		content = renderMessage(content, false, true, width, info...)
		messages = append(messages, uiMessage{
			ID:          msg.ID,
			messageType: assistantMessageType,
			position:    position,
			height:      lipgloss.Height(content),
			content:     content,
		})
		position += messages[len(messages)-1].height
		position++ // for the space
	}
	return messages
}

func findToolResponse(toolCallID string, futureMessages []message.Message) *message.ToolResult {
	for _, msg := range futureMessages {
		for _, result := range msg.ToolResults() {
			if result.ToolCallID == toolCallID {
				return &result
			}
		}
	}
	return nil
}

func toolName(name string) string {
	switch name {
	case agent.AgentToolName:
		return "Task"
	case tools.BashToolName:
		return "Bash"
	case tools.EditToolName:
		return "Edit"
	case tools.FetchToolName:
		return "Fetch"
	case tools.GlobToolName:
		return "Glob"
	case tools.GrepToolName:
		return "Grep"
	case tools.LSToolName:
		return "List"
	case tools.SourcegraphToolName:
		return "Sourcegraph"
	case tools.ViewToolName:
		return "View"
	case tools.WriteToolName:
		return "Write"
	case tools.PatchToolName:
		return "Patch"
	}
	return name
}

func getToolAction(name string) string {
	switch name {
	case agent.AgentToolName:
		return "Preparing prompt..."
	case tools.BashToolName:
		return "Building command..."
	case tools.EditToolName:
		return "Preparing edit..."
	case tools.FetchToolName:
		return "Writing fetch..."
	case tools.GlobToolName:
		return "Finding files..."
	case tools.GrepToolName:
		return "Searching content..."
	case tools.LSToolName:
		return "Listing directory..."
	case tools.SourcegraphToolName:
		return "Searching code..."
	case tools.ViewToolName:
		return "Reading file..."
	case tools.WriteToolName:
		return "Preparing write..."
	case tools.PatchToolName:
		return "Preparing patch..."
	}
	return "Working..."
}

// renders params, params[0] (params[1]=params[2] ....)
func renderParams(paramsWidth int, params ...string) string {
	if len(params) == 0 {
		return ""
	}
	mainParam := params[0]
	if len(mainParam) > paramsWidth {
		mainParam = mainParam[:paramsWidth-3] + "..."
	}

	if len(params) == 1 {
		return mainParam
	}
	otherParams := params[1:]
	// create pairs of key/value
	// if odd number of params, the last one is a key without value
	if len(otherParams)%2 != 0 {
		otherParams = append(otherParams, "")
	}
	parts := make([]string, 0, len(otherParams)/2)
	for i := 0; i < len(otherParams); i += 2 {
		key := otherParams[i]
		value := otherParams[i+1]
		if value == "" {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s=%s", key, value))
	}

	partsRendered := strings.Join(parts, ", ")
	remainingWidth := paramsWidth - lipgloss.Width(partsRendered) - 5 // for the space
	if remainingWidth < 30 {
		// No space for the params, just show the main
		return mainParam
	}

	if len(parts) > 0 {
		mainParam = fmt.Sprintf("%s (%s)", mainParam, strings.Join(parts, ", "))
	}

	return ansi.Truncate(mainParam, paramsWidth, "...")
}

func removeWorkingDirPrefix(path string) string {
	wd := config.WorkingDirectory()
	if strings.HasPrefix(path, wd) {
		path = strings.TrimPrefix(path, wd)
	}
	if strings.HasPrefix(path, "/") {
		path = strings.TrimPrefix(path, "/")
	}
	if strings.HasPrefix(path, "./") {
		path = strings.TrimPrefix(path, "./")
	}
	if strings.HasPrefix(path, "../") {
		path = strings.TrimPrefix(path, "../")
	}
	return path
}

func renderToolParams(paramWidth int, toolCall message.ToolCall) string {
	params := ""
	switch toolCall.Name {
	case agent.AgentToolName:
		var params agent.AgentParams
		json.Unmarshal([]byte(toolCall.Input), &params)
		prompt := strings.ReplaceAll(params.Prompt, "\n", " ")
		return renderParams(paramWidth, prompt)
	case tools.BashToolName:
		var params tools.BashParams
		json.Unmarshal([]byte(toolCall.Input), &params)
		command := strings.ReplaceAll(params.Command, "\n", " ")
		return renderParams(paramWidth, command)
	case tools.EditToolName:
		var params tools.EditParams
		json.Unmarshal([]byte(toolCall.Input), &params)
		filePath := removeWorkingDirPrefix(params.FilePath)
		return renderParams(paramWidth, filePath)
	case tools.FetchToolName:
		var params tools.FetchParams
		json.Unmarshal([]byte(toolCall.Input), &params)
		url := params.URL
		toolParams := []string{
			url,
		}
		if params.Format != "" {
			toolParams = append(toolParams, "format", params.Format)
		}
		if params.Timeout != 0 {
			toolParams = append(toolParams, "timeout", (time.Duration(params.Timeout) * time.Second).String())
		}
		return renderParams(paramWidth, toolParams...)
	case tools.GlobToolName:
		var params tools.GlobParams
		json.Unmarshal([]byte(toolCall.Input), &params)
		pattern := params.Pattern
		toolParams := []string{
			pattern,
		}
		if params.Path != "" {
			toolParams = append(toolParams, "path", params.Path)
		}
		return renderParams(paramWidth, toolParams...)
	case tools.GrepToolName:
		var params tools.GrepParams
		json.Unmarshal([]byte(toolCall.Input), &params)
		pattern := params.Pattern
		toolParams := []string{
			pattern,
		}
		if params.Path != "" {
			toolParams = append(toolParams, "path", params.Path)
		}
		if params.Include != "" {
			toolParams = append(toolParams, "include", params.Include)
		}
		if params.LiteralText {
			toolParams = append(toolParams, "literal", "true")
		}
		return renderParams(paramWidth, toolParams...)
	case tools.LSToolName:
		var params tools.LSParams
		json.Unmarshal([]byte(toolCall.Input), &params)
		path := params.Path
		if path == "" {
			path = "."
		}
		return renderParams(paramWidth, path)
	case tools.SourcegraphToolName:
		var params tools.SourcegraphParams
		json.Unmarshal([]byte(toolCall.Input), &params)
		return renderParams(paramWidth, params.Query)
	case tools.ViewToolName:
		var params tools.ViewParams
		json.Unmarshal([]byte(toolCall.Input), &params)
		filePath := removeWorkingDirPrefix(params.FilePath)
		toolParams := []string{
			filePath,
		}
		if params.Limit != 0 {
			toolParams = append(toolParams, "limit", fmt.Sprintf("%d", params.Limit))
		}
		if params.Offset != 0 {
			toolParams = append(toolParams, "offset", fmt.Sprintf("%d", params.Offset))
		}
		return renderParams(paramWidth, toolParams...)
	case tools.WriteToolName:
		var params tools.WriteParams
		json.Unmarshal([]byte(toolCall.Input), &params)
		filePath := removeWorkingDirPrefix(params.FilePath)
		return renderParams(paramWidth, filePath)
	default:
		input := strings.ReplaceAll(toolCall.Input, "\n", " ")
		params = renderParams(paramWidth, input)
	}
	return params
}

func truncateHeight(content string, height int) string {
	lines := strings.Split(content, "\n")
	if len(lines) > height {
		return strings.Join(lines[:height], "\n")
	}
	return content
}

func truncateWithHint(content string, height int, expanded bool) string {
	lines := strings.Split(content, "\n")
	totalLines := len(lines)
	if totalLines <= height {
		return content
	}
	if expanded {
		return content + fmt.Sprintf("\n▼ Collapse (%d lines)", totalLines)
	}
	return strings.Join(lines[:height], "\n") + fmt.Sprintf("\n▶ Show all (%d lines)", totalLines)
}

func renderToolResponse(toolCall message.ToolCall, response message.ToolResult, width int) string {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()

	if response.IsError {
		errContent := fmt.Sprintf("Error: %s", strings.ReplaceAll(response.Content, "\n", " "))
		errContent = ansi.Truncate(errContent, width-1, "...")
		return baseStyle.
			Width(width).
			Foreground(t.Error()).
			Render(errContent)
	}

	resultContent := response.Content
	switch toolCall.Name {
	case agent.AgentToolName:
		return styles.ForceReplaceBackgroundWithLipgloss(
			toMarkdown(resultContent, false, width),
			t.Background(),
		)
	case tools.BashToolName:
		resultContent = fmt.Sprintf("```bash\n%s\n```", resultContent)
		return styles.ForceReplaceBackgroundWithLipgloss(
			toMarkdown(resultContent, true, width),
			t.Background(),
		)
	case tools.EditToolName:
		metadata := tools.EditResponseMetadata{}
		json.Unmarshal([]byte(response.Metadata), &metadata)
		truncDiff := metadata.Diff
		formattedDiff, _ := diff.FormatDiff(truncDiff, diff.WithTotalWidth(width))
		return formattedDiff
	case tools.FetchToolName:
		var params tools.FetchParams
		json.Unmarshal([]byte(toolCall.Input), &params)
		mdFormat := "markdown"
		switch params.Format {
		case "text":
			mdFormat = "text"
		case "html":
			mdFormat = "html"
		}
		resultContent = fmt.Sprintf("```%s\n%s\n```", mdFormat, resultContent)
		return styles.ForceReplaceBackgroundWithLipgloss(
			toMarkdown(resultContent, true, width),
			t.Background(),
		)
	case tools.GlobToolName:
		return baseStyle.Width(width).Foreground(t.TextMuted()).Render(resultContent)
	case tools.GrepToolName:
		return baseStyle.Width(width).Foreground(t.TextMuted()).Render(resultContent)
	case tools.LSToolName:
		return baseStyle.Width(width).Foreground(t.TextMuted()).Render(resultContent)
	case tools.SourcegraphToolName:
		return baseStyle.Width(width).Foreground(t.TextMuted()).Render(resultContent)
	case tools.ViewToolName:
		metadata := tools.ViewResponseMetadata{}
		json.Unmarshal([]byte(response.Metadata), &metadata)
		ext := filepath.Ext(metadata.FilePath)
		if ext == "" {
			ext = ""
		} else {
			ext = strings.ToLower(ext[1:])
		}
		resultContent = fmt.Sprintf("```%s\n%s\n```", ext, metadata.Content)
		return styles.ForceReplaceBackgroundWithLipgloss(
			toMarkdown(resultContent, true, width),
			t.Background(),
		)
	case tools.WriteToolName:
		params := tools.WriteParams{}
		json.Unmarshal([]byte(toolCall.Input), &params)
		metadata := tools.WriteResponseMetadata{}
		json.Unmarshal([]byte(response.Metadata), &metadata)
		ext := filepath.Ext(params.FilePath)
		if ext == "" {
			ext = ""
		} else {
			ext = strings.ToLower(ext[1:])
		}
		resultContent = fmt.Sprintf("```%s\n%s\n```", ext, params.Content)
		return styles.ForceReplaceBackgroundWithLipgloss(
			toMarkdown(resultContent, true, width),
			t.Background(),
		)
	default:
		resultContent = fmt.Sprintf("```text\n%s\n```", resultContent)
		return styles.ForceReplaceBackgroundWithLipgloss(
			toMarkdown(resultContent, true, width),
			t.Background(),
		)
	}
}

// renderToolCallsGroup renders all tool calls from one assistant message as a single collapsible block.
// Collapsed: shows "▶ N tool calls (Tool1, Tool2, ...)" summary line.
// Expanded: shows each tool call's name, params, and response.
func renderToolCallsGroup(
	msgID string,
	toolCalls []message.ToolCall,
	allMessages []message.Message,
	messagesService message.Service,
	focusedUIMessageId string,
	width int,
	position int,
	expandedBlocks map[string]bool,
	infoLines []string,
) uiMessage {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()

	groupBlockID := msgID + "-toolcalls"
	isExpanded := expandedBlocks[groupBlockID]

	style := baseStyle.
		Width(width - 1).
		BorderLeft(true).
		BorderStyle(lipgloss.ThickBorder()).
		PaddingLeft(1).
		BorderForeground(t.TextMuted())

	// Build summary of tool names
	names := make([]string, len(toolCalls))
	for i, tc := range toolCalls {
		names[i] = toolName(tc.Name)
	}

	expandIndicator := "▶ "
	if isExpanded {
		expandIndicator = "▼ "
	}

	n := len(toolCalls)
	suffix := "s"
	if n == 1 {
		suffix = ""
	}
	headerText := fmt.Sprintf("%s%d tool call%s", expandIndicator, n, suffix)
	if n <= 4 {
		headerText += " (" + strings.Join(names, ", ") + ")"
	}

	parts := []string{
		baseStyle.Foreground(t.TextMuted()).Render(headerText),
	}

	// Track line count for subBlock hit-testing.
	// Start after the header line. The border/padding adds 0 extra lines.
	lineCount := 1 // header is 1 line
	var subs []subBlock

	if isExpanded {
		for i, tc := range toolCalls {
			if i > 0 {
				sep := baseStyle.Width(width-3).Foreground(t.TextMuted()).Render("──────────")
				parts = append(parts, sep)
				lineCount += lipgloss.Height(sep)
			}
			if !tc.Finished {
				action := getToolAction(tc.Name)
				line := baseStyle.Foreground(t.TextMuted()).
					Render(fmt.Sprintf("%s: %s", toolName(tc.Name), action))
				parts = append(parts, line)
				lineCount += lipgloss.Height(line)
				continue
			}
			// Each tool call has its own ▶/▼ toggle for the response
			tcExpanded := expandedBlocks[tc.ID]
			tcIndicator := "▶ "
			if tcExpanded {
				tcIndicator = "▼ "
			}
			params := renderToolParams(width-8, tc)
			tcHeader := baseStyle.Foreground(t.TextMuted()).
				Render(fmt.Sprintf("%s%s: %s", tcIndicator, toolName(tc.Name), params))
			subs = append(subs, subBlock{lineOffset: lineCount, id: tc.ID})
			parts = append(parts, tcHeader)
			lineCount += lipgloss.Height(tcHeader)
			// Response (only when this tool call is expanded)
			if tcExpanded {
				response := findToolResponse(tc.ID, allMessages)
				if response != nil {
					respContent := renderToolResponse(tc, *response, width-4)
					respContent = strings.TrimSuffix(respContent, "\n")
					if respContent != "" {
						parts = append(parts, respContent)
						lineCount += lipgloss.Height(respContent)
					}
				}
				// Nested agent tool calls
				if tc.Name == agent.AgentToolName {
					taskMessages, _ := messagesService.List(context.Background(), tc.ID)
					for _, tm := range taskMessages {
						for _, call := range tm.ToolCalls() {
							rendered := renderToolMessage(call, []message.Message{}, messagesService, focusedUIMessageId, true, width, 0, expandedBlocks, nil)
							parts = append(parts, rendered.content)
							lineCount += lipgloss.Height(rendered.content)
						}
					}
				}
			}
		}
	}

	for _, line := range infoLines {
		parts = append(parts, line)
	}

	content := style.Render(lipgloss.JoinVertical(lipgloss.Left, parts...))
	return uiMessage{
		ID:          groupBlockID,
		messageType: toolMessageType,
		subBlocks:   subs,
		position:    position,
		height:      lipgloss.Height(content),
		content:     content,
	}
}

func renderToolMessage(
	toolCall message.ToolCall,
	allMessages []message.Message,
	messagesService message.Service,
	focusedUIMessageId string,
	nested bool,
	width int,
	position int,
	expandedBlocks map[string]bool,
	infoLines []string,
) uiMessage {
	if nested {
		width = width - 3
	}

	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()

	style := baseStyle.
		Width(width - 1).
		BorderLeft(true).
		BorderStyle(lipgloss.ThickBorder()).
		PaddingLeft(1).
		BorderForeground(t.TextMuted())

	response := findToolResponse(toolCall.ID, allMessages)
	toolNameText := baseStyle.Foreground(t.TextMuted()).
		Render(fmt.Sprintf("%s: ", toolName(toolCall.Name)))

	if !toolCall.Finished {
		// Get a brief description of what the tool is doing
		toolAction := getToolAction(toolCall.Name)

		progressText := baseStyle.
			Width(width - 2 - lipgloss.Width(toolNameText)).
			Foreground(t.TextMuted()).
			Render(fmt.Sprintf("%s", toolAction))

		content := style.Render(lipgloss.JoinHorizontal(lipgloss.Left, toolNameText, progressText))
		toolMsg := uiMessage{
			messageType: toolMessageType,
			position:    position,
			height:      lipgloss.Height(content),
			content:     content,
		}
		return toolMsg
	}

	toolBlockID := toolCall.ID
	isExpanded := expandedBlocks[toolBlockID]

	// ▶/▼ indicator on the header — clicking it toggles the response
	expandIndicator := "▶ "
	if isExpanded {
		expandIndicator = "▼ "
	}
	toolNameText = baseStyle.Foreground(t.TextMuted()).
		Render(fmt.Sprintf("%s%s: ", expandIndicator, toolName(toolCall.Name)))

	params := renderToolParams(width-2-lipgloss.Width(toolNameText), toolCall)
	responseContent := ""
	if response != nil && isExpanded {
		responseContent = renderToolResponse(toolCall, *response, width-2)
		responseContent = strings.TrimSuffix(responseContent, "\n")
	}

	parts := []string{}
	if !nested {
		formattedParams := baseStyle.
			Width(width - 2 - lipgloss.Width(toolNameText)).
			Foreground(t.TextMuted()).
			Render(params)

		parts = append(parts, lipgloss.JoinHorizontal(lipgloss.Left, toolNameText, formattedParams))
	} else {
		prefix := baseStyle.
			Foreground(t.TextMuted()).
			Render(" └ ")
		formattedParams := baseStyle.
			Width(width - 2 - lipgloss.Width(toolNameText)).
			Foreground(t.TextMuted()).
			Render(params)
		parts = append(parts, lipgloss.JoinHorizontal(lipgloss.Left, prefix, toolNameText, formattedParams))
	}

	if toolCall.Name == agent.AgentToolName {
		taskMessages, _ := messagesService.List(context.Background(), toolCall.ID)
		toolCalls := []message.ToolCall{}
		for _, v := range taskMessages {
			toolCalls = append(toolCalls, v.ToolCalls()...)
		}
		for _, call := range toolCalls {
			rendered := renderToolMessage(call, []message.Message{}, messagesService, focusedUIMessageId, true, width, 0, expandedBlocks, nil)
			parts = append(parts, rendered.content)
		}
	}
	if responseContent != "" && !nested {
		parts = append(parts, responseContent)
	}
	for _, infoLine := range infoLines {
		parts = append(parts, infoLine)
	}

	content := style.Render(
		lipgloss.JoinVertical(
			lipgloss.Left,
			parts...,
		),
	)
	if nested {
		content = lipgloss.JoinVertical(
			lipgloss.Left,
			parts...,
		)
	}
	toolMsg := uiMessage{
		ID:          toolBlockID,
		messageType: toolMessageType,
		position:    position,
		height:      lipgloss.Height(content),
		content:     content,
	}
	return toolMsg
}

// Helper function to format the time difference between two Unix timestamps
func formatTimestampDiff(start, end int64) string {
	diffSeconds := float64(end-start) / 1000.0 // Convert to seconds
	if diffSeconds < 1 {
		return fmt.Sprintf("%dms", int(diffSeconds*1000))
	}
	if diffSeconds < 60 {
		return fmt.Sprintf("%.1fs", diffSeconds)
	}
	return fmt.Sprintf("%.1fm", diffSeconds/60)
}
