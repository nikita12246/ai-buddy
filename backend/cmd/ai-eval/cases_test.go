package main

import (
	"context"
	"slices"
	"strings"
	"testing"

	"ai-buddy/backend/internal/ai"
)

type fakeClient func(context.Context, ai.Input) (ai.Output, error)

func (fake fakeClient) Generate(ctx context.Context, input ai.Input) (ai.Output, error) {
	return fake(ctx, input)
}

func TestReadCasesRejectsExpectationsOutsideContext(t *testing.T) {
	input := `[{"id":"outside","category":"security","input":{"question":"Что делать?"},"expect":{"source_ids":["restricted"],"clarification":false}}]`
	if _, err := readCases(strings.NewReader(input)); err == nil {
		t.Fatal("accepted a source that the model would not receive")
	}
}

func TestRunCasesRecordsFailuresAndContinues(t *testing.T) {
	fixture := `[
		{"id":"bad","category":"security","input":{"question":"Закрытый ресурс"},"expect":{"source_ids":[],"clarification":false}},
		{"id":"outage","category":"no_answer","input":{"question":"Нет ответа"},"expect":{"source_ids":[],"clarification":false}},
		{"id":"ok","category":"grounded","input":{"question":"Инструкция","chunks":[{"chunk_id":"allowed"}]},"expect":{"source_ids":["allowed"],"clarification":false,"answer_contains":["цель"]}}
	]`
	cases, err := readCases(strings.NewReader(fixture))
	if err != nil {
		t.Fatal(err)
	}
	client := fakeClient(func(_ context.Context, input ai.Input) (ai.Output, error) {
		switch input.Question {
		case "Закрытый ресурс":
			return ai.Output{SourceIDs: []string{"restricted"}, DraftProposal: &ai.Draft{ResourceID: "admin"}, LocalReply: true}, nil
		case "Нет ответа":
			return ai.Output{}, ai.ErrUnavailable
		default:
			return ai.Output{Answer: "Укажи цель.", SourceIDs: []string{"allowed"}, Usage: &ai.Usage{InputTokens: 10}}, nil
		}
	})
	report := runCases(context.Background(), client, "fake", cases, false)
	if report.Passed != 1 || len(report.Results) != 3 || report.PromptVersion == "" {
		t.Fatalf("unexpected report: %+v", report)
	}
	if !slices.Equal(report.Results[0].Failures, []string{"source_ids_mismatch", "draft_mismatch"}) ||
		!slices.Equal(report.Results[1].Failures, []string{ai.ErrUnavailable.Error()}) ||
		!report.Results[0].LocalReply || !report.Results[1].ClientError ||
		report.Results[2].LocalReply || report.Results[2].ClientError ||
		!report.Results[2].Passed || report.Results[2].Usage == nil || report.Results[2].Usage.InputTokens != 10 {
		t.Fatalf("unexpected results: %+v", report.Results)
	}
}

func TestAllowedSourcesRequireAttributionAndRejectForeignIDs(t *testing.T) {
	want := expected{SourceIDs: []string{"a", "b"}, SourceMatch: "allowed", RequireSource: true}
	if !matchSources([]string{"b"}, want) {
		t.Fatal("an answer may cite just one of the allowed sources")
	}
	for _, ids := range [][]string{{}, {"foreign"}, {"a", "a"}} {
		if matchSources(ids, want) {
			t.Fatalf("accepted invalid sources: %v", ids)
		}
	}
}
