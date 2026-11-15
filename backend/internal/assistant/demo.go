package assistant

import (
	"context"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"ai-buddy/backend/internal/ai"
	"ai-buddy/backend/internal/onboarding"
)

func demoRoleReply(identified bool) *onboarding.Generation {
	if identified {
		return &onboarding.Generation{Answer: "В доступных инструкциях не нашёл, какую роль выбрать для этого ресурса."}
	}
	question := "Для какого инструмента или ресурса нужна роль?"
	return &onboarding.Generation{Answer: question, ClarifyingQuestion: &question}
}

// DemoClient uses the same scoped search and source validation as a live model.
type DemoClient struct{}

func (DemoClient) Generate(_ context.Context, input ai.Input) (ai.Output, error) {
	words := questionWords(input.Question)
	if slices.Contains(words, "нужен") && slices.Contains(words, "доступ") &&
		(slices.Contains(words, "gitlab") || slices.Contains(words, "гитлаб")) {
		for _, resource := range input.Catalog {
			if resource.ToolID != "gitlab" {
				continue
			}
			for _, chunk := range input.Chunks {
				if referencesResource(chunk.Text, len(chunk.Text), resource.ResourceID) {
					return ai.Output{
						Answer:        "Учебный режим. Проверьте ресурс и укажите обоснование перед подтверждением.",
						SourceIDs:     []string{chunk.ID},
						DraftProposal: &ai.Draft{ToolID: resource.ToolID, ResourceID: resource.ResourceID},
					}, nil
				}
			}
		}
	}
	for _, chunk := range input.Chunks {
		if isRoleFollowUp(input.Question) || demoMatches(words, chunk, input.Catalog) {
			return ai.Output{Answer: "Учебный режим. Фрагмент из базы знаний:\n\n" + chunk.Text, SourceIDs: []string{chunk.ID}}, nil
		}
	}
	return ai.Output{Answer: "В доступных материалах нет ответа на этот вопрос. Уточни тему или выбери пример рядом с чатом."}, nil
}

func questionWords(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

func demoMatches(question []string, chunk ai.Chunk, catalog []ai.Resource) bool {
	title := questionWords(chunk.Title)
	for _, word := range question {
		switch word {
		case "как", "где", "что", "кто", "куда", "мне", "мой", "моей", "можно",
			"нужно", "нужен", "написать", "найти", "получить", "доступ", "учебный", "учебному", "команда", "команды":
			continue
		}
		if utf8.RuneCountInString(word) > 1 && slices.Contains(title, word) {
			return true
		}
		for _, resource := range catalog {
			if word == resource.ToolID && referencesResource(chunk.Text, len(chunk.Text), resource.ResourceID) {
				return true
			}
		}
	}
	return false
}
