package usage

import "vivatom-api-svc/internal/domain"

type Summary struct {
	WorkspaceID string `json:"workspaceId"`
	Limit       int    `json:"limit"`
	Used        int    `json:"used"`
	Remaining   int    `json:"remaining"`
}

type Version struct {
	ID              string                     `json:"id"`
	ProjectID       string                     `json:"projectId"`
	ParentVersionID string                     `json:"parentVersionId,omitempty"`
	Prompt          string                     `json:"prompt"`
	Snapshot        domain.ProjectSnapshot     `json:"snapshot"`
	CandidateID     string                     `json:"candidateId"`
	SnapshotHash    string                     `json:"snapshotHash"`
	SourceAction    string                     `json:"sourceAction"`
	ApprovalID      string                     `json:"approvalId,omitempty"`
	CreatedAt       string                     `json:"createdAt"`
	Build           *domain.BuildVerification  `json:"build,omitempty"`
	Safety          *domain.SafetyVerification `json:"safety,omitempty"`
}

type Candidate struct {
	ID           string                 `json:"candidateId"`
	SnapshotHash string                 `json:"snapshotHash"`
	Prompt       string                 `json:"prompt"`
	Snapshot     domain.ProjectSnapshot `json:"snapshot"`
}

type Result string

const (
	ResultOK                Result = "ok"
	ResultForbidden         Result = "forbidden"
	ResultExhausted         Result = "exhausted"
	ResultApprovalInvalid   Result = "approval_invalid"
	ResultCandidateInvalid  Result = "candidate_invalid"
	ResultCompileInProgress Result = "compile_in_progress"
	ResultVersionConflict   Result = "version_conflict"
	ResultSafetyRejected    Result = "safety_rejected"
)

type Error struct {
	Code    string
	Status  int
	Message string
}

func (e *Error) Error() string { return e.Code }
