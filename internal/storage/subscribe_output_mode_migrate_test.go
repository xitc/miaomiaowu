package storage

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestMigrateLegacyProviderMode(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "migrate.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	// Pre-migration schema (no dual-mode columns)
	_, err = db.Exec(`
CREATE TABLE subscribe_files (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL,
  description TEXT DEFAULT '',
  url TEXT DEFAULT '',
  type TEXT NOT NULL,
  filename TEXT NOT NULL UNIQUE,
  file_short_code TEXT NOT NULL DEFAULT '',
  custom_short_code TEXT NOT NULL DEFAULT '',
  auto_sync_custom_rules INTEGER NOT NULL DEFAULT 0,
  template_filename TEXT NOT NULL DEFAULT '',
  selected_tags TEXT NOT NULL DEFAULT '[]',
  selected_node_ids TEXT NOT NULL DEFAULT '[]',
  selected_custom_rule_ids TEXT NOT NULL DEFAULT '[]',
  selected_override_script_ids TEXT NOT NULL DEFAULT '[]',
  selected_provider_names TEXT NOT NULL DEFAULT '[]',
  expire_at TIMESTAMP,
  raw_output INTEGER NOT NULL DEFAULT 0,
  sort_order INTEGER NOT NULL DEFAULT 0,
  traffic_limit REAL,
  traffic_start_at TIMESTAMP,
  stats_server_ids TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);`)
	if err != nil {
		t.Fatal(err)
	}

	// Seed: provider mode (raw_output+template), normal template, true raw file
	_, err = db.Exec(`INSERT INTO subscribe_files (name, type, filename, template_filename, raw_output) VALUES
		('provider-sub', 'create', 'p.yaml', 'bobo_provider.yaml', 1),
		('normal-sub', 'create', 'n.yaml', 'my-provider-v3.yaml', 0),
		('raw-sub', 'upload', 'r.txt', '', 1)`)
	if err != nil {
		t.Fatal(err)
	}

	repo := &TrafficRepository{db: db}
	if err := repo.ensureSubscribeOutputModeColumns(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// Second call must be idempotent and not re-flip rows
	if err := repo.ensureSubscribeOutputModeColumns(); err != nil {
		t.Fatalf("remigrate: %v", err)
	}

	// Simulate a startup that added the columns but stopped before migrating a legacy row.
	_, err = db.Exec(`INSERT INTO subscribe_files (name, type, filename, template_filename, raw_output) VALUES
		('interrupted-provider-sub', 'create', 'interrupted.yaml', 'bobo_provider.yaml', 1)`)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.ensureSubscribeOutputModeColumns(); err != nil {
		t.Fatalf("recover interrupted migration: %v", err)
	}

	ctx := context.Background()
	files, err := repo.ListSubscribeFiles(ctx)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]SubscribeFile{}
	for _, f := range files {
		byName[f.Name] = f
	}

	p := byName["provider-sub"]
	if !p.ProviderLinkEnabled || p.NormalLinkEnabled || p.RawOutput || p.DefaultOutputMode != OutputModeProvider {
		t.Fatalf("provider-sub migration wrong: %+v", p)
	}
	if p.ProviderTemplateFilename != "bobo_provider.yaml" || p.NormalTemplateFilename != "" {
		t.Fatalf("provider template migration wrong: %+v", p)
	}
	n := byName["normal-sub"]
	if !n.NormalLinkEnabled || n.ProviderLinkEnabled || n.DefaultOutputMode != OutputModeNormal {
		t.Fatalf("normal-sub migration wrong: %+v", n)
	}
	if n.NormalTemplateFilename != "my-provider-v3.yaml" || n.ProviderTemplateFilename != "" {
		t.Fatalf("normal template migration wrong: %+v", n)
	}
	r := byName["raw-sub"]
	if !r.RawOutput || !r.NormalLinkEnabled || r.ProviderLinkEnabled {
		t.Fatalf("raw-sub migration wrong: %+v", r)
	}

	mode, err := p.ResolveOutputMode("")
	if err != nil || mode != OutputModeProvider {
		t.Fatalf("provider default mode: %v %v", mode, err)
	}
	if _, err := p.ResolveOutputMode("normal"); err == nil {
		t.Fatal("provider-only should reject normal")
	}
	interrupted := byName["interrupted-provider-sub"]
	if !interrupted.ProviderLinkEnabled || interrupted.NormalLinkEnabled || interrupted.RawOutput || interrupted.DefaultOutputMode != OutputModeProvider {
		t.Fatalf("interrupted provider migration wrong: %+v", interrupted)
	}
}
