package appconfig

import (
	"testing"
	"time"
)

func TestFromEnvDefaultsAndOverrides(t *testing.T) {
	t.Setenv("VIVATOM_HTTP_ADDRESS", "")
	t.Setenv("VIVATOM_BUILDER_TOKEN", "builder-secret-123")
	t.Setenv("VIVATOM_AUTH_RATE_LIMIT", "7")
	t.Setenv("VIVATOM_SHUTDOWN_TIMEOUT", "3s")
	config, err := FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if config.Address != ":8080" || config.AuthRateLimit != 7 || config.ShutdownTimeout != 3*time.Second {
		t.Fatalf("unexpected config: %+v", config)
	}
}

func TestFromEnvRejectsInvalidOperationalValues(t *testing.T) {
	t.Setenv("VIVATOM_BUILDER_TOKEN", "builder-secret-123")
	t.Setenv("VIVATOM_AUTH_RATE_LIMIT", "0")
	if _, err := FromEnv(); err == nil {
		t.Fatal("expected invalid rate limit to fail startup")
	}
}

func TestFromEnvRequiresBuilderToken(t *testing.T) {
	t.Setenv("VIVATOM_BUILDER_TOKEN", "")
	if _, err := FromEnv(); err == nil {
		t.Fatal("expected missing builder token to fail startup")
	}
	t.Setenv("VIVATOM_BUILDER_TOKEN", "short")
	if _, err := FromEnv(); err == nil {
		t.Fatal("expected short builder token to fail startup")
	}
}
