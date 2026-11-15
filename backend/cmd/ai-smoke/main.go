package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"time"

	"ai-buddy/backend/internal/ai"
)

func main() {
	client, err := ai.NewClient(os.Getenv("LLM_PROVIDER"), os.Getenv("LLM_API_KEY"), os.Getenv("LLM_MODEL"), 25*time.Second)
	if err != nil {
		log.Fatal("Set LLM_PROVIDER, LLM_API_KEY and LLM_MODEL for the AI smoke check")
	}
	output, err := client.Generate(context.Background(), ai.Input{
		Question: "Какие данные надо указать в заявке на доступ к учебному GitLab?",
		Chunks: []ai.Chunk{{
			ID:    "demo-gitlab-access-01",
			Title: "Учебная инструкция GitLab",
			Text:  "Для заявки на доступ к учебному GitLab укажи проект и цель доступа. Заявка отправляется только после подтверждения.",
		}},
	})
	if err != nil {
		log.Fatal(err)
	}
	if len(output.SourceIDs) != 1 || output.SourceIDs[0] != "demo-gitlab-access-01" || output.DraftProposal != nil {
		log.Fatal("Expected an answer citing the synthetic instruction without a draft proposal")
	}
	if err := json.NewEncoder(os.Stdout).Encode(output); err != nil {
		log.Fatal("Could not write the smoke result")
	}
}
