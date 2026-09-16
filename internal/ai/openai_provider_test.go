package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"vivatom-api-svc/internal/domain"
)

func TestOpenAIProviderBuildsPlanFromStream(t *testing.T) {
	planJSON := `{"productSummary":"任务板","targetUsers":["团队"],"features":["任务"],"pages":[{"name":"首页","purpose":"管理任务"}],"filePlan":[{"path":"/src/App.tsx","responsibility":"页面"}],"designDirection":"清晰","acceptanceChecks":["可创建任务"]}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("authorization = %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		midpoint := len(planJSON) / 2
		fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":%q}}]}\n\n", planJSON[:midpoint])
		fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":%q}}]}\n\n", planJSON[midpoint:])
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	provider := NewOpenAIProvider(Config{Mode: "openai", APIKey: "test-key", BaseURL: server.URL, Model: "test-model", Timeout: time.Second})
	plan, err := provider.Plan(context.Background(), "做一个任务板")
	if err != nil || plan.ProductSummary != "任务板" {
		t.Fatalf("plan = %#v error=%v", plan, err)
	}
}

func TestOpenAIProviderPlansFromIndependentRequirementBrief(t *testing.T) {
	planJSON := `{"productSummary":"任务板","targetUsers":["团队"],"features":["任务"],"pages":[{"name":"首页","purpose":"管理任务"}],"filePlan":[{"path":"/src/App.tsx","responsibility":"页面"}],"designDirection":"清晰","acceptanceChecks":["可创建任务"]}`
	briefJSON := `{"goal":"管理团队任务","users":["小团队"],"coreFlows":["创建并分配任务"],"constraints":["移动端可用"]}`
	var requests atomic.Int32
	models := make([]string, 0, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var requestBody struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Error(err)
		}
		models = append(models, requestBody.Model)
		content := briefJSON
		if requests.Add(1) == 2 {
			content = planJSON
		}
		fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":%q}}]}\n\ndata: [DONE]\n\n", content)
	}))
	defer server.Close()

	provider := NewOpenAIProvider(Config{APIKey: "key", BaseURL: server.URL, Model: "default", AnalystModel: "analyst-model", ArchitectModel: "architect-model", Timeout: time.Second})
	brief, err := provider.AnalyzeRequirements(context.Background(), "做一个任务板")
	if err != nil {
		t.Fatal(err)
	}
	plan, err := provider.PlanFromBrief(context.Background(), "做一个任务板", brief)
	if err != nil || plan.RequirementBrief == nil || plan.RequirementBrief.Goal != brief.Goal || requests.Load() != 2 || len(models) != 2 || models[0] != "analyst-model" || models[1] != "architect-model" {
		t.Fatalf("plan=%+v requests=%d models=%v err=%v", plan, requests.Load(), models, err)
	}
}

func TestBuilderRequestsShareReactSandboxContract(t *testing.T) {
	snapshot := domain.ProjectSnapshot{
		Source: "vibe", Title: "Tasks", Summary: "Task board", EntryFile: "/src/App.tsx",
		Files:        map[string]string{"/src/main.tsx": "main", "/src/App.tsx": "app", "/src/styles.css": "css"},
		Dependencies: map[string]string{"react": "18.3.1", "react-dom": "18.3.1"},
	}
	snapshotJSON, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	var systems []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		systems = append(systems, body.Messages[0].Content)
		fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":%q}}]}\n\ndata: [DONE]\n\n", snapshotJSON)
	}))
	defer server.Close()
	provider := NewOpenAIProvider(Config{APIKey: "key", BaseURL: server.URL, Model: "model", Timeout: time.Second})
	plan := domain.BuildPlan{}
	if _, err := provider.Build(context.Background(), "build tasks", plan); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Revise(context.Background(), domain.ActionIterate, "add filters", snapshot); err != nil {
		t.Fatal(err)
	}
	for index, system := range systems {
		for _, required := range []string{
			"React + TypeScript",
			"real, testable interaction",
			"project-specific localStorage key",
			"Do not use network requests",
		} {
			if !strings.Contains(system, required) {
				t.Fatalf("request %d omitted React sandbox contract %q", index, required)
			}
		}
	}
}

func TestOpenAIProviderRetriesTransientStatus(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if requests.Add(1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"{}\"}}]}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()
	provider := NewOpenAIProvider(Config{APIKey: "key", BaseURL: server.URL, Model: "model", Timeout: time.Second, MaxRetries: 1})
	var result map[string]any
	if err := provider.completeJSON(context.Background(), "system", "user", &result); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 2 {
		t.Fatalf("requests = %d", requests.Load())
	}
}

func TestOpenAIProviderRetriesInvalidOutputWithoutLeakingPartialFields(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		content := `{"name":"accepted"}`
		if requests.Add(1) == 1 {
			content = `{"name":"rejected","description":"must not leak","unknown":true}`
		}
		fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":%q}}]}\n\ndata: [DONE]\n\n", content)
	}))
	defer server.Close()
	provider := NewOpenAIProvider(Config{APIKey: "key", BaseURL: server.URL, Model: "model", Timeout: time.Second, MaxRetries: 1})
	result := struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}{Name: "original", Description: "original"}
	if err := provider.completeJSON(context.Background(), "system", "user", &result); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 2 || result.Name != "accepted" || result.Description != "" {
		t.Fatalf("requests=%d result=%+v", requests.Load(), result)
	}
}

func TestStrictCompletionJSONRejectsAmbiguousDocuments(t *testing.T) {
	var target struct {
		Name string `json:"name"`
	}
	if err := decodeStrictJSON(`{"name":"one"}`, &target); err != nil || target.Name != "one" {
		t.Fatalf("valid JSON rejected: target=%+v err=%v", target, err)
	}
	for _, content := range []string{
		`{"name":"one"}{"name":"two"}`,
		`{"name":"one","name":"two"}`,
		`[{"name":"one"}]`,
		`{"name":"one","unknown":true}`,
	} {
		if err := decodeStrictJSON(content, &target); err == nil {
			t.Fatalf("ambiguous JSON accepted: %s", content)
		}
	}
}

func TestCompletionStreamRequiresDoneAndBoundsOutput(t *testing.T) {
	withoutDone := `data: {"choices":[{"delta":{"content":"{}"}}]}` + "\n\n"
	if _, err := readCompletionStream(strings.NewReader(withoutDone)); err == nil {
		t.Fatal("stream without done marker was accepted")
	}
	chunk := strings.Repeat("x", 1024*1024)
	var stream strings.Builder
	for range 5 {
		encoded, err := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]string{"content": chunk}}}})
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&stream, "data: %s\n\n", encoded)
	}
	stream.WriteString("data: [DONE]\n\n")
	if _, err := readCompletionStream(strings.NewReader(stream.String())); err == nil {
		t.Fatal("oversized completion was accepted")
	}
}

func TestProviderErrorsAreSafeAndClassified(t *testing.T) {
	code, message, retryable := NormalizeError(invalidOutput(errors.New("secret upstream body")))
	if code != "provider_output_invalid" || !retryable || strings.Contains(message, "secret") {
		t.Fatalf("normalized = %q %q %v", code, message, retryable)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(80 * time.Millisecond)
	}))
	defer server.Close()
	provider := NewOpenAIProvider(Config{APIKey: "key", BaseURL: server.URL, Model: "model", Timeout: 10 * time.Millisecond})
	var result map[string]any
	err := provider.completeJSON(context.Background(), "system", "user", &result)
	code, _, retryable = NormalizeError(err)
	if code != "provider_timeout" || !retryable {
		t.Fatalf("timeout normalized = %q, error=%v", code, err)
	}
}

func TestConfigAutoFallsBackToFake(t *testing.T) {
	for _, name := range []string{"VIVATOM_AI_PROVIDER", "VIVATOM_AI_API_KEY", "OPENAI_API_KEY", "VIVATOM_AI_TIMEOUT", "VIVATOM_AI_MAX_RETRIES", "VIVATOM_AI_ANALYST_MODEL", "VIVATOM_AI_ARCHITECT_MODEL", "VIVATOM_AI_BUILDER_MODEL"} {
		t.Setenv(name, "")
	}
	config, err := ConfigFromEnv()
	if err != nil || config.Mode != "fake" {
		t.Fatalf("config = %#v error=%v", config, err)
	}
	if _, ok := NewProvider(config).(*FakeProvider); !ok {
		t.Fatal("auto mode did not create fake provider")
	}
}

func TestConfigSupportsPerRoleModels(t *testing.T) {
	t.Setenv("VIVATOM_AI_PROVIDER", "openai")
	t.Setenv("VIVATOM_AI_API_KEY", "key")
	t.Setenv("VIVATOM_AI_MODEL", "default-model")
	t.Setenv("VIVATOM_AI_ANALYST_MODEL", "fast-model")
	t.Setenv("VIVATOM_AI_ARCHITECT_MODEL", "reasoning-model")
	t.Setenv("VIVATOM_AI_BUILDER_MODEL", "coding-model")
	config, err := ConfigFromEnv()
	if err != nil || config.AnalystModel != "fast-model" || config.ArchitectModel != "reasoning-model" || config.BuilderModel != "coding-model" {
		t.Fatalf("config=%+v err=%v", config, err)
	}
}

func TestLocalTemplateSelectsProductTypeAndFulfillsFilePlan(t *testing.T) {
	provider := &FakeProvider{}
	plan := domain.BuildPlan{FilePlan: []domain.PlanFile{{Path: "/src/App.tsx"}, {Path: "/src/components/Metrics.tsx"}}}
	snapshot, err := provider.Build(context.Background(), "收入分析 Dashboard", plan)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Title != "数据分析 Dashboard" || snapshot.Source != "template" {
		t.Fatalf("unexpected template: %+v", snapshot)
	}
	if _, exists := snapshot.Files["/src/components/Metrics.tsx"]; !exists {
		t.Fatal("local template did not fulfill the approved file plan")
	}
}
