package domain

import (
	"errors"
	"fmt"
	"strings"
)

const (
	MaxPromptRunes = 12000
	MaxErrorRunes  = 8000
)

type AgentAction string

type WorkMode string

const (
	ActionPlan    AgentAction = "plan"
	ActionBuild   AgentAction = "build"
	ActionIterate AgentAction = "iterate"
	ActionRepair  AgentAction = "repair"
	ActionRace    AgentAction = "race"
	ActionPolish  AgentAction = "polish" // kept for older clients; treated as an iteration

	ModeEngineer WorkMode = "engineer"
	ModeTeam     WorkMode = "team"
	ModeRace     WorkMode = "race"
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

type RequirementBrief struct {
	Goal        string   `json:"goal"`
	Users       []string `json:"users"`
	CoreFlows   []string `json:"coreFlows"`
	Constraints []string `json:"constraints"`
}

type BuildPlan struct {
	RequirementBrief *RequirementBrief `json:"-"`
	ProductType      string            `json:"-"`
	ProductSummary   string            `json:"productSummary"`
	TargetUsers      []string          `json:"targetUsers"`
	Features         []string          `json:"features"`
	Pages            []PlanPage        `json:"pages"`
	FilePlan         []PlanFile        `json:"filePlan"`
	DesignDirection  string            `json:"designDirection"`
	AcceptanceChecks []string          `json:"acceptanceChecks"`
	Backend          BackendSpec       `json:"-"`
}

type ProjectSnapshot struct {
	Source       string            `json:"source"`
	Title        string            `json:"title"`
	Summary      string            `json:"summary"`
	Files        map[string]string `json:"files"`
	Dependencies map[string]string `json:"dependencies"`
	EntryFile    string            `json:"entryFile"`
	Backend      BackendSpec       `json:"-"`
}

type BuildVerification struct {
	Toolchain  string `json:"toolchain"`
	DurationMS int64  `json:"durationMs"`
	ArtifactID string `json:"artifactId,omitempty"`
	VerifiedAt string `json:"verifiedAt,omitempty"`
}

type SafetyVerification struct {
	Policy     string `json:"policy"`
	VerifiedAt string `json:"verifiedAt"`
}

type AgentRequest struct {
	Action     AgentAction      `json:"action"`
	Mode       WorkMode         `json:"mode,omitempty"`
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
	Candidates   []RaceCandidate  `json:"candidates,omitempty"`
}

type RaceCandidate struct {
	ID           string          `json:"id"`
	Direction    string          `json:"direction"`
	Snapshot     ProjectSnapshot `json:"snapshot"`
	CandidateID  string          `json:"candidateId,omitempty"`
	SnapshotHash string          `json:"snapshotHash,omitempty"`
}

func (r AgentRequest) Validate() error {
	if strings.TrimSpace(r.ProjectID) == "" {
		return errors.New("projectId is required")
	}

	mode := r.Mode
	if mode == "" {
		mode = ModeTeam
	}
	if mode != ModeEngineer && mode != ModeTeam && mode != ModeRace {
		return fmt.Errorf("unsupported mode %q", mode)
	}

	switch r.Action {
	case ActionPlan:
		if !validRequestText(r.Prompt, MaxPromptRunes) {
			return errors.New("plan requires prompt")
		}
	case ActionBuild:
		if !validRequestText(r.Prompt, MaxPromptRunes) || r.Plan == nil {
			return errors.New("build requires prompt and plan")
		}
		if err := ValidateBuildPlan(*r.Plan); err != nil {
			return err
		}
	case ActionRace:
		if !validRequestText(r.Prompt, MaxPromptRunes) || r.Plan == nil {
			return errors.New("race requires prompt and plan")
		}
		if err := ValidateBuildPlan(*r.Plan); err != nil {
			return err
		}
	case ActionIterate, ActionPolish:
		if !validRequestText(r.Prompt, MaxPromptRunes) || r.Snapshot == nil {
			return fmt.Errorf("%s requires prompt and snapshot", r.Action)
		}
	case ActionRepair:
		if !validRequestText(r.Error, MaxErrorRunes) || r.Snapshot == nil {
			return errors.New("repair requires error and snapshot")
		}
	default:
		return fmt.Errorf("unsupported action %q", r.Action)
	}

	return nil
}

func validRequestText(value string, limit int) bool {
	size := len([]rune(strings.TrimSpace(value)))
	return size > 0 && size <= limit
}
