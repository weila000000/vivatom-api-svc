package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"vivatom-api-svc/internal/domain"
)

type OpenAIProvider struct {
	config Config
	client *http.Client
}

func NewOpenAIProvider(config Config) *OpenAIProvider {
	if config.AnalystModel == "" {
		config.AnalystModel = config.Model
	}
	if config.ArchitectModel == "" {
		config.ArchitectModel = config.Model
	}
	if config.BuilderModel == "" {
		config.BuilderModel = config.Model
	}
	return &OpenAIProvider{config: config, client: &http.Client{Timeout: config.Timeout}}
}

func (p *OpenAIProvider) AnalyzeRequirements(ctx context.Context, prompt string) (domain.RequirementBrief, error) {
	var brief domain.RequirementBrief
	err := p.completeJSON(ctx, "You are a product analyst. Convert the user's request into exactly one JSON object with: goal string, users string[], coreFlows string[], constraints string[]. Keep every item concrete and concise. Include only requirements supported by the user request; put necessary engineering boundaries in constraints. No markdown or code fences.", prompt, &brief)
	if err != nil {
		return domain.RequirementBrief{}, err
	}
	if strings.TrimSpace(brief.Goal) == "" || len(brief.Users) == 0 || len(brief.CoreFlows) == 0 {
		return domain.RequirementBrief{}, invalidOutput(errors.New("incomplete requirement brief"))
	}
	return brief, nil
}

func (p *OpenAIProvider) PlanFromBrief(ctx context.Context, prompt string, brief domain.RequirementBrief) (domain.BuildPlan, error) {
	briefJSON, err := json.Marshal(brief)
	if err != nil {
		return domain.BuildPlan{}, invalidOutput(err)
	}
	plan, err := p.plan(ctx, fmt.Sprintf("Original requirement:\n%s\n\nProduct analyst brief:\n%s", prompt, briefJSON))
	if err != nil {
		return domain.BuildPlan{}, err
	}
	plan.RequirementBrief = &brief
	return plan, nil
}

func (p *OpenAIProvider) Plan(ctx context.Context, prompt string) (domain.BuildPlan, error) {
	return p.plan(ctx, prompt)
}

func (p *OpenAIProvider) plan(ctx context.Context, prompt string) (domain.BuildPlan, error) {
	var plan domain.BuildPlan
	err := p.completeJSON(ctx, "You are a product architect. Return exactly one JSON object with this shape: productType string, productSummary string, targetUsers string[], features string[], pages [{name,purpose}], filePlan [{path,responsibility}], designDirection string, acceptanceChecks string[], backend {enabled boolean, auth string, collections [{name,label,access string,fields [{name,label,type string,required boolean}]}]}. Hard constraints: productType MUST be exactly website or web_app; every filePlan.path MUST start with /src/ and end in .vue, .ts, .tsx, .js, .jsx, .css, or .json; backend.auth MUST be exactly none or email_password; collection access MUST be exactly public or owner; collection and field names MUST match ^[a-z][a-z0-9_]{0,47}$ using lowercase snake_case; field type MUST be exactly text, long_text, number, boolean, or date; when backend.enabled is false, auth MUST be none and collections MUST be empty; owner collections require email_password auth. Use Vue 3. Enum values and paths must not be translated. No markdown or code fences.", prompt, &plan)
	if err != nil {
		return domain.BuildPlan{}, err
	}
	if strings.TrimSpace(plan.ProductSummary) == "" || len(plan.Features) == 0 || len(plan.FilePlan) == 0 {
		return domain.BuildPlan{}, invalidOutput(errors.New("incomplete build plan"))
	}
	return plan, nil
}

func (p *OpenAIProvider) Build(ctx context.Context, prompt string, plan domain.BuildPlan) (domain.ProjectSnapshot, error) {
	planJSON, err := json.Marshal(plan)
	if err != nil {
		return domain.ProjectSnapshot{}, invalidOutput(err)
	}
	userPrompt := fmt.Sprintf("User requirement:\n%s\n\nApproved plan:\n%s", prompt, planJSON)
	var snapshot domain.ProjectSnapshot
	err = p.completeJSON(ctx, "You are a senior Vue 3 engineer. Return one JSON object: source string, title string, summary string, files object mapping absolute /src paths to complete file contents, dependencies object mapping package names to versions, entryFile string, backend matching the approved plan. Include /src/main.ts, /src/App.vue and /src/styles.css. The only package dependency and bare import allowed is vue at version 3.5.42; implement everything else with local source files and browser APIs. No markdown or code fences.", userPrompt, &snapshot)
	if err != nil {
		return domain.ProjectSnapshot{}, err
	}
	if strings.TrimSpace(snapshot.Title) == "" || snapshot.EntryFile == "" || len(snapshot.Files) < 3 {
		return domain.ProjectSnapshot{}, invalidOutput(errors.New("incomplete snapshot"))
	}
	return snapshot, nil
}

