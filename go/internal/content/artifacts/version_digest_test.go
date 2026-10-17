// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// guardSnapshotFiles are the committed guard corpus, in digest order. They are
// written only by TestRegenerateExtractorCorpus (version_snapshot_test.go).
var guardSnapshotFiles = []string{
	filepath.Join("testdata", "guard", "fixtures.jsonl"),
	filepath.Join("testdata", "guard", "generated.jsonl"),
}

// guardRepoID is the repository id the guard store answers for. The id never
// enters a row.
const guardRepoID = "extractor-guard"

// guardRepository is one repository of the guard corpus: one JSONL line.
type guardRepository struct {
	Label string      `json:"label"`
	Files []guardFile `json:"files"`
}

// guardFile is one stored file of a guard repository.
type guardFile struct {
	RelativePath string `json:"relative_path"`
	Content      string `json:"content"`
}

// guardRow is one stored row as the derive writes it: the name-free build's
// path as raw_path, the relative path, the evidence kind and the name scope
// the production table gives that kind.
type guardRow struct {
	RawPath, RelativePath, EvidenceKind string
	NameScoped                          bool
}

// guardOutput is the derive's output over a corpus, with the counts the guard
// reports.
type guardOutput struct {
	Digest       string
	Repositories int
	Files        int
	Rows         int
	WithRows     int
	KindRows     map[string]int
}

// guardStore is the guard's own in-memory content_files store. It answers the
// three statements of the Reader port by the rules of their SQL: the list is
// in relative_path order with empty content and a row limit, a read returns
// each stored path once and skips a missing one, and the blank check trims
// with strings.TrimSpace. It is read-only after construction, so the
// concurrent chunk reads need no lock. It is not the hydration tests' store,
// so an edit to their fake cannot move the pinned digest.
type guardStore struct {
	rows map[string]File
}

func newGuardStore(files []guardFile) guardStore {
	rows := make(map[string]File, len(files))
	for _, f := range files {
		rows[f.RelativePath] = File{RepoID: guardRepoID, RelativePath: f.RelativePath, Content: f.Content}
	}
	return guardStore{rows: rows}
}

func (s guardStore) ListFiles(_ context.Context, _ string, limit int) ([]File, error) {
	paths := make([]string, 0, len(s.rows))
	for p := range s.rows {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	if limit > 0 && len(paths) > limit {
		paths = paths[:limit]
	}
	files := make([]File, 0, len(paths))
	for _, p := range paths {
		row := s.rows[p]
		row.Content = ""
		files = append(files, row)
	}
	return files, nil
}

func (s guardStore) ReadFiles(_ context.Context, _ string, relativePaths []string) ([]File, error) {
	seen := make(map[string]struct{}, len(relativePaths))
	out := make([]File, 0, len(relativePaths))
	for _, p := range relativePaths {
		row, ok := s.rows[p]
		if _, dup := seen[p]; dup || !ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, row)
	}
	return out, nil
}

