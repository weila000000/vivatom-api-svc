package appconfig

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Address         string
	DatabasePath    string
	AllowedOrigin   string
	TrustedProxies  []string
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	IdleTimeout     time.Duration
	ShutdownTimeout time.Duration
	AuthRateLimit   int
	AgentRateLimit  int
	RateLimitWindow time.Duration
}

func FromEnv() (Config, error) {
	config := Config{
		Address:         value("VIVATOM_HTTP_ADDRESS", ":8080"),
		DatabasePath:    value("VIVATOM_RUNTIME_DATABASE_PATH", ".data/vivatom-runtime.db"),
		AllowedOrigin:   value("VIVATOM_ALLOWED_ORIGIN", "http://localhost:5173"),
		TrustedProxies:  csv("VIVATOM_TRUSTED_PROXIES"),
		ReadTimeout:     15 * time.Second,
		WriteTimeout:    5 * time.Minute,
		IdleTimeout:     60 * time.Second,
		ShutdownTimeout: 10 * time.Second,
		AuthRateLimit:   20,
		AgentRateLimit:  10,
		RateLimitWindow: time.Minute,
	}
	var err error
	if config.ReadTimeout, err = duration("VIVATOM_HTTP_READ_TIMEOUT", config.ReadTimeout); err != nil {
		return Config{}, err
	}
	if config.WriteTimeout, err = duration("VIVATOM_HTTP_WRITE_TIMEOUT", config.WriteTimeout); err != nil {
		return Config{}, err
	}
	if config.IdleTimeout, err = duration("VIVATOM_HTTP_IDLE_TIMEOUT", config.IdleTimeout); err != nil {
		return Config{}, err
	}
	if config.ShutdownTimeout, err = duration("VIVATOM_SHUTDOWN_TIMEOUT", config.ShutdownTimeout); err != nil {
		return Config{}, err
	}
	if config.RateLimitWindow, err = duration("VIVATOM_RATE_LIMIT_WINDOW", config.RateLimitWindow); err != nil {
		return Config{}, err
	}
	if config.AuthRateLimit, err = positiveInt("VIVATOM_AUTH_RATE_LIMIT", config.AuthRateLimit); err != nil {
		return Config{}, err
	}
	if config.AgentRateLimit, err = positiveInt("VIVATOM_AGENT_RATE_LIMIT", config.AgentRateLimit); err != nil {
		return Config{}, err
	}
	return config, nil
}

func csv(key string) []string {
	var result []string
	for _, item := range strings.Split(os.Getenv(key), ",") {
		if item = strings.TrimSpace(item); item != "" {
			result = append(result, item)
		}
	}
	return result
}

func value(key, fallback string) string {
	if result := os.Getenv(key); result != "" {
		return result
	}
	return fallback
}

func duration(key string, fallback time.Duration) (time.Duration, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback, nil
	}
	result, err := time.ParseDuration(raw)
	if err != nil || result <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration", key)
	}
	return result, nil
}

func positiveInt(key string, fallback int) (int, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback, nil
	}
	result, err := strconv.Atoi(raw)
	if err != nil || result <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", key)
	}
	return result, nil
}
