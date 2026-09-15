package sqlite

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestOpenMigratesApprovedPlanPrompt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	legacy, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = legacy.Exec(`
		CREATE TABLE approved_plans (
			id TEXT PRIMARY KEY, workspace_id TEXT NOT NULL, account_id TEXT NOT NULL,
			project_id TEXT NOT NULL, plan_json TEXT NOT NULL, status TEXT NOT NULL,
			created_at TEXT NOT NULL, approved_at TEXT, consumed_at TEXT
		);
		INSERT INTO approved_plans (id,workspace_id,account_id,project_id,plan_json,status,created_at)
		VALUES ('legacy-plan','workspace','account','project','{}','approved','2026-01-01T00:00:00Z');
		CREATE TABLE build_candidates (
			id TEXT PRIMARY KEY, workspace_id TEXT NOT NULL, account_id TEXT NOT NULL,
			project_id TEXT NOT NULL, usage_id TEXT NOT NULL, snapshot_json TEXT NOT NULL,
			snapshot_hash TEXT NOT NULL, status TEXT NOT NULL, created_at TEXT NOT NULL, committed_at TEXT
		);
		INSERT INTO build_candidates (id,workspace_id,account_id,project_id,usage_id,snapshot_json,snapshot_hash,status,created_at)
		VALUES ('legacy-candidate','workspace','account','project','usage','{}','hash','pending','2026-01-01T00:00:00Z');
	`)
	if err != nil {
		legacy.Close()
		t.Fatal(err)
	}
	if err = legacy.Close(); err != nil {
		t.Fatal(err)
	}

	database, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	var prompt string
	if err = database.QueryRow(`SELECT prompt FROM approved_plans WHERE id='legacy-plan'`).Scan(&prompt); err != nil {
		t.Fatal(err)
	}
	if prompt != "" {
		t.Fatalf("legacy prompt = %q", prompt)
	}
	if err = database.QueryRow(`SELECT prompt FROM build_candidates WHERE id='legacy-candidate'`).Scan(&prompt); err != nil {
		t.Fatal(err)
	}
	if prompt != "" {
		t.Fatalf("legacy candidate prompt = %q", prompt)
	}
}
