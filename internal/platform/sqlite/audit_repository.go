package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"

	"vivatom-api-svc/internal/audit"
)

type auditExecer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func appendAudit(ctx context.Context, executor auditExecer, workspaceID, actorID, action, targetType, targetID, createdAt string, metadata map[string]any) error {
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	_, err = executor.ExecContext(ctx, `INSERT INTO audit_events (id,workspace_id,actor_id,action,target_type,target_id,metadata_json,created_at) VALUES (lower(hex(randomblob(16))),?,?,?,?,?,?,?)`, workspaceID, actorID, action, targetType, targetID, string(encoded), createdAt)
	return err
}

type AuditRepository struct{ db *sql.DB }

func NewAuditRepository(db *sql.DB) *AuditRepository { return &AuditRepository{db: db} }
func (r *AuditRepository) List(ctx context.Context, accountID, workspaceID, before string, limit int) ([]audit.Event, bool, error) {
	var allowed int
	if err := r.db.QueryRowContext(ctx, `SELECT count(*) FROM memberships WHERE account_id=? AND workspace_id=?`, accountID, workspaceID).Scan(&allowed); err != nil {
		return nil, false, err
	}
	if allowed == 0 {
		return nil, false, nil
	}
	query := `SELECT e.id,e.workspace_id,e.actor_id,a.name,a.email,e.action,e.target_type,e.target_id,e.metadata_json,e.created_at FROM audit_events e JOIN tenant_accounts a ON a.id=e.actor_id WHERE e.workspace_id=?`
	args := []any{workspaceID}
	if before != "" {
		query += ` AND e.created_at < ?`
		args = append(args, before)
	}
	query += ` ORDER BY e.created_at DESC,e.id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	events := make([]audit.Event, 0)
	for rows.Next() {
		var event audit.Event
		var metadata string
		if err := rows.Scan(&event.ID, &event.WorkspaceID, &event.ActorID, &event.ActorName, &event.ActorEmail, &event.Action, &event.TargetType, &event.TargetID, &metadata, &event.CreatedAt); err != nil {
			return nil, false, err
		}
		event.Metadata = json.RawMessage(metadata)
		events = append(events, event)
	}
	return events, true, rows.Err()
}