func (p *OpenAIProvider) Revise(ctx context.Context, action domain.AgentAction, instruction string, current domain.ProjectSnapshot) (domain.ProjectSnapshot, error) {
	currentJSON, err := json.Marshal(current)
	if err != nil {
		return domain.ProjectSnapshot{}, invalidOutput(err)
	}
	userPrompt := fmt.Sprintf("Operation: %s\nInstruction: %s\n\nCurrent complete snapshot:\n%s", action, instruction, currentJSON)
	var snapshot domain.ProjectSnapshot
	err = p.completeJSON(ctx, "You are a senior Vue 3 engineer revising an existing application. Return the complete replacement snapshot as one JSON object with source, title, summary, files, dependencies, entryFile, and backend. Preserve working features unless the instruction changes them. For repair, fix the supplied problem. For polish, improve usability and visual quality. Include every required file, not a diff. The only package dependency and bare import allowed is vue at version 3.5.42; implement everything else with local source files and browser APIs. No markdown or code fences.", userPrompt, &snapshot)
	if err != nil {
		return domain.ProjectSnapshot{}, err
	}
	if strings.TrimSpace(snapshot.Title) == "" || snapshot.EntryFile == "" || len(snapshot.Files) < 3 {
		return domain.ProjectSnapshot{}, invalidOutput(errors.New("incomplete revised snapshot"))
	}
	return snapshot, nil
}

func (p *OpenAIProvider) completeJSON(ctx context.Context, system, user string, target any) error {
	model := p.config.Model
	switch {
	case strings.HasPrefix(system, "You are a product analyst"):
		model = p.config.AnalystModel
	case strings.HasPrefix(system, "You are a product architect"):
		model = p.config.ArchitectModel
	case strings.HasPrefix(system, "You are a senior Vue 3 engineer"):
		model = p.config.BuilderModel
	}
	payload := map[string]any{
		"model": model, "stream": true,
		"response_format": map[string]string{"type": "json_object"},
		"messages":        []map[string]string{{"role": "system", "content": system}, {"role": "user", "content": user}},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return invalidOutput(err)
	}
	var last error
	for attempt := 0; attempt <= p.config.MaxRetries; attempt++ {
		content, retry, err := p.request(ctx, body)
		if err == nil {
			decoder := json.NewDecoder(strings.NewReader(content))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(target); err != nil {
				return invalidOutput(err)
			}
			return nil
		}
		last = err
		if !retry || attempt == p.config.MaxRetries || ctx.Err() != nil {
			return err
		}
		timer := time.NewTimer(time.Duration(attempt+1) * 100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return last
}

func (p *OpenAIProvider) request(ctx context.Context, body []byte) (string, bool, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, p.config.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", false, &ProviderError{Code: "provider_error", Retryable: false, Cause: err}
	}
	request.Header.Set("Authorization", "Bearer "+p.config.APIKey)
	request.Header.Set("Content-Type", "application/json")
	response, err := p.client.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return "", false, ctx.Err()
		}
		return "", true, &ProviderError{Code: "provider_error", Retryable: true, Cause: err}
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.CopyN(io.Discard, response.Body, 64*1024)
		switch response.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return "", false, &ProviderError{Code: "provider_unauthorized", Retryable: false}
		case http.StatusTooManyRequests:
			return "", true, &ProviderError{Code: "provider_rate_limited", Retryable: true}
		default:
			retry := response.StatusCode >= 500
			return "", retry, &ProviderError{Code: "provider_error", Retryable: retry}
		}
	}
	content, err := readCompletionStream(response.Body)
	if err != nil {
		return "", true, err
	}
	return content, false, nil
}

func readCompletionStream(reader io.Reader) (string, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), 10*1024*1024)
	var content strings.Builder
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return "", invalidOutput(err)
		}
		for _, choice := range chunk.Choices {
			content.WriteString(choice.Delta.Content)
		}
	}
	if err := scanner.Err(); err != nil {
		return "", &ProviderError{Code: "provider_error", Retryable: true, Cause: err}
	}
	if strings.TrimSpace(content.String()) == "" {
		return "", invalidOutput(errors.New("empty completion"))
	}
	return content.String(), nil
}

func invalidOutput(err error) error {
	return &ProviderError{Code: "provider_output_invalid", Retryable: true, Cause: err}
}
