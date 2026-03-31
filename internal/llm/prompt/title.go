package prompt

import "github.com/cliffren/toc/internal/llm/models"

func TitlePrompt(_ models.ModelProvider) string {
	return `Generate a very short title for this conversation.
Focus on what the user asked or what was accomplished.
Be extremely concise. No quotes, no colons, no punctuation.
Return ONLY the title text, nothing else.`
}
