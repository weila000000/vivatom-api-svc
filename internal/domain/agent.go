package domain

import (
	"errors"
	"fmt"
	"strings"
)

type AgentAction string

const (
	ActionPlan    AgentAction = "plan"
	ActionBuild   AgentAction = "build"
	ActionIterate AgentAction = "iterate"
	ActionRepair  AgentAction = "repair"
	ActionPolish  AgentAction = "polish"
)

type BackendField struct {
	Name     string `json:"name"`
	Label    string `json:"label"`
	Type     string `json:"type"`
	Required bool   `json:"required"`
}

type BackendCollection struct {
	Name   string         `json:"name"`
	Label  string         `json:"label"`
	Access string         `json:"access"`
	Fields []BackendField `json:"fields"`
}

type BackendSpec struct {
	Enabled     bool                `json:"enabled"`
	Auth        string              `json:"auth"`
	Collections []BackendCollection `json:"collections"`
}

type PlanPage struct {
	Name    string `json:"name"`
	Purpose string `json:"purpose"`
}

type PlanFile struct {
	Path           string `json:"path"`
	Responsibility string `json:"responsibility"`
}

type BuildPlan struct {
	ProductType      string      `json:"productType"`
	ProductSummary   string      `json:"productSummary"`
	TargetUsers      []string    `json:"targetUsers"`
	Features         []string    `json:"features"`
	Pages            []PlanPage  `json:"pages"`
	FilePlan         []PlanFile  `json:"filePlan"`
	DesignDirection  string      `json:"designDirection"`
	AcceptanceChecks []string    `json:"acceptanceChecks"`
	Backend          BackendSpec `json:"backend"`
}

type ProjectSnapshot struct {
	Source       string            `json:"source"`
	Title        string            `json:"title"`
	Summary      string            `json:"summary"`
	Files        map[string]string `json:"files"`
	Dependencies map[string]string `json:"dependencies"`
	EntryFile    string            `json:"entryFile"`
	Backend      BackendSpec       `json:"backend"`
}

type AgentRequest struct {
	Action     AgentAction      `json:"action"`
	ProjectID  string           `json:"projectId"`
	ApprovalID string           `json:"approvalId,omitempty"`
	Prompt     string           `json:"prompt,omitempty"`
	Plan       *BuildPlan       `json:"plan,omitempty"`
	Snapshot   *ProjectSnapshot `json:"snapshot,omitempty"`
	Error      string           `json:"error,omitempty"`
}

type AgentEvent struct {
	Type         string           `json:"type"`
	ID           string           `json:"id,omitempty"`
	Agent        string           `json:"agent,omitempty"`
	Action       string           `json:"action,omitempty"`
	Status       string           `json:"status,omitempty"`
	Label        string           `json:"label,omitempty"`
	Text         string           `json:"text,omitempty"`
	Message      string           `json:"message,omitempty"`
	Code         string           `json:"code,omitempty"`
	Retryable    bool             `json:"retryable,omitempty"`
	ApprovalID   string           `json:"approvalId,omitempty"`
	CandidateID  string           `json:"candidateId,omitempty"`
	SnapshotHash string           `json:"snapshotHash,omitempty"`
	Plan         *BuildPlan       `json:"plan,omitempty"`
	Snapshot     *ProjectSnapshot `json:"snapshot,omitempty"`
}

func (r AgentRequest) Validate() error {
	if strings.TrimSpace(r.ProjectID) == "" {
		return errors.New("projectId is required")
	}

	switch r.Action {
	case ActionPlan:
		if strings.TrimSpace(r.Prompt) == "" {
			return errors.New("plan requires prompt")
		}
	case ActionBuild:
		if strings.TrimSpace(r.Prompt) == "" || r.Plan == nil {
			return errors.New("build requires prompt and plan")
		}
		if err := ValidateBuildPlan(*r.Plan); err != nil {
			return err
		}
	case ActionIterate, ActionPolish:
		if strings.TrimSpace(r.Prompt) == "" || r.Snapshot == nil {
			return fmt.Errorf("%s requires prompt and snapshot", r.Action)
		}
	case ActionRepair:
		if strings.TrimSpace(r.Error) == "" || r.Snapshot == nil {
			return errors.New("repair requires error and snapshot")
		}
	default:
		return fmt.Errorf("unsupported action %q", r.Action)
	}

	return nil
}
