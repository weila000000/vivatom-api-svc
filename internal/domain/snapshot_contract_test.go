package domain

import (
	"strings"
	"testing"
)

func TestBuildContractEnforcesApprovedFiles(t *testing.T) {
	plan := BuildPlan{FilePlan: []PlanFile{{Path: "/src/App.tsx"}}}
	snapshot := ProjectSnapshot{Title: "Task", Summary: "Board", Files: map[string]string{"/src/App.tsx": "app"}}
	if err := ValidateBuildContract(plan, snapshot); err != nil {
		t.Fatal(err)
	}
	delete(snapshot.Files, "/src/App.tsx")
	assertContractCode(t, ValidateBuildContract(plan, snapshot), "snapshot.planned_file_missing")
}

func TestSnapshotMetadataAppliesToBuildsAndRevisions(t *testing.T) {
	plan := BuildPlan{}
	previous := ProjectSnapshot{Title: "Task", Summary: "Board"}
	candidate := previous
	candidate.Title = "  "
	assertContractCode(t, ValidateBuildContract(plan, candidate), "snapshot.title_missing")
	assertContractCode(t, ValidateRevisionContract(previous, candidate), "snapshot.title_missing")
	candidate = previous
	candidate.Summary = strings.Repeat("界", 2001)
	assertContractCode(t, ValidateRevisionContract(previous, candidate), "snapshot.summary_too_long")
}

func assertContractCode(t *testing.T, err error, want string) {
	t.Helper()
	value, ok := err.(*ContractError)
	if !ok || value.Code != want {
		t.Fatalf("want %s, got %v", want, err)
	}
}
