package catalog

import "vivatom-api-svc/internal/domain"

type Project struct {
	ID              string  `json:"id"`
	WorkspaceID     string  `json:"workspaceId"`
	Title           string  `json:"title"`
	Status          string  `json:"status"`
	ActiveVersionID *string `json:"activeVersionId,omitempty"`
	CreatedAt       string  `json:"createdAt"`
	UpdatedAt       string  `json:"updatedAt"`
}

type SyncInput struct {
	ID              string
	Title           string
	Status          string
	ActiveVersionID *string
}

type DocumentProject struct {
	ID              string            `json:"id"`
	WorkspaceID     string            `json:"workspaceId"`
	Title           string            `json:"title"`
	Status          string            `json:"status"`
	Plan            *domain.BuildPlan `json:"plan,omitempty"`
	ActiveVersionID *string           `json:"activeVersionId,omitempty"`
	CreatedAt       string            `json:"createdAt"`
	UpdatedAt       string            `json:"updatedAt"`
}

type DocumentMessage struct {
	ID        string `json:"id"`
	ProjectID string `json:"projectId"`
	Role      string `json:"role"`
	Content   string `json:"content"`
	EventType string `json:"eventType,omitempty"`
	Agent     string `json:"agent,omitempty"`
	Action    string `json:"action,omitempty"`
	Status    string `json:"status,omitempty"`
	CreatedAt string `json:"createdAt"`
}

type DocumentVersion struct {
	ID              string                 `json:"id"`
	ProjectID       string                 `json:"projectId"`
	ParentVersionID *string                `json:"parentVersionId,omitempty"`
	Prompt          string                 `json:"prompt"`
	Snapshot        domain.ProjectSnapshot `json:"snapshot"`
	CreatedAt       string                 `json:"createdAt"`
	CandidateID     string                 `json:"candidateId,omitempty"`
	SnapshotHash    string                 `json:"snapshotHash,omitempty"`
}

type DocumentPayload struct {
	Project  DocumentProject   `json:"project"`
	Messages []DocumentMessage `json:"messages"`
	Versions []DocumentVersion `json:"versions"`
}

type Document struct {
	ProjectID   string          `json:"projectId"`
	Revision    int             `json:"revision"`
	ContentHash string          `json:"contentHash"`
	Payload     DocumentPayload `json:"payload"`
	UpdatedAt   string          `json:"updatedAt"`
}

type StoredDocument struct {
	ProjectID   string
	Revision    int
	ContentHash string
	PayloadJSON string
	UpdatedAt   string
}

type SaveDocumentResult string

const (
	DocumentSaved     SaveDocumentResult = "saved"
	DocumentConflict  SaveDocumentResult = "conflict"
	DocumentForbidden SaveDocumentResult = "forbidden"
)

type Error struct {
	Code   string
	Status int
}

func (e *Error) Error() string { return e.Code }
