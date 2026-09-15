package sqlite

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

func Open(databasePath string) (*sql.DB, error) {
	if databasePath != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(databasePath), 0o755); err != nil {
			return nil, err
		}
	}
	db, err := sql.Open("sqlite", databasePath)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		db.Close()
		return nil, err
	}
	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func migrate(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS runtime_projects (
			id TEXT PRIMARY KEY,
			title TEXT NOT NULL,
			backend_json TEXT NOT NULL,
			schema_version INTEGER NOT NULL CHECK(schema_version > 0),
			public_key_hash TEXT NOT NULL UNIQUE,
			admin_token_hash TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);
		CREATE TABLE IF NOT EXISTS runtime_users (
			id TEXT PRIMARY KEY,
			project_id TEXT NOT NULL,
			email TEXT NOT NULL,
			password_salt TEXT NOT NULL,
			password_hash TEXT NOT NULL,
			created_at TEXT NOT NULL,
			UNIQUE(project_id, email),
			FOREIGN KEY(project_id) REFERENCES runtime_projects(id) ON DELETE CASCADE
		);
		CREATE TABLE IF NOT EXISTS runtime_sessions (
			token_hash TEXT PRIMARY KEY,
			project_id TEXT NOT NULL,
			user_id TEXT NOT NULL,
			expires_at TEXT NOT NULL,
			created_at TEXT NOT NULL,
			FOREIGN KEY(project_id) REFERENCES runtime_projects(id) ON DELETE CASCADE,
			FOREIGN KEY(user_id) REFERENCES runtime_users(id) ON DELETE CASCADE
		);
		CREATE INDEX IF NOT EXISTS runtime_sessions_lookup
			ON runtime_sessions(project_id, token_hash, expires_at);
		CREATE TABLE IF NOT EXISTS runtime_records (
			id TEXT PRIMARY KEY,
			project_id TEXT NOT NULL,
			collection_name TEXT NOT NULL,
			owner_id TEXT,
			data_json TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			FOREIGN KEY(project_id) REFERENCES runtime_projects(id) ON DELETE CASCADE,
			FOREIGN KEY(owner_id) REFERENCES runtime_users(id) ON DELETE CASCADE
		);
		CREATE INDEX IF NOT EXISTS runtime_records_scope
			ON runtime_records(project_id, collection_name, owner_id, created_at DESC);
		CREATE TABLE IF NOT EXISTS tenant_accounts (
			id TEXT PRIMARY KEY,
			email TEXT NOT NULL UNIQUE,
			name TEXT NOT NULL,
			password_salt TEXT NOT NULL,
			password_hash TEXT NOT NULL,
			created_at TEXT NOT NULL
		);
		CREATE TABLE IF NOT EXISTS workspaces (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			created_at TEXT NOT NULL
		);
		CREATE TABLE IF NOT EXISTS memberships (
			workspace_id TEXT NOT NULL,
			account_id TEXT NOT NULL,
			role TEXT NOT NULL CHECK(role IN ('owner', 'member')),
			created_at TEXT NOT NULL,
			PRIMARY KEY(workspace_id, account_id),
			FOREIGN KEY(workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE,
			FOREIGN KEY(account_id) REFERENCES tenant_accounts(id) ON DELETE CASCADE
		);
		CREATE INDEX IF NOT EXISTS memberships_account
			ON memberships(account_id, workspace_id);
		CREATE TABLE IF NOT EXISTS workspace_invitations (
			id TEXT PRIMARY KEY,
			workspace_id TEXT NOT NULL,
			email TEXT NOT NULL,
			role TEXT NOT NULL CHECK(role IN ('owner','member')),
			token_hash TEXT NOT NULL UNIQUE,
			invited_by TEXT NOT NULL,
			expires_at TEXT NOT NULL,
			accepted_at TEXT,
			created_at TEXT NOT NULL,
			FOREIGN KEY(workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE,
			FOREIGN KEY(invited_by) REFERENCES tenant_accounts(id)
		);
		CREATE INDEX IF NOT EXISTS workspace_invitations_lookup
			ON workspace_invitations(token_hash, expires_at);
		CREATE TABLE IF NOT EXISTS audit_events (
			id TEXT PRIMARY KEY,
			workspace_id TEXT NOT NULL,
			actor_id TEXT NOT NULL,
			action TEXT NOT NULL,
			target_type TEXT NOT NULL,
			target_id TEXT NOT NULL,
			metadata_json TEXT NOT NULL,
			created_at TEXT NOT NULL,
			FOREIGN KEY(workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE,
			FOREIGN KEY(actor_id) REFERENCES tenant_accounts(id)
		);
		CREATE INDEX IF NOT EXISTS audit_events_recent
			ON audit_events(workspace_id, created_at DESC, id DESC);
		CREATE TABLE IF NOT EXISTS agent_usage (
			id TEXT PRIMARY KEY,
			workspace_id TEXT NOT NULL,
			account_id TEXT NOT NULL,
			project_id TEXT NOT NULL,
			action TEXT NOT NULL CHECK(action IN ('plan','build','iterate','repair','polish')),
			credits INTEGER NOT NULL CHECK(credits > 0),
			status TEXT NOT NULL CHECK(status IN ('running','succeeded','failed','cancelled')),
			created_at TEXT NOT NULL,
			completed_at TEXT,
			FOREIGN KEY(workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE,
			FOREIGN KEY(account_id) REFERENCES tenant_accounts(id)
		);
		CREATE INDEX IF NOT EXISTS agent_usage_workspace ON agent_usage(workspace_id, created_at DESC);
		CREATE TABLE IF NOT EXISTS approved_plans (
			id TEXT PRIMARY KEY,
			workspace_id TEXT NOT NULL,
			account_id TEXT NOT NULL,
			project_id TEXT NOT NULL,
			prompt TEXT NOT NULL,
			plan_json TEXT NOT NULL,
			status TEXT NOT NULL CHECK(status IN ('pending','approved','consumed')),
			created_at TEXT NOT NULL,
			approved_at TEXT,
			consumed_at TEXT,
			FOREIGN KEY(workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE,
			FOREIGN KEY(account_id) REFERENCES tenant_accounts(id)
		);
		CREATE INDEX IF NOT EXISTS approved_plans_project ON approved_plans(workspace_id, project_id, created_at DESC);
		CREATE TABLE IF NOT EXISTS build_candidates (
			id TEXT PRIMARY KEY,
			workspace_id TEXT NOT NULL,
			account_id TEXT NOT NULL,
			project_id TEXT NOT NULL,
			usage_id TEXT NOT NULL,
			prompt TEXT NOT NULL,
			snapshot_json TEXT NOT NULL,
			snapshot_hash TEXT NOT NULL,
			status TEXT NOT NULL CHECK(status IN ('pending','committed','rejected')),
			created_at TEXT NOT NULL,
			committed_at TEXT,
			FOREIGN KEY(workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE,
			FOREIGN KEY(account_id) REFERENCES tenant_accounts(id)
		);
		CREATE INDEX IF NOT EXISTS build_candidates_project ON build_candidates(workspace_id, project_id, created_at DESC);
		CREATE TABLE IF NOT EXISTS build_verifications (
			candidate_id TEXT PRIMARY KEY,
			snapshot_hash TEXT NOT NULL,
			toolchain TEXT NOT NULL,
			duration_ms INTEGER NOT NULL CHECK(duration_ms >= 0),
			verified_at TEXT NOT NULL,
			FOREIGN KEY(candidate_id) REFERENCES build_candidates(id) ON DELETE CASCADE
		);
		CREATE TABLE IF NOT EXISTS immutable_versions (
			id TEXT PRIMARY KEY,
			workspace_id TEXT NOT NULL,
			account_id TEXT NOT NULL,
			project_id TEXT NOT NULL,
			candidate_id TEXT NOT NULL UNIQUE,
			parent_version_id TEXT,
			prompt TEXT NOT NULL,
			snapshot_json TEXT NOT NULL,
			snapshot_hash TEXT NOT NULL,
			created_at TEXT NOT NULL,
			FOREIGN KEY(workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE,
			FOREIGN KEY(account_id) REFERENCES tenant_accounts(id),
			FOREIGN KEY(candidate_id) REFERENCES build_candidates(id),
			FOREIGN KEY(parent_version_id) REFERENCES immutable_versions(id)
		);
		CREATE INDEX IF NOT EXISTS immutable_versions_project ON immutable_versions(workspace_id, project_id, created_at);
		CREATE TABLE IF NOT EXISTS platform_sessions (
			token_hash TEXT PRIMARY KEY,
			account_id TEXT NOT NULL,
			expires_at TEXT NOT NULL,
			created_at TEXT NOT NULL,
			FOREIGN KEY(account_id) REFERENCES tenant_accounts(id) ON DELETE CASCADE
		);
		CREATE INDEX IF NOT EXISTS platform_sessions_account_expiry
			ON platform_sessions(account_id, expires_at);
		CREATE TABLE IF NOT EXISTS workspace_projects (
			id TEXT PRIMARY KEY,
			workspace_id TEXT NOT NULL,
			title TEXT NOT NULL,
			status TEXT NOT NULL CHECK(status IN ('draft','planning','awaiting_approval','building','ready','error')),
			active_version_id TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			FOREIGN KEY(workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE
		);
		CREATE INDEX IF NOT EXISTS workspace_projects_recent
			ON workspace_projects(workspace_id, updated_at DESC);
		CREATE TABLE IF NOT EXISTS workspace_project_documents (
			project_id TEXT PRIMARY KEY,
			revision INTEGER NOT NULL CHECK(revision > 0),
			content_hash TEXT NOT NULL,
			payload_json TEXT NOT NULL,
			updated_by TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			FOREIGN KEY(project_id) REFERENCES workspace_projects(id) ON DELETE CASCADE,
			FOREIGN KEY(updated_by) REFERENCES tenant_accounts(id)
		);
	`)
	if err != nil {
		return err
	}
	if err = migrateAgentUsageActions(db); err != nil {
		return err
	}
	if err = migrateApprovedPlanPrompts(db); err != nil {
		return err
	}
	return migrateBuildCandidatePrompts(db)
}

func migrateApprovedPlanPrompts(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(approved_plans)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, dataType string
		var defaultValue any
		if err = rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &primaryKey); err != nil {
			return err
		}
		if name == "prompt" {
			return nil
		}
	}
	if err = rows.Err(); err != nil {
		return err
	}
	if err = rows.Close(); err != nil {
		return err
	}
	_, err = db.Exec(`ALTER TABLE approved_plans ADD COLUMN prompt TEXT NOT NULL DEFAULT ''`)
	return err
}

func migrateBuildCandidatePrompts(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(build_candidates)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, dataType string
		var defaultValue any
		if err = rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &primaryKey); err != nil {
			return err
		}
		if name == "prompt" {
			return nil
		}
	}
	if err = rows.Err(); err != nil {
		return err
	}
	if err = rows.Close(); err != nil {
		return err
	}
	if _, err = db.Exec(`ALTER TABLE build_candidates ADD COLUMN prompt TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	_, err = db.Exec(`UPDATE build_candidates SET prompt=coalesce((SELECT prompt FROM immutable_versions WHERE candidate_id=build_candidates.id),'')`)
	return err
}

func migrateAgentUsageActions(db *sql.DB) error {
	var schema string
	if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='agent_usage'`).Scan(&schema); err != nil {
		return err
	}
	if strings.Contains(schema, "'iterate'") {
		return nil
	}
	_, err := db.Exec(`
		ALTER TABLE agent_usage RENAME TO agent_usage_legacy;
		CREATE TABLE agent_usage (
			id TEXT PRIMARY KEY, workspace_id TEXT NOT NULL, account_id TEXT NOT NULL, project_id TEXT NOT NULL,
			action TEXT NOT NULL CHECK(action IN ('plan','build','iterate','repair','polish')),
			credits INTEGER NOT NULL CHECK(credits > 0), status TEXT NOT NULL CHECK(status IN ('running','succeeded','failed','cancelled')),
			created_at TEXT NOT NULL, completed_at TEXT,
			FOREIGN KEY(workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE,
			FOREIGN KEY(account_id) REFERENCES tenant_accounts(id)
		);
		INSERT INTO agent_usage SELECT * FROM agent_usage_legacy;
		DROP TABLE agent_usage_legacy;
		CREATE INDEX agent_usage_workspace ON agent_usage(workspace_id, created_at DESC);
	`)
	return err
}

func ReadyCheck(db *sql.DB) func() error {
	return func() error {
		return db.PingContext(context.Background())
	}
}
