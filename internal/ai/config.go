package ai

import (
	"errors"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Mode           string
	APIKey         string
	BaseURL        string
	Model          string
	AnalystModel   string
	ArchitectModel string
	BuilderModel   string
	Timeout        time.Duration
	MaxRetries     int
}

func ConfigFromEnv() (Config, error) {
	config := Config{
		Mode:           strings.ToLower(strings.TrimSpace(os.Getenv("VIVATOM_AI_PROVIDER"))),
		APIKey:         strings.TrimSpace(os.Getenv("VIBE_API_KEY")),
		BaseURL:        strings.TrimRight(strings.TrimSpace(os.Getenv("VIBE_BASE_URL")), "/"),
		Model:          strings.TrimSpace(os.Getenv("VIBE_MODEL")),
		AnalystModel:   strings.TrimSpace(os.Getenv("VIVATOM_AI_ANALYST_MODEL")),
		ArchitectModel: strings.TrimSpace(os.Getenv("VIVATOM_AI_ARCHITECT_MODEL")),
		BuilderModel:   strings.TrimSpace(os.Getenv("VIVATOM_AI_BUILDER_MODEL")),
		Timeout:        5 * time.Minute, MaxRetries: 2,
	}
	if config.APIKey == "" {
		config.APIKey = strings.TrimSpace(os.Getenv("VIVATOM_AI_API_KEY"))
	}
	if config.BaseURL == "" {
		config.BaseURL = strings.TrimRight(strings.TrimSpace(os.Getenv("VIVATOM_AI_BASE_URL")), "/")
	}
	if config.Model == "" {
		config.Model = strings.TrimSpace(os.Getenv("VIVATOM_AI_MODEL"))
	}
	if config.APIKey == "" {
		config.APIKey = strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	}
	if config.BaseURL == "" {
		config.BaseURL = strings.TrimRight(strings.TrimSpace(os.Getenv("OPENAI_BASE_URL")), "/")
	}
	if config.Model == "" {
		config.Model = strings.TrimSpace(os.Getenv("OPENAI_MODEL"))
	}
	if value := strings.TrimSpace(os.Getenv("VIVATOM_AI_TIMEOUT")); value != "" {
		duration, err := time.ParseDuration(value)
		if err != nil || duration <= 0 {
			return Config{}, errors.New("invalid VIVATOM_AI_TIMEOUT")
		}
		config.Timeout = duration
	}
	if value := strings.TrimSpace(os.Getenv("VIVATOM_AI_MAX_RETRIES")); value != "" {
		retries, err := strconv.Atoi(value)
		if err != nil || retries < 0 || retries > 5 {
			return Config{}, errors.New("invalid VIVATOM_AI_MAX_RETRIES")
		}
		config.MaxRetries = retries
	}
	if config.Mode == "" || config.Mode == "auto" {
		if config.APIKey == "" {
			config.Mode = "fake"
		} else {
			config.Mode = "openai"
		}
	}
	if config.Mode == "fake" {
		return config, nil
	}
	if config.Mode != "openai" {
		return Config{}, errors.New("unsupported VIVATOM_AI_PROVIDER")
	}
	if config.APIKey == "" {
		return Config{}, errors.New("VIVATOM_AI_API_KEY is required")
	}
	if config.BaseURL == "" {
		config.BaseURL = "https://vibe.linux008.com/v1"
	}
	if config.Model == "" {
		config.Model = "gpt-6-astra"
	}
	if config.AnalystModel == "" {
		config.AnalystModel = config.Model
	}
	if config.ArchitectModel == "" {
		config.ArchitectModel = config.Model
	}
	if config.BuilderModel == "" {
		config.BuilderModel = config.Model
	}
	return config, nil
}

func NewProvider(config Config) Provider {
	if config.Mode == "openai" {
		return NewOpenAIProvider(config)
	}
	return NewFakeProvider()
}
