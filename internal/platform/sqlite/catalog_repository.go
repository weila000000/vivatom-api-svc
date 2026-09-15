package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"reflect"

	"vivatom-api-svc/internal/catalog"
	"vivatom-api-svc/internal/domain"
)

type CatalogRepository struct{ db *sql.DB }

func NewCatalogRepository(db *sql.DB) *CatalogRepository { return &CatalogRepository{db: db} }

func (r *CatalogRepository) VersionsMatchForMember(ctx context.Context, accountID, workspaceID, projectID string, versions []catalog.DocumentVersion) (bool, error) {
	if len(versions) == 0 {
		return true, nil
	}
	var member int
	if err := r.db.QueryRowContext(ctx, `SELECT count(*) FROM memberships WHERE account_id=? AND workspace_id=?`, accountID, workspaceID).Scan(&member); err != nil {
		return false, err
	}
	if member == 0 {
		return false, nil
	}
	for _, candidate := range versions {
		var parentID, prompt, snapshotJSON, candidateID, snapshotHash, createdAt, toolchain, verifiedAt string
		var durationMS int64
		err := r.db.QueryRowContext(ctx, `
			SELECT coalesce(v.parent_version_id,''),v.prompt,v.snapshot_json,v.candidate_id,v.snapshot_hash,v.created_at,b.toolchain,b.duration_ms,b.verified_at
			FROM immutable_versions v JOIN build_verifications b ON b.candidate_id=v.candidate_id AND b.snapshot_hash=v.snapshot_hash
			WHERE v.id=? AND v.workspace_id=? AND v.project_id=?`, candidate.ID, workspaceID, projectID).
			Scan(&parentID, &prompt, &snapshotJSON, &candidateID, &snapshotHash, &createdAt, &toolchain, &durationMS, &verifiedAt)
		if err == sql.ErrNoRows {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		var snapshot domain.ProjectSnapshot
		if err = json.Unmarshal([]byte(snapshotJSON), &snapshot); err != nil {
			return false, err
		}
		candidateParent := ""
		if candidate.ParentVersionID != nil {
			candidateParent = *candidate.ParentVersionID
		}
		if candidate.Build == nil || parentID != candidateParent || prompt != candidate.Prompt || candidateID != candidate.CandidateID || snapshotHash != candidate.SnapshotHash || createdAt != candidate.CreatedAt || toolchain != candidate.Build.Toolchain || durationMS != candidate.Build.DurationMS || verifiedAt != candidate.Build.VerifiedAt || !reflect.DeepEqual(snapshot, candidate.Snapshot) {
			return false, nil
		}
	}
	return true, nil
}

func (r *CatalogRepository) UpsertForMember(ctx context.Context, accountID string, project catalog.Project) (*catalog.Project, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var member int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM memberships WHERE account_id=? AND workspace_id=?`, accountID, project.WorkspaceID).Scan(&member); err != nil {
		return nil, err
	}
	if member == 0 {
		return nil, nil
	}
	var previous catalog.Project
	var previousActive sql.NullString
	existing := true
	if err = tx.QueryRowContext(ctx, `SELECT id,workspace_id,title,status,active_version_id,created_at,updated_at FROM workspace_projects WHERE id=? AND workspace_id=?`, project.ID, project.WorkspaceID).Scan(&previous.ID, &previous.WorkspaceID, &previous.Title, &previous.Status, &previousActive, &previous.CreatedAt, &previous.UpdatedAt); err == sql.ErrNoRows {
		existing = false
	} else if err != nil {
		return nil, err
	}
	if previousActive.Valid {
		previous.ActiveVersionID = &previousActive.String
	}
	row := tx.QueryRowContext(ctx, `
		INSERT INTO workspace_projects (id,workspace_id,title,status,active_version_id,created_at,updated_at)
		SELECT ?,?,?,?,?,?,?
		WHERE EXISTS (SELECT 1 FROM memberships WHERE account_id=? AND workspace_id=?)
		ON CONFLICT(id) DO UPDATE SET
			title=excluded.title,
			status=excluded.status,
			active_version_id=excluded.active_version_id,
			updated_at=excluded.updated_at
		WHERE workspace_projects.workspace_id=excluded.workspace_id
		RETURNING id,workspace_id,title,status,active_version_id,created_at,updated_at`,
		project.ID, project.WorkspaceID, project.Title, project.Status, project.ActiveVersionID,
		project.CreatedAt, project.UpdatedAt, accountID, project.WorkspaceID)
	stored, err := scanCatalogProject(row)
	if err != nil || stored == nil {
		return stored, err
	}
	changed := !existing || previous.Title != stored.Title || previous.Status != stored.Status || stringPointerValue(previous.ActiveVersionID) != stringPointerValue(stored.ActiveVersionID)
	if changed {
		action := "project.metadata_updated"
		if !existing {
			action = "project.created"
		}
		if err = appendAudit(ctx, tx, project.WorkspaceID, accountID, action, "project", project.ID, project.UpdatedAt, map[string]any{"title": stored.Title, "status": stored.Status, "activeVersionId": stored.ActiveVersionID}); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return stored, nil
}

func (r *CatalogRepository) ListForMember(ctx context.Context, accountID, workspaceID string) ([]catalog.Project, error) {
	var member int
	if err := r.db.QueryRowContext(ctx, `SELECT count(*) FROM memberships WHERE account_id=? AND workspace_id=?`, accountID, workspaceID).Scan(&member); err != nil {
		return nil, err
	}
	if member == 0 {
		return nil, nil
	}
	rows, err := r.db.QueryContext(ctx, `SELECT id,workspace_id,title,status,active_version_id,created_at,updated_at FROM workspace_projects WHERE workspace_id=? ORDER BY updated_at DESC LIMIT 100`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	projects := make([]catalog.Project, 0)
	for rows.Next() {
		project, err := scanCatalogProject(rows)
		if err != nil {
			return nil, err
		}
		projects = append(projects, *project)
	}
	return projects, rows.Err()
}

func (r *CatalogRepository) SaveDocumentForMember(ctx context.Context, accountID, workspaceID string, document catalog.StoredDocument, expectedRevision int) (*catalog.StoredDocument, catalog.SaveDocumentResult, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, "", err
	}
	if err = appendAudit(ctx, tx, workspaceID, accountID, "project.document_saved", "project", document.ProjectID, document.UpdatedAt, map[string]any{"revision": document.Revision, "contentHash": document.ContentHash}); err != nil {
		return nil, "", err
	}
	defer tx.Rollback()
	var ownerWorkspace string
	if err := tx.QueryRowContext(ctx, `SELECT p.workspace_id FROM workspace_projects p JOIN memberships m ON m.workspace_id=p.workspace_id WHERE p.id=? AND p.workspace_id=? AND m.account_id=?`, document.ProjectID, workspaceID, accountID).Scan(&ownerWorkspace); err != nil {
		if err == sql.ErrNoRows {
			return nil, catalog.DocumentForbidden, nil
		}
		return nil, "", err
	}
	current, err := scanStoredDocument(tx.QueryRowContext(ctx, `SELECT project_id,revision,content_hash,payload_json,updated_at FROM workspace_project_documents WHERE project_id=?`, document.ProjectID))
	if err != nil {
		return nil, "", err
	}
	if current != nil && current.ContentHash == document.ContentHash {
		return current, catalog.DocumentSaved, nil
	}
	currentRevision := 0
	if current != nil {
		currentRevision = current.Revision
	}
	if currentRevision != expectedRevision {
		return current, catalog.DocumentConflict, nil
	}
	document.Revision = currentRevision + 1
	if current == nil {
		_, err = tx.ExecContext(ctx, `INSERT INTO workspace_project_documents (project_id,revision,content_hash,payload_json,updated_by,updated_at) VALUES (?,?,?,?,?,?)`, document.ProjectID, document.Revision, document.ContentHash, document.PayloadJSON, accountID, document.UpdatedAt)
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE workspace_project_documents SET revision=?,content_hash=?,payload_json=?,updated_by=?,updated_at=? WHERE project_id=? AND revision=?`, document.Revision, document.ContentHash, document.PayloadJSON, accountID, document.UpdatedAt, document.ProjectID, expectedRevision)
	}
	if err != nil {
		return nil, "", err
	}
	if err := tx.Commit(); err != nil {
		return nil, "", err
	}
	return &document, catalog.DocumentSaved, nil
}

