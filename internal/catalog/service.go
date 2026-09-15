package catalog

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"vivatom-api-svc/internal/generation"
	"vivatom-api-svc/internal/identity"
)

type Identity interface {
	Me(context.Context, string) (identity.Account, []identity.Workspace, error)
}

type Repository interface {
	UpsertForMember(context.Context, string, Project) (*Project, error)
	ListForMember(context.Context, string, string) ([]Project, error)
	SaveDocumentForMember(context.Context, string, string, StoredDocument, int) (*StoredDocument, SaveDocumentResult, error)
	GetDocumentForMember(context.Context, string, string, string) (*StoredDocument, bool, error)
	RecordConflictResolution(context.Context, string, string, string, string, string) (bool, error)
}

func (s *Service) ResolveConflict(ctx context.Context, token, workspaceID, projectID, choice string) error {
	account, err := s.account(ctx, token)
	if err != nil {
		return err
	}
	if !validID(workspaceID) || !validID(projectID) || (choice != "local" && choice != "cloud") {
		return apiError("invalid_conflict_resolution", http.StatusBadRequest)
	}
	allowed, err := s.repository.RecordConflictResolution(ctx, account.ID, workspaceID, projectID, choice, s.now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return apiError("catalog_unavailable", http.StatusServiceUnavailable)
	}
	if !allowed {
		return apiError("workspace_forbidden", http.StatusForbidden)
	}
	return nil
}

func (s *Service) SaveDocument(ctx context.Context, token, workspaceID, projectID string, expectedRevision int, payload DocumentPayload) (Document, error) {
	account, err := s.account(ctx, token)
	if err != nil {
		return Document{}, err
	}
	if expectedRevision < 0 || !validID(workspaceID) || !validID(projectID) {
		return Document{}, apiError("invalid_document", http.StatusBadRequest)
	}
	if err := validateDocument(workspaceID, projectID, payload); err != nil {
		return Document{}, apiError("invalid_document", http.StatusBadRequest)
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return Document{}, apiError("invalid_document", http.StatusBadRequest)
	}
	if len(payloadJSON) > 2*1024*1024 {
		return Document{}, apiError("document_too_large", http.StatusRequestEntityTooLarge)
	}
	sum := sha256.Sum256(payloadJSON)
	now := s.now().UTC().Format(time.RFC3339Nano)
	stored, result, err := s.repository.SaveDocumentForMember(ctx, account.ID, workspaceID, StoredDocument{ProjectID: projectID, ContentHash: hex.EncodeToString(sum[:]), PayloadJSON: string(payloadJSON), UpdatedAt: now}, expectedRevision)
	if err != nil {
		return Document{}, apiError("catalog_unavailable", http.StatusServiceUnavailable)
	}
	switch result {
	case DocumentConflict:
		return Document{}, apiError("document_conflict", http.StatusConflict)
	case DocumentForbidden:
		return Document{}, apiError("workspace_forbidden", http.StatusForbidden)
	}
	return decodeDocument(stored)
}

func (s *Service) GetDocument(ctx context.Context, token, workspaceID, projectID string) (Document, error) {
	account, err := s.account(ctx, token)
	if err != nil {
		return Document{}, err
	}
	stored, allowed, err := s.repository.GetDocumentForMember(ctx, account.ID, workspaceID, projectID)
	if err != nil {
		return Document{}, apiError("catalog_unavailable", http.StatusServiceUnavailable)
	}
	if !allowed {
		return Document{}, apiError("workspace_forbidden", http.StatusForbidden)
	}
	if stored == nil {
		return Document{}, apiError("document_not_found", http.StatusNotFound)
	}
	document, err := decodeDocument(stored)
	if err != nil {
		return Document{}, apiError("catalog_unavailable", http.StatusServiceUnavailable)
	}
	payloadJSON, _ := json.Marshal(document.Payload)
	sum := sha256.Sum256(payloadJSON)
	if hex.EncodeToString(sum[:]) != document.ContentHash {
		return Document{}, apiError("catalog_unavailable", http.StatusServiceUnavailable)
	}
	return document, nil
}

