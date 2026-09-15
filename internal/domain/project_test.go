package domain

import "testing"

func TestProjectHappyPath(t *testing.T) {
	status := ProjectDraft
	commands := []ProjectCommand{
		CommandStartPlan,
		CommandPlanReady,
		CommandApprove,
		CommandBuildSucceeded,
		CommandStartIteration,
	}
	want := []ProjectStatus{
		ProjectPlanning,
		ProjectAwaitingApproval,
		ProjectBuilding,
		ProjectReady,
		ProjectBuilding,
	}

	for i, command := range commands {
		var err error
		status, err = TransitionProject(status, command)
		if err != nil {
			t.Fatalf("transition %d: %v", i, err)
		}
		if status != want[i] {
			t.Fatalf("transition %d status = %q, want %q", i, status, want[i])
		}
	}
}

func TestProjectRejectsInvalidTransition(t *testing.T) {
	next, err := TransitionProject(ProjectDraft, CommandApprove)
	if err == nil {
		t.Fatal("draft project unexpectedly accepted approve")
	}
	if next != ProjectDraft {
		t.Fatalf("failed transition changed status to %q", next)
	}
}

func TestProjectRecoveryCommandsAreExplicit(t *testing.T) {
	for _, tt := range []struct {
		command ProjectCommand
		want    ProjectStatus
	}{
		{CommandRetryPlan, ProjectPlanning},
		{CommandRetryBuild, ProjectBuilding},
	} {
		got, err := TransitionProject(ProjectError, tt.command)
		if err != nil {
			t.Fatal(err)
		}
		if got != tt.want {
			t.Fatalf("%s produced %s, want %s", tt.command, got, tt.want)
		}
	}
}
