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
	"reflect"
	"strings"
	"time"

	"vivatom-api-svc/internal/domain"
)

type OpenAIProvider struct {
	config Config
	client *http.Client
}

const (
	maxCompletionBytes      = 4 * 1024 * 1024
	maxStreamEventBytes     = 8 * 1024 * 1024
	maxStreamBytes          = 32 * 1024 * 1024
	reactGenerationContract = ` Generate a browser-only React + TypeScript application for Sandpack. It must include at least one real, testable interaction. Persist user-created or mutable preview data with a project-specific localStorage key. Do not use network requests, authentication, server APIs, cookies, eval, dynamic Function, WebSocket, parent/top window access, or remote imports.`
)

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
	err := p.completeJSON(ctx, "You are Emma, a product strategist. Convert the user's request into exactly one JSON object with: goal string, users string[], coreFlows string[], constraints string[]. Keep every item concrete and concise. No markdown or code fences.", prompt, &brief)
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
	err := p.completeJSON(ctx, "You are Bob, a technical planner. Return exactly one JSON object with this shape: productSummary string, targetUsers string[], features string[], pages [{name,purpose}], filePlan [{path,responsibility}], designDirection string, acceptanceChecks string[]. Every filePlan.path must start with /src/ and use .ts, .tsx, .js, .jsx, .css, or .json. Plan a React + TypeScript browser application with at least one real interaction and localStorage persistence when data changes. No markdown or code fences.", prompt, &plan)
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
	err = p.completeJSON(ctx, "You are Alex, a senior React engineer. Return one JSON object: source string (vibe), title string, summary string, files object mapping absolute /src paths to complete file contents, dependencies object mapping package names to versions, and entryFile exactly /src/App.tsx. Include /src/main.tsx, /src/App.tsx and /src/styles.css."+reactGenerationContract+" Allowed packages are react, react-dom, lucide-react, recharts, and date-fns. No markdown or code fences.", userPrompt, &snapshot)
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
	err = p.completeJSON(ctx, "You are Alex, a senior React engineer revising an existing application. Return the complete replacement snapshot as one JSON object with source, title, summary, files, dependencies, and entryFile exactly /src/App.tsx. Preserve working features unless the instruction changes them. Include every required file."+reactGenerationContract+" Allowed packages are react, react-dom, lucide-react, recharts, and date-fns. No markdown or code fences.", userPrompt, &snapshot)
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
	case strings.HasPrefix(system, "You are Emma"):
		model = p.config.AnalystModel
	case strings.HasPrefix(system, "You are Bob"):
		model = p.config.ArchitectModel
	case strings.HasPrefix(system, "You are Alex"):
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
			if decodeErr := decodeStrictJSON(content, target); decodeErr == nil {
				return nil
			} else {
				err = invalidOutput(decodeErr)
				retry = true
			}
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
		case http.StatusPaymentRequired:
			return "", false, &ProviderError{Code: "provider_quota_exhausted", Retryable: false}
		case http.StatusRequestTimeout:
			return "", true, &ProviderError{Code: "provider_timeout", Retryable: true}
		case http.StatusTooManyRequests:
			return "", true, &ProviderError{Code: "provider_rate_limited", Retryable: true}
		case 529:
			return "", true, &ProviderError{Code: "provider_overloaded", Retryable: true}
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
	limited := &io.LimitedReader{R: reader, N: maxStreamBytes + 1}
	scanner := bufio.NewScanner(limited)
	scanner.Buffer(make([]byte, 64*1024), maxStreamEventBytes)
	var content strings.Builder
	done := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			done = true
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
		if len(chunk.Choices) > 1 {
			return "", invalidOutput(errors.New("multiple completion choices"))
		}
		for _, choice := range chunk.Choices {
			if content.Len()+len(choice.Delta.Content) > maxCompletionBytes {
				return "", invalidOutput(errors.New("completion too large"))
			}
			content.WriteString(choice.Delta.Content)
		}
	}
	if err := scanner.Err(); err != nil {
		return "", &ProviderError{Code: "provider_error", Retryable: true, Cause: err}
	}
	if limited.N == 0 {
		return "", invalidOutput(errors.New("completion stream too large"))
	}
	if !done {
		return "", &ProviderError{Code: "provider_error", Retryable: true, Cause: errors.New("completion stream ended before done")}
	}
	if strings.TrimSpace(content.String()) == "" {
		return "", invalidOutput(errors.New("empty completion"))
	}
	return content.String(), nil
}

func decodeStrictJSON(content string, target any) error {
	targetValue := reflect.ValueOf(target)
	if targetValue.Kind() != reflect.Pointer || targetValue.IsNil() {
		return errors.New("JSON target must be a non-nil pointer")
	}
	validator := json.NewDecoder(strings.NewReader(content))
	first, err := validator.Token()
	if err != nil {
		return err
	}
	if delimiter, ok := first.(json.Delim); !ok || delimiter != '{' {
		return errors.New("completion must be one JSON object")
	}
	if err = validateJSONObject(validator); err != nil {
		return err
	}
	if _, err = validator.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("trailing JSON value")
		}
		return err
	}
	candidate := reflect.New(targetValue.Elem().Type())
	decoder := json.NewDecoder(strings.NewReader(content))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(candidate.Interface()); err != nil {
		return err
	}
	if err = decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("trailing JSON value")
		}
		return err
	}
	targetValue.Elem().Set(candidate.Elem())
	return nil
}

func validateJSONObject(decoder *json.Decoder) error {
	keys := map[string]struct{}{}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		key, ok := token.(string)
		if !ok {
			return errors.New("invalid JSON object key")
		}
		if _, duplicate := keys[key]; duplicate {
			return fmt.Errorf("duplicate JSON key %q", key)
		}
		keys[key] = struct{}{}
		if err = validateJSONValue(decoder); err != nil {
			return err
		}
	}
	closing, err := decoder.Token()
	if err != nil {
		return err
	}
	if delimiter, ok := closing.(json.Delim); !ok || delimiter != '}' {
		return errors.New("invalid JSON object")
	}
	return nil
}

func validateJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, nested := token.(json.Delim)
	if !nested {
		return nil
	}
	switch delimiter {
	case '{':
		return validateJSONObject(decoder)
	case '[':
		for decoder.More() {
			if err = validateJSONValue(decoder); err != nil {
				return err
			}
		}
		closing, closeErr := decoder.Token()
		if closeErr != nil {
			return closeErr
		}
		if closing != json.Delim(']') {
			return errors.New("invalid JSON array")
		}
		return nil
	default:
		return errors.New("unexpected JSON delimiter")
	}
}

func invalidOutput(err error) error {
	return &ProviderError{Code: "provider_output_invalid", Retryable: true, Cause: err}
}