func (r *CatalogRepository) RecordConflictResolution(ctx context.Context, accountID, workspaceID, projectID, choice, createdAt string) (bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var allowed int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM workspace_projects p JOIN memberships m ON m.workspace_id=p.workspace_id WHERE p.id=? AND p.workspace_id=? AND m.account_id=?`, projectID, workspaceID, accountID).Scan(&allowed); err != nil {
		return false, err
	}
	if allowed == 0 {
		return false, nil
	}
	if err = appendAudit(ctx, tx, workspaceID, accountID, "project.conflict_resolved", "project", projectID, createdAt, map[string]any{"choice": choice}); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

func stringPointerValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func (r *CatalogRepository) GetDocumentForMember(ctx context.Context, accountID, workspaceID, projectID string) (*catalog.StoredDocument, bool, error) {
	var member int
	if err := r.db.QueryRowContext(ctx, `SELECT count(*) FROM workspace_projects p JOIN memberships m ON m.workspace_id=p.workspace_id WHERE p.id=? AND p.workspace_id=? AND m.account_id=?`, projectID, workspaceID, accountID).Scan(&member); err != nil {
		return nil, false, err
	}
	if member == 0 {
		return nil, false, nil
	}
	document, err := scanStoredDocument(r.db.QueryRowContext(ctx, `SELECT project_id,revision,content_hash,payload_json,updated_at FROM workspace_project_documents WHERE project_id=?`, projectID))
	return document, true, err
}

func scanStoredDocument(scanner catalogScanner) (*catalog.StoredDocument, error) {
	var document catalog.StoredDocument
	if err := scanner.Scan(&document.ProjectID, &document.Revision, &document.ContentHash, &document.PayloadJSON, &document.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &document, nil
}

type catalogScanner interface{ Scan(...any) error }

func scanCatalogProject(scanner catalogScanner) (*catalog.Project, error) {
	var project catalog.Project
	var activeVersionID sql.NullString
	if err := scanner.Scan(&project.ID, &project.WorkspaceID, &project.Title, &project.Status, &activeVersionID, &project.CreatedAt, &project.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	if activeVersionID.Valid {
		project.ActiveVersionID = &activeVersionID.String
	}
	return &project, nil
}
