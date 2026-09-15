package domain

import "testing"

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
	previous := ProjectSnapshot{Backend: BackendSpec{Auth: "none", Collections: []BackendCollection{}}}
	candidate := previous
	if err := ValidateRevisionContract(previous, candidate); err != nil {
		t.Fatal(err)
	}
	candidate.Backend.Auth = "email_password"
	assertContractCode(t, ValidateRevisionContract(previous, candidate), "snapshot.backend_changed_without_approval")
}

func assertContractCode(t *testing.T, err error, want string) {
	t.Helper()
	value, ok := err.(*ContractError)
	if !ok || value.Code != want {
		t.Fatalf("want %s, got %v", want, err)
	}
}
