package domain

import "testing"

func TestAgentRequestValidate(t *testing.T) {
	snapshot := &ProjectSnapshot{}
	plan := validBuildPlan()

	tests := []struct {
		name    string
		request AgentRequest
		wantErr bool
	}{
		{"plan", AgentRequest{Action: ActionPlan, ProjectID: "p1", Prompt: "做一个任务板"}, false},
		{"build needs plan", AgentRequest{Action: ActionBuild, ProjectID: "p1", Prompt: "开始"}, true},
		{"build", AgentRequest{Action: ActionBuild, ProjectID: "p1", Prompt: "开始", Plan: &plan}, false},
		{"build rejects malformed plan", AgentRequest{Action: ActionBuild, ProjectID: "p1", Prompt: "开始", Plan: &BuildPlan{}}, true},
		{"repair", AgentRequest{Action: ActionRepair, ProjectID: "p1", Error: "compile failed", Snapshot: snapshot}, false},
		{"unknown", AgentRequest{Action: "unknown", ProjectID: "p1"}, true},
		{"missing project", AgentRequest{Action: ActionPlan, Prompt: "需求"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if gotErr := tt.request.Validate() != nil; gotErr != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", gotErr, tt.wantErr)
			}
		})
	}
}
