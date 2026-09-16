package domain

import (
	"strings"
	"testing"
)

func TestBuildContractEnforcesApprovedFilesAndBackend(t *testing.T) {
	plan := BuildPlan{FilePlan: []PlanFile{{Path: "/src/App.vue"}}, Backend: BackendSpec{Enabled: false, Auth: "none", Collections: []BackendCollection{}}}
	snapshot := ProjectSnapshot{Title: "Task", Summary: "Board", Files: map[string]string{"/src/App.vue": "app"}, Backend: plan.Backend}
	if err := ValidateBuildContract(plan, snapshot); err != nil {
		t.Fatal(err)
	}
	delete(snapshot.Files, "/src/App.vue")
	assertContractCode(t, ValidateBuildContract(plan, snapshot), "snapshot.planned_file_missing")
	snapshot.Files["/src/App.vue"] = "app"
	snapshot.Backend.Enabled = true
	assertContractCode(t, ValidateBuildContract(plan, snapshot), "snapshot.backend_mismatch")
}

func TestRevisionContractRejectsUnapprovedBackendChanges(t *testing.T) {
	previous := ProjectSnapshot{Title: "Task", Summary: "Board", Backend: BackendSpec{Auth: "none", Collections: []BackendCollection{}}}
	candidate := previous
	if err := ValidateRevisionContract(previous, candidate); err != nil {
		t.Fatal(err)
	}
	candidate.Backend.Auth = "email_password"
	assertContractCode(t, ValidateRevisionContract(previous, candidate), "snapshot.backend_changed_without_approval")
}

func TestSnapshotMetadataAppliesToBuildsAndRevisions(t *testing.T) {
	plan := BuildPlan{Backend: BackendSpec{Auth: "none"}}
	previous := ProjectSnapshot{Title: "Task", Summary: "Board", Backend: plan.Backend}

	candidate := previous
	candidate.Title = "  "
	assertContractCode(t, ValidateBuildContract(plan, candidate), "snapshot.title_missing")
	assertContractCode(t, ValidateRevisionContract(previous, candidate), "snapshot.title_missing")

	candidate = previous
	candidate.Summary = ""
	assertContractCode(t, ValidateBuildContract(plan, candidate), "snapshot.summary_missing")
	assertContractCode(t, ValidateRevisionContract(previous, candidate), "snapshot.summary_missing")

	candidate = previous
	candidate.Title = strings.Repeat("界", 201)
	assertContractCode(t, ValidateRevisionContract(previous, candidate), "snapshot.title_too_long")
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
