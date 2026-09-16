package generation

import (
	"strings"
	"testing"

	"vivatom-api-svc/internal/domain"
)

func validSnapshot() domain.ProjectSnapshot {
	return domain.ProjectSnapshot{
		Source: "template", Title: "Task", Summary: "Board",
		EntryFile: "/src/App.tsx",
		Files: map[string]string{
			"/src/main.tsx":  `import App from "./App"`,
			"/src/App.tsx":   `export default function App() { return <main>Task</main> }`,
			"/src/style.css": `main { color: green; }`,
		},
		Dependencies: map[string]string{"react": "latest", "react-dom": "latest"},
		Backend:      domain.BackendSpec{Auth: "none", Collections: []domain.BackendCollection{}},
	}
}

func TestGuardAcceptsAndPinsDependency(t *testing.T) {
	got, err := NewGuard().Check(validSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	if got.Dependencies["react"] != "18.3.1" {
		t.Fatalf("react version = %q", got.Dependencies["react"])
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
		{"missing react", func(s *domain.ProjectSnapshot) { delete(s.Dependencies, "react") }, "dependency.missing"},
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
	if input.Dependencies["react"] != "latest" {
		t.Fatal("guard mutated provider-owned input")
	}
}
