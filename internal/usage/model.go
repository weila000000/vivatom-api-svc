package usage

import "vivatom-api-svc/internal/domain"

type Summary struct {
	WorkspaceID string `json:"workspaceId"`
	Limit       int    `json:"limit"`
	Used        int    `json:"used"`
	Remaining   int    `json:"remaining"`
}

type Version struct {
	ID              string                    `json:"id"`
	ProjectID       string                    `json:"projectId"`
	ParentVersionID string                    `json:"parentVersionId,omitempty"`
	Prompt          string                    `json:"prompt"`
	Snapshot        domain.ProjectSnapshot    `json:"snapshot"`
	CandidateID     string                    `json:"candidateId"`
	SnapshotHash    string                    `json:"snapshotHash"`
	CreatedAt       string                    `json:"createdAt"`
	Build           *domain.BuildVerification `json:"build,omitempty"`
}

type Candidate struct {
	ID           string                 `json:"candidateId"`
	SnapshotHash string                 `json:"snapshotHash"`
	Prompt       string                 `json:"prompt"`
	Snapshot     domain.ProjectSnapshot `json:"snapshot"`
}

type Result string

const (
	ResultOK               Result = "ok"
	ResultForbidden        Result = "forbidden"
	ResultExhausted        Result = "exhausted"
	ResultApprovalInvalid  Result = "approval_invalid"
	ResultCandidateInvalid Result = "candidate_invalid"
	ResultVersionConflict  Result = "version_conflict"
)

type Error struct {
	Code   string
	Status int
}

func (e *Error) Error() string { return e.Code }
