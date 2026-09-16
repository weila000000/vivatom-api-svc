package generation

import (
	"strings"
	"testing"

	"vivatom-api-svc/internal/domain"
)

func validSnapshot() domain.ProjectSnapshot {
	return domain.ProjectSnapshot{
		Source: "template", Title: "Task", Summary: "Board",
		EntryFile: "/src/main.ts",
		Files: map[string]string{
			"/src/main.ts":   `import App from "./App.vue"`,
			"/src/App.vue":   `<template><main>Task</main></template>`,
			"/src/style.css": `main { color: green; }`,
		},
		Dependencies: map[string]string{"vue": "latest"},
		Backend:      domain.BackendSpec{Auth: "none", Collections: []domain.BackendCollection{}},
	}
}

func TestGuardAcceptsAndPinsDependency(t *testing.T) {
	got, err := NewGuard().Check(validSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	if got.Dependencies["vue"] != "3.5.42" {
		t.Fatalf("vue version = %q", got.Dependencies["vue"])
	}
}

func TestGuardRejectsUnsafeSnapshots(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*domain.ProjectSnapshot)
		code   string
	}{
		{"traversal", func(s *domain.ProjectSnapshot) { s.Files["/src/../secret.ts"] = "x" }, "path.invalid"},
		{"missing entry", func(s *domain.ProjectSnapshot) { delete(s.Files, s.EntryFile) }, "entry.missing"},
		{"network", func(s *domain.ProjectSnapshot) { s.Files["/src/main.ts"] = `fetch("/secret")` }, "source.fetch"},
		{"remote css", func(s *domain.ProjectSnapshot) { s.Files["/src/style.css"] = `@import "https://bad.test/a.css";` }, "source.remote_css"},
		{"dependency", func(s *domain.ProjectSnapshot) { s.Dependencies["axios"] = "latest" }, "dependency.denied"},
		{"missing vue", func(s *domain.ProjectSnapshot) { delete(s.Dependencies, "vue") }, "dependency.missing"},
		{"large file", func(s *domain.ProjectSnapshot) { s.Files["/src/main.ts"] = strings.Repeat("x", MaxSnapshotFileBytes+1) }, "file.too_large"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snapshot := validSnapshot()
			tt.mutate(&snapshot)
			_, err := NewGuard().Check(snapshot)
			rejected, ok := err.(*RejectedError)
			if !ok || rejected.Code != tt.code {
				t.Fatalf("error = %#v, want code %s", err, tt.code)
			}
		})
	}
}

func TestGuardDoesNotMutateInputDependencies(t *testing.T) {
	input := validSnapshot()
	_, err := NewGuard().Check(input)
	if err != nil {
		t.Fatal(err)
	}
	if input.Dependencies["vue"] != "latest" {
		t.Fatal("guard mutated provider-owned input")
	}
}
