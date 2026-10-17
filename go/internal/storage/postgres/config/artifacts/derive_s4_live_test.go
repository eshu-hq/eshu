// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts_test

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8155 (#7126 PR3b-2b) S4-style equality of the derive core: every tests/fixtures directory and child directory plus the 26 recorded S0 corpus repositories are written to content_files, derived with DeriveRepository, and the stored rows after the read-time name step equal the live path's rows (the query package's ContentReader through LoadConfigPaths with the graph name, allowance -1) with 0 / 0 differing rows; the derive's own reader also equals the live reader; 220 repositories, 160 rows, about 4 s on a local PostgreSQL 18.6 on 2026-10-17; enrolled in the live-postgres-readiness runner
//eshu:readiness:env ESHU_TEST_CONTENT_INDEX_POSTGRES_DISPOSABLE ESHU_TEST_CONTENT_INDEX_POSTGRES_DSN

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	contentartifacts "github.com/eshu-hq/eshu/go/internal/content/artifacts"
	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/config/artifacts"
)

// recordedCorpusRepositories loads the recorded file sets of the S0 corpus
// (content/artifacts/testdata/name_free_corpus.jsonl) as repositories, each
// bound to its recorded name.
func recordedCorpusRepositories(t *testing.T) []fixtureRepository {
	t.Helper()
	handle, err := os.Open(filepath.Join("..", "..", "..", "..", "content", "artifacts", "testdata", "name_free_corpus.jsonl"))
	if err != nil {
		t.Fatalf("open recorded corpus: %v", err)
	}
	defer func() { _ = handle.Close() }()
	scanner := bufio.NewScanner(handle)
	scanner.Buffer(make([]byte, 0, 1<<20), 16<<20)
	var repos []fixtureRepository
	for i := 0; scanner.Scan(); i++ {
		var recorded struct {
			Name  string `json:"name"`
			Files []struct {
				RelativePath string `json:"relative_path"`
				Content      string `json:"content"`
			} `json:"files"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &recorded); err != nil {
			t.Fatalf("decode recorded corpus line %d: %v", i, err)
		}
		repo := fixtureRepository{ID: fmt.Sprintf("recorded:%02d", i), Name: recorded.Name}
		for _, file := range recorded.Files {
			repo.Files = append(repo.Files, contentartifacts.File{RelativePath: file.RelativePath, Content: file.Content})
		}
		repos = append(repos, repo)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("read recorded corpus: %v", err)
	}
	return repos
}

// TestDeriveStoredRowsEqualLivePathLive is the S4-style equality over the
// golden fixture repositories in this repository: for every repository the
// stored rows, after the read-time name step, equal the rows the live path
// builds from the same content_files state (the query package's
// ContentReader through contentartifacts.LoadConfigPaths with the graph name,
// allowance -1). Differing rows are counted both ways and must be 0 / 0.
func TestDeriveStoredRowsEqualLivePathLive(t *testing.T) {
	ctx, db := openDatabase(t)
	repos := append(fixtureRepositories(t), recordedCorpusRepositories(t)...)
	for _, repo := range repos {
		putFiles(ctx, t, db, repo.ID, repo.Files)
	}
	deriver := newLiveDeriver(t, db, testExtractorVersion)
	liveReader := liveQueryReader{reader: query.NewContentReader(db)}
	deriveReader := artifacts.NewContentReader(postgres.SQLDB{DB: db})

	var totalStored, totalLive, missingStored, extraStored, repositoriesWithRows int
	for _, repo := range repos {
		result := deriver.DeriveRepository(ctx, artifacts.Request{RepoID: repo.ID, Trigger: artifacts.TriggerBackfill})
		if result.Outcome != artifacts.OutcomeOK {
			t.Fatalf("derive %s = %+v, want ok", repo.ID, result)
		}
		stored := rowKeys(applyName(storedRows(ctx, t, db, repo.ID), repo.Name))
		repository := contentartifacts.Repository{ID: repo.ID, Name: repo.Name}
		live, _, err := contentartifacts.LoadConfigPaths(ctx, liveReader, repository, nil, false, contentartifacts.AllowanceWholeWindow)
		if err != nil {
			t.Fatalf("live load of %s: %v", repo.ID, err)
		}
		liveKeys := rowKeys(live)
		// The derive's own reader must agree with the live reader too.
		viaDerive, _, err := contentartifacts.LoadConfigPaths(ctx, deriveReader, repository, nil, false, contentartifacts.AllowanceWholeWindow)
		if err != nil {
			t.Fatalf("derive-reader load of %s: %v", repo.ID, err)
		}
		if missing := missingFrom(liveKeys, rowKeys(viaDerive)); len(missing) > 0 || len(viaDerive) != len(live) {
			t.Fatalf("%s: derive reader differs from the live reader: missing %q", repo.ID, missing)
		}
		if result.Rows != len(storedRows(ctx, t, db, repo.ID)) {
			t.Fatalf("%s: Result.Rows = %d, stored %d", repo.ID, result.Rows, len(storedRows(ctx, t, db, repo.ID)))
		}
		gone := missingFrom(liveKeys, stored)
		extra := missingFrom(stored, liveKeys)
		for _, key := range gone {
			t.Errorf("%s (name %q): live row missing from stored rows: %q", repo.ID, repo.Name, key)
		}
		for _, key := range extra {
			t.Errorf("%s (name %q): stored row absent from live rows: %q", repo.ID, repo.Name, key)
		}
		totalStored += len(stored)
		totalLive += len(liveKeys)
		missingStored += len(gone)
		extraStored += len(extra)
		if len(liveKeys) > 0 {
			repositoriesWithRows++
		}
	}
	t.Logf("S4 equality: %d repositories (%d with rows), %d live rows, %d stored rows, differing %d / %d",
		len(repos), repositoriesWithRows, totalLive, totalStored, missingStored, extraStored)
	if repositoriesWithRows < 10 || totalLive < 50 {
		t.Fatalf("corpus too small to prove equality: %d repositories with rows, %d rows", repositoriesWithRows, totalLive)
	}
}