func validateDocument(workspaceID, projectID string, payload DocumentPayload) error {
	if payload.Project.ID != projectID || payload.Project.WorkspaceID != workspaceID || !validStatus(payload.Project.Status) || strings.TrimSpace(payload.Project.Title) == "" || len([]rune(payload.Project.Title)) > 120 || len(payload.Messages) > 500 || len(payload.Versions) > 100 {
		return apiError("invalid_document", http.StatusBadRequest)
	}
	versions := make(map[string]struct{}, len(payload.Versions))
	guard := generation.NewGuard()
	for _, version := range payload.Versions {
		if version.ProjectID != projectID || !validID(version.ID) || len([]rune(version.Prompt)) > 20000 {
			return apiError("invalid_document", http.StatusBadRequest)
		}
		if _, err := guard.Check(version.Snapshot); err != nil {
			return err
		}
		versions[version.ID] = struct{}{}
	}
	for _, message := range payload.Messages {
		if message.ProjectID != projectID || !validID(message.ID) || len([]rune(message.Content)) > 20000 || !validMessageRole(message.Role) {
			return apiError("invalid_document", http.StatusBadRequest)
		}
	}
	for _, version := range payload.Versions {
		if version.ParentVersionID != nil {
			if _, ok := versions[*version.ParentVersionID]; !ok {
				return apiError("invalid_document", http.StatusBadRequest)
			}
		}
	}
	if payload.Project.ActiveVersionID != nil {
		if _, ok := versions[*payload.Project.ActiveVersionID]; !ok {
			return apiError("invalid_document", http.StatusBadRequest)
		}
	}
	return nil
}

func validMessageRole(role string) bool {
	return role == "user" || role == "agent" || role == "system"
}

func decodeDocument(stored *StoredDocument) (Document, error) {
	if stored == nil {
		return Document{}, apiError("document_not_found", http.StatusNotFound)
	}
	var payload DocumentPayload
	if err := json.Unmarshal([]byte(stored.PayloadJSON), &payload); err != nil {
		return Document{}, err
	}
	return Document{ProjectID: stored.ProjectID, Revision: stored.Revision, ContentHash: stored.ContentHash, Payload: payload, UpdatedAt: stored.UpdatedAt}, nil
}

type Service struct {
	identity   Identity
	repository Repository
	now        func() time.Time
}

func NewService(identityService Identity, repository Repository) *Service {
	return &Service{identity: identityService, repository: repository, now: time.Now}
}

func (s *Service) Sync(ctx context.Context, token, workspaceID string, input SyncInput) (Project, error) {
	account, err := s.account(ctx, token)
	if err != nil {
		return Project{}, err
	}
	if !validID(workspaceID) || !validID(input.ID) || strings.TrimSpace(input.Title) == "" || len([]rune(input.Title)) > 120 || !validStatus(input.Status) {
		return Project{}, apiError("invalid_project", http.StatusBadRequest)
	}
	if input.ActiveVersionID != nil && !validID(*input.ActiveVersionID) {
		return Project{}, apiError("invalid_project", http.StatusBadRequest)
	}
	now := s.now().UTC().Format(time.RFC3339Nano)
	project := Project{ID: input.ID, WorkspaceID: workspaceID, Title: strings.TrimSpace(input.Title), Status: input.Status, ActiveVersionID: input.ActiveVersionID, CreatedAt: now, UpdatedAt: now}
	stored, err := s.repository.UpsertForMember(ctx, account.ID, project)
	if err != nil {
		return Project{}, apiError("catalog_unavailable", http.StatusServiceUnavailable)
	}
	if stored == nil {
		return Project{}, apiError("workspace_forbidden", http.StatusForbidden)
	}
	return *stored, nil
}

func (s *Service) List(ctx context.Context, token, workspaceID string) ([]Project, error) {
	account, err := s.account(ctx, token)
	if err != nil {
		return nil, err
	}
	if !validID(workspaceID) {
		return nil, apiError("workspace_forbidden", http.StatusForbidden)
	}
	projects, err := s.repository.ListForMember(ctx, account.ID, workspaceID)
	if err != nil {
		return nil, apiError("catalog_unavailable", http.StatusServiceUnavailable)
	}
	if projects == nil {
		return nil, apiError("workspace_forbidden", http.StatusForbidden)
	}
	return projects, nil
}

func (s *Service) account(ctx context.Context, token string) (identity.Account, error) {
	if s.identity == nil {
		return identity.Account{}, apiError("catalog_unavailable", http.StatusServiceUnavailable)
	}
	account, _, err := s.identity.Me(ctx, token)
	if err != nil {
		return identity.Account{}, apiError("unauthorized", http.StatusUnauthorized)
	}
	return account, nil
}

func validID(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && len(value) <= 100
}

func validStatus(value string) bool {
	switch value {
	case "draft", "planning", "awaiting_approval", "building", "ready", "error":
		return true
	default:
		return false
	}
}

func apiError(code string, status int) error { return &Error{Code: code, Status: status} }
