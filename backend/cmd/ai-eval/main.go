package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"ai-buddy/backend/internal/ai"
)

func main() {
	path := flag.String("cases", "../evals/starter-cases.json", "JSON file with evaluation cases")
	checkOnly := flag.Bool("check", false, "validate cases without calling a model")
	includeReview := flag.Bool("review", false, "include answers and expected facts for synthetic case review")
	flag.Parse()

	data, err := os.ReadFile(*path)
	if err != nil {
		log.Fatal(err)
	}
	cases, err := readCases(bytes.NewReader(data))
	if err != nil {
		log.Fatal(err)
	}
	if *checkOnly {
		fmt.Printf("Validated %d evaluation cases\n", len(cases))
		return
	}

	model := os.Getenv("LLM_MODEL")
	provider := os.Getenv("LLM_PROVIDER")
	client, err := ai.NewClient(provider, os.Getenv("LLM_API_KEY"), model, 25*time.Second)
	if err != nil {
		log.Fatal("Set LLM_PROVIDER, LLM_API_KEY and LLM_MODEL for evaluation")
	}
	report := runCases(context.Background(), client, model, cases, *includeReview)
	report.Provider = provider
	report.CasesSHA256 = fmt.Sprintf("%x", sha256.Sum256(data))
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		log.Fatal(err)
	}
	if report.Passed != len(report.Results) {
		os.Exit(1)
	}
}
