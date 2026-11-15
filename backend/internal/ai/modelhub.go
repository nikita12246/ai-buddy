package ai

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"
)

const modelHubEndpoint = "https://modelhub.my/v1/chat/completions"

const modelHubJSONInstruction = "\nВерни только один JSON-объект. Не добавляй Markdown, блоки кода или текст вне JSON. Схема ответа:\n"

type ModelHub struct {
	apiKey string
	model  string
	http   *http.Client
}

func NewModelHub(apiKey, model string, timeout time.Duration) (*ModelHub, error) {
	apiKey = strings.TrimSpace(apiKey)
	model = strings.TrimSpace(model)
	if apiKey == "" || model == "" || timeout <= 0 {
		return nil, ErrConfiguration
	}
	return &ModelHub{apiKey: apiKey, model: model, http: providerHTTPClient(timeout)}, nil
}

func (c *ModelHub) Generate(ctx context.Context, input Input) (Output, error) {
	contextJSON, err := prepareInput(ctx, input)
	if err != nil {
		return Output{}, err
	}
	data, err := postJSON(ctx, c.http, modelHubEndpoint, c.apiKey, "", map[string]any{
		"model":      c.model,
		"max_tokens": 1024,
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt + modelHubJSONInstruction + string(responseSchema)},
			{"role": "user", "content": contextJSON},
		},
	})
	if err != nil {
		return Output{}, err
	}
	var response struct {
		Choices []struct {
			FinishReason string `json:"finish_reason"`
			Message      struct {
				Content string `json:"content"`
				Refusal string `json:"refusal"`
			} `json:"message"`
		} `json:"choices"`
		Usage *struct {
			Prompt     int `json:"prompt_tokens"`
			Completion int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(data, &response); err != nil || len(response.Choices) != 1 {
		return Output{}, ErrInvalidResponse
	}
	choice := response.Choices[0]
	if choice.FinishReason != "stop" || choice.Message.Refusal != "" {
		return Output{}, ErrInvalidResponse
	}
	output, err := parseOutput(choice.Message.Content, input)
	if err != nil {
		return Output{}, err
	}
	if response.Usage != nil {
		output.Usage = &Usage{InputTokens: response.Usage.Prompt, OutputTokens: response.Usage.Completion}
		log.Printf("ModelHub reply model=%q input_tokens=%d output_tokens=%d", c.model, output.Usage.InputTokens, output.Usage.OutputTokens)
	} else {
		log.Printf("ModelHub reply model=%q usage=unavailable", c.model)
	}
	return output, nil
}
