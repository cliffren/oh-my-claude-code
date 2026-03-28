package prompt

import "github.com/Krontx/oh-my-claude-code/internal/llm/models"

func TitlePrompt(_ models.ModelProvider) string {
	return `Generate a very short title for the user's message.
Be extremely concise. No quotes, no colons, no punctuation.
Return ONLY the title text, nothing else.`
}