func (s guardStore) NonBlankPaths(_ context.Context, _ string, relativePaths []string) ([]string, error) {
	seen := make(map[string]struct{}, len(relativePaths))
	out := make([]string, 0, len(relativePaths))
	for _, p := range relativePaths {
		row, ok := s.rows[p]
		if _, dup := seen[p]; dup || !ok || strings.TrimSpace(row.Content) == "" {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	return out, nil
}

// deriveGuardRows runs the derive's own call over one repository: the
// production LoadConfigPaths with no repository name, no supplied file list
// and the whole-window allowance (storage/postgres/config/artifacts
// derive.go). It maps the rows the way the derive's storedRows does and fails
// on a row the derive would refuse.
func deriveGuardRows(t *testing.T, repo guardRepository) []guardRow {
	t.Helper()
	built, _, err := LoadConfigPaths(context.Background(), newGuardStore(repo.Files), Repository{ID: guardRepoID}, nil, false, AllowanceWholeWindow)
	if err != nil {
		t.Fatalf("%s: LoadConfigPaths() error = %v", repo.Label, err)
	}
	rows := make([]guardRow, 0, len(built))
	for _, item := range built {
		kind := stringValue(item, "evidence_kind")
		scoped, known := NameScoped(kind)
		if !known {
			t.Fatalf("%s: row has evidence kind %q, which no extractor emits", repo.Label, kind)
		}
		if name := stringValue(item, "source_repo"); name != "" {
			t.Fatalf("%s: row carries source_repo %q; the derive's build is name-free", repo.Label, name)
		}
		rows = append(rows, guardRow{
			RawPath:      stringValue(item, "path"),
			RelativePath: stringValue(item, "relative_path"),
			EvidenceKind: kind,
			NameScoped:   scoped,
		})
	}
	sort.Slice(rows, func(a, b int) bool { return lessGuardRow(rows[a], rows[b]) })
	return rows
}

// digestGuardOutput hashes the derive's output over the corpus. The input is
// canonical: repositories sorted by label, rows sorted by raw_path,
// relative_path, evidence_kind and name_scoped, and every field framed as
// "<byte length>:<bytes>", so no field boundary is ambiguous and no map order,
// locale or path separator enters the digest. A repository with no rows still
// contributes its label and a zero row count.
func digestGuardOutput(t *testing.T, repos []guardRepository) guardOutput {
	t.Helper()
	sorted := append([]guardRepository(nil), repos...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Label < sorted[j].Label })
	h := sha256.New()
	out := guardOutput{Repositories: len(sorted), KindRows: map[string]int{}}
	for i, repo := range sorted {
		if i > 0 && repo.Label == sorted[i-1].Label {
			t.Fatalf("guard corpus holds repository label %q twice", repo.Label)
		}
		rows := deriveGuardRows(t, repo)
		writeGuardField(h, repo.Label)
		writeGuardField(h, strconv.Itoa(len(rows)))
		for _, row := range rows {
			writeGuardField(h, row.RawPath)
			writeGuardField(h, row.RelativePath)
			writeGuardField(h, row.EvidenceKind)
			writeGuardField(h, strconv.FormatBool(row.NameScoped))
			out.KindRows[row.EvidenceKind]++
		}
		out.Files += len(repo.Files)
		out.Rows += len(rows)
		if len(rows) > 0 {
			out.WithRows++
		}
	}
	out.Digest = hex.EncodeToString(h.Sum(nil))
	return out
}

func lessGuardRow(a, b guardRow) bool {
	if a.RawPath != b.RawPath {
		return a.RawPath < b.RawPath
	}
	if a.RelativePath != b.RelativePath {
		return a.RelativePath < b.RelativePath
	}
	if a.EvidenceKind != b.EvidenceKind {
		return a.EvidenceKind < b.EvidenceKind
	}
	return !a.NameScoped && b.NameScoped
}

func writeGuardField(h hash.Hash, value string) {
	_, _ = fmt.Fprintf(h, "%d:%s", len(value), value)
}

// readGuardSnapshot reads the committed guard corpus and returns its
// repositories and the digest of its bytes: each file framed by its slash
// path and its byte length, in guardSnapshotFiles order.
func readGuardSnapshot(t *testing.T) ([]guardRepository, string) {
	t.Helper()
	h := sha256.New()
	var repos []guardRepository
	for _, name := range guardSnapshotFiles {
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read guard corpus: %v", err)
		}
		writeGuardField(h, filepath.ToSlash(name))
		writeGuardField(h, string(data))
		scanner := bufio.NewScanner(bytes.NewReader(data))
		scanner.Buffer(make([]byte, 0, 1<<20), 1<<24)
		for line := 1; scanner.Scan(); line++ {
			var repo guardRepository
			if err := json.Unmarshal(scanner.Bytes(), &repo); err != nil {
				t.Fatalf("%s line %d: %v", name, line, err)
			}
			repos = append(repos, repo)
		}
		if err := scanner.Err(); err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
	}
	return repos, hex.EncodeToString(h.Sum(nil))
}
