package ai

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestOpenAIProviderBuildsPlanFromStream(t *testing.T) {
	planJSON := `{"productType":"web_app","productSummary":"任务板","targetUsers":["团队"],"features":["任务"],"pages":[{"name":"首页","purpose":"管理任务"}],"filePlan":[{"path":"/src/App.vue","responsibility":"页面"}],"designDirection":"清晰","acceptanceChecks":["可创建任务"],"backend":{"enabled":false,"auth":"none","collections":[]}}`
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
	for _, name := range []string{"VIVATOM_AI_PROVIDER", "VIVATOM_AI_API_KEY", "OPENAI_API_KEY", "VIVATOM_AI_TIMEOUT", "VIVATOM_AI_MAX_RETRIES"} {
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
