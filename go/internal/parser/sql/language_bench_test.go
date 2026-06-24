// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sql

import (
	"os"
	"path/filepath"
	"testing"

	tree_sitter_sql "github.com/alexaandru/go-sitter-forest/sql"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// newSQLBenchParser builds a parser for benchmarks, mirroring newSQLTestParser
// but bound to *testing.B.
func newSQLBenchParser(b *testing.B) *tree_sitter.Parser {
	b.Helper()

	parser := tree_sitter.NewParser()
	if err := parser.SetLanguage(tree_sitter.NewLanguage(tree_sitter_sql.GetLanguage())); err != nil {
		b.Fatalf("SetLanguage() error = %v", err)
	}
	return parser
}

// benchSQLDocument is a representative multi-statement SQL file exercising every
// extracted construct (table, column, view, materialized view, function,
// procedure, trigger, index, alter table, and DML reference scanning). It is the
// shared input for the regex-vs-AST parse benchmark so the before/after numbers
// in the package README measure the same work.
const benchSQLDocument = `CREATE TABLE public.users (
  id BIGSERIAL NOT NULL,
  org_id UUID NOT NULL,
  email TEXT NOT NULL,
  created_at TIMESTAMPTZ DEFAULT now(),
  PRIMARY KEY (id),
  FOREIGN KEY (org_id) REFERENCES public.orgs(id),
  CONSTRAINT users_email_unique UNIQUE (email)
);

CREATE TABLE public.orgs (
  id UUID NOT NULL,
  name VARCHAR(255) NOT NULL,
  PRIMARY KEY (id)
);

CREATE INDEX users_org_idx ON public.users (org_id);

CREATE VIEW public.active_users AS
  SELECT u.id, u.email FROM public.users u JOIN public.orgs o ON o.id = u.org_id;

CREATE MATERIALIZED VIEW public.user_counts AS
  SELECT org_id, count(*) FROM public.users GROUP BY org_id;

CREATE OR REPLACE FUNCTION public.sync_user_segment() RETURNS trigger AS $$
BEGIN
  UPDATE public.users u SET segment = s.segment
  FROM public.segments s WHERE s.user_id = NEW.id;
  INSERT INTO public.audit_log (user_id) VALUES (NEW.id);
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE PROCEDURE public.purge_stale(days INT)
LANGUAGE plpgsql AS $$
BEGIN
  DELETE FROM public.users WHERE last_seen < now() - days;
END;
$$;

CREATE TRIGGER users_sync_segment
AFTER UPDATE ON public.users
FOR EACH ROW EXECUTE PROCEDURE public.sync_user_segment();

ALTER TABLE public.users ADD COLUMN last_seen TIMESTAMPTZ;
`

// BenchmarkParseComprehensive measures one full SQL parse over a representative
// multi-construct document. It is the tracked baseline for the regex-to-AST
// migration recorded in README.md (Performance Evidence / No-Regression
// Evidence). Run with:
//
//	go test ./internal/parser/sql -run '^$' -bench BenchmarkParseComprehensive -benchmem
func BenchmarkParseComprehensive(b *testing.B) {
	dir := b.TempDir()
	path := filepath.Join(dir, "schema.sql")
	if err := os.WriteFile(path, []byte(benchSQLDocument), 0o644); err != nil {
		b.Fatalf("WriteFile() error = %v", err)
	}
	parser := newSQLBenchParser(b)
	defer parser.Close()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Parse(path, false, Options{}, parser); err != nil {
			b.Fatalf("Parse() error = %v", err)
		}
	}
}
