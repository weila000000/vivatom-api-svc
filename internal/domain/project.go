package domain

import "fmt"

type ProjectStatus string

const (
	ProjectDraft            ProjectStatus = "draft"
	ProjectPlanning         ProjectStatus = "planning"
	ProjectAwaitingApproval ProjectStatus = "awaiting_approval"
	ProjectBuilding         ProjectStatus = "building"
	ProjectReady            ProjectStatus = "ready"
	ProjectError            ProjectStatus = "error"
)

type ProjectCommand string

const (
	CommandStartPlan      ProjectCommand = "start_plan"
	CommandPlanReady      ProjectCommand = "plan_ready"
	CommandRevisePlan     ProjectCommand = "revise_plan"
	CommandApprove        ProjectCommand = "approve"
	CommandBuildSucceeded ProjectCommand = "build_succeeded"
	CommandStartIteration ProjectCommand = "start_iteration"
	CommandFail           ProjectCommand = "fail"
	CommandRetryPlan      ProjectCommand = "retry_plan"
	CommandRetryBuild     ProjectCommand = "retry_build"
)

var projectTransitions = map[ProjectStatus]map[ProjectCommand]ProjectStatus{
	ProjectDraft: {
		CommandStartPlan: ProjectPlanning,
	},
	ProjectPlanning: {
		CommandPlanReady: ProjectAwaitingApproval,
		CommandFail:      ProjectError,
	},
	ProjectAwaitingApproval: {
		CommandRevisePlan: ProjectPlanning,
		CommandApprove:    ProjectBuilding,
		CommandFail:       ProjectError,
	},
	ProjectBuilding: {
		CommandBuildSucceeded: ProjectReady,
		CommandFail:           ProjectError,
	},
	ProjectReady: {
		CommandStartIteration: ProjectBuilding,
	},
	ProjectError: {
		CommandRetryPlan:  ProjectPlanning,
		CommandRetryBuild: ProjectBuilding,
	},
}

func TransitionProject(status ProjectStatus, command ProjectCommand) (ProjectStatus, error) {
	commands, exists := projectTransitions[status]
	if !exists {
		return status, fmt.Errorf("unknown project status %q", status)
	}
	next, allowed := commands[command]
	if !allowed {
		return status, fmt.Errorf("command %q is not allowed while project is %q", command, status)
	}
	return next, nil
}
