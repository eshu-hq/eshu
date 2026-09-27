// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/content"
	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// secretProofRow is the six-column shape both queries return.
type secretProofRow struct {
	Repo, Path, Language string
	Line                 int
	Text, Kind           string
}

// legacySecretRows runs the frozen pre-#7125 corpus scan for req.
func legacySecretRows(ctx context.Context, db *sql.DB, item secretProofRequest) ([]secretProofRow, error) {
	query, args := legacyHardcodedSecretInvestigationQuery(item.req)
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("legacy query for %q: %w", item.name, err)
	}
	defer func() { _ = rows.Close() }()
	var out []secretProofRow
	for rows.Next() {
		var row secretProofRow
		if err := rows.Scan(&row.Repo, &row.Path, &row.Language, &row.Line, &row.Text, &row.Kind); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// sideTableSecretRows runs the production reader for req.
func sideTableSecretRows(ctx context.Context, reader *ContentReader, item secretProofRequest) ([]secretProofRow, error) {
	rows, err := reader.InvestigateHardcodedSecrets(ctx, item.req)
	if err != nil {
		return nil, fmt.Errorf("side-table reader for %q: %w", item.name, err)
	}
	var out []secretProofRow
	for _, row := range rows {
		out = append(out, secretProofRow{
			Repo: row.RepoID, Path: row.RelativePath, Language: row.Language,
			Line: row.LineNumber, Text: row.LineText, Kind: row.FindingKind,
		})
	}
	return out, nil
}

// secretDifferentialDiffs runs every argument set through both queries and
// returns one line per divergence, plus the total legacy rows compared so a
// caller can prove the comparison was not vacuous.
func secretDifferentialDiffs(
	ctx context.Context, db *sql.DB, reader *ContentReader,
) (diffs []string, total int, err error) {
	for _, item := range secretProofRequests() {
		want, err := legacySecretRows(ctx, db, item)
		if err != nil {
			return nil, 0, err
		}
		got, err := sideTableSecretRows(ctx, reader, item)
		if err != nil {
			diffs = append(diffs, err.Error())
			continue
		}
		total += len(want)
		if slices.Equal(want, got) {
			continue
		}
		detail := fmt.Sprintf("len legacy=%d side=%d", len(want), len(got))
		for i := 0; i < len(want) && i < len(got); i++ {
			if want[i] != got[i] {
				detail += fmt.Sprintf("; first diff at %d: legacy=%+v side=%+v", i, want[i], got[i])
				break
			}
		}
		diffs = append(diffs, fmt.Sprintf("%s: %s", item.name, detail))
	}
	return diffs, total, nil
}

// secretParityCounts compares the side table with the derivation recomputed
// from content_files, EXCEPT ALL in both directions. Both counts must be zero.
func secretParityCounts(ctx context.Context, db *sql.DB) (missing, extra int64, err error) {
	const parity = `
WITH derived AS (
  SELECT f.repo_id, f.relative_path, d.line_number, coalesce(f.language, '') AS language, d.finding_kind, d.line_text
  FROM content_files f CROSS JOIN LATERAL eshu_secret_line_findings(f.content) d
), side AS (
  SELECT repo_id, relative_path, line_number, language, finding_kind, line_text FROM content_file_secret_lines
)
SELECT
  (SELECT count(*) FROM (SELECT * FROM derived EXCEPT ALL SELECT * FROM side) a),
  (SELECT count(*) FROM (SELECT * FROM side EXCEPT ALL SELECT * FROM derived) b)`
	err = db.QueryRowContext(ctx, parity).Scan(&missing, &extra)
	return missing, extra, err
}

// assertSecretState fails the test unless the two queries agree on every
// argument set, the side table equals the derivation, and the comparison saw
// real rows.
func assertSecretState(t *testing.T, ctx context.Context, db *sql.DB, reader *ContentReader, label string) int {
	t.Helper()
	diffs, total, err := secretDifferentialDiffs(ctx, db, reader)
	if err != nil {
		t.Fatalf("%s: differential: %v", label, err)
	}
	if len(diffs) != 0 {
		t.Fatalf("%s: side-table reader diverges from the legacy scan:\n%v", label, diffs)
	}
	if total == 0 {
		t.Fatalf("%s: differential compared zero rows; the proof is vacuous", label)
	}
	missing, extra, err := secretParityCounts(ctx, db)
	if err != nil {
		t.Fatalf("%s: parity: %v", label, err)
	}
	if missing != 0 || extra != 0 {
		t.Fatalf("%s: side table vs derivation EXCEPT ALL = missing %d, extra %d, want 0/0", label, missing, extra)
	}
	t.Logf("%s: %d legacy rows compared identical across %d argument sets", label, total, len(secretProofRequests()))
	return total
}

// secretProofWriter writes files through the real ContentWriter so the
// production upsert, batch delete, and trigger interleaving are exercised.
type secretProofWriter struct {
	t      *testing.T
	ctx    context.Context
	writer storagepostgres.ContentWriter
}

func newSecretProofWriter(t *testing.T, ctx context.Context, db *sql.DB) secretProofWriter {
	return secretProofWriter{t: t, ctx: ctx, writer: storagepostgres.NewContentWriter(storagepostgres.SQLDB{DB: db})}
}

func (w secretProofWriter) write(files []secretProofFile, deleted bool) {
	w.t.Helper()
	byRepo := map[string][]content.Record{}
	for _, file := range files {
		record := content.Record{Path: file.path, Body: file.body, Deleted: deleted}
		if file.language != "" {
			record.Metadata = map[string]string{"language": file.language}
		}
		byRepo[file.repo] = append(byRepo[file.repo], record)
	}
	repos := make([]string, 0, len(byRepo))
	for repo := range byRepo {
		repos = append(repos, repo)
	}
	slices.Sort(repos)
	for _, repo := range repos {
		_, err := w.writer.Write(w.ctx, content.Materialization{
			RepoID: repo, ScopeID: "scope-" + repo, GenerationID: "gen-proof",
			SourceSystem: "git", Records: byRepo[repo],
		})
		if err != nil {
			w.t.Fatalf("write %d records for %s: %v", len(byRepo[repo]), repo, err)
		}
	}
}

// sideRowVersions snapshots each side row's xmin so a test can prove an
// unchanged re-upsert did not rewrite the derived rows.
func sideRowVersions(ctx context.Context, db *sql.DB) (map[string]string, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT repo_id || '|' || relative_path || '|' || line_number, xmin::text FROM content_file_secret_lines`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[string]string{}
	for rows.Next() {
		var key, xmin string
		if err := rows.Scan(&key, &xmin); err != nil {
			return nil, err
		}
		out[key] = xmin
	}
	return out, rows.Err()
}
