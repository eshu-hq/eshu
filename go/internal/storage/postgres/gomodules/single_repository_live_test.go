// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gomodules

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10032 real PostgreSQL proof that the operator single-repository read returns the exact row the rotation page read returns for that repository, with the page's total-candidates count, and fails closed on unknown ids and missing active generations
//eshu:readiness:env ESHU_POSTGRES_DISPOSABLE ESHU_POSTGRES_DSN

import (
	"errors"
	"os"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/scannercomponent"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// TestGetGoModuleRepositoryMatchesPageRowLive proves the operator read and the
// rotation page read agree on live Postgres: the same row for the same
// repository (id, remote, commit, go.mod and evidence flags) and the page's
// total-candidates count, plus fail-closed refusals for an unknown id and a
// repository whose active generation is gone.
func TestGetGoModuleRepositoryMatchesPageRowLive(t *testing.T) {
	if os.Getenv("ESHU_POSTGRES_DSN") == "" {
		t.Skip("ESHU_POSTGRES_DSN is not set")
	}
	database := openStoreProof(t)
	goID := seedRepository(t, database, 1, "go.mod")
	seedRepository(t, database, 2, "")
	seedRepository(t, database, 3, "nested/go.mod")

	store := NewStore(postgres.SQLDB{DB: database})
	page, err := store.ListGoModuleRepositories(t.Context(),
		scannercomponent.GoModuleRepositoryFilter{PageSize: scannercomponent.GoModulePageSize})
	if err != nil {
		t.Fatalf("read page: %v", err)
	}
	var want *scannercomponent.GoModuleRepository
	for i := range page.Repositories {
		if page.Repositories[i].RepositoryID == goID {
			want = &page.Repositories[i]
		}
	}
	if want == nil {
		t.Fatalf("page = %+v, want the go.mod repository %q", page.Repositories, goID)
	}

	read, err := store.GetGoModuleRepository(t.Context(), goID)
	if err != nil {
		t.Fatalf("GetGoModuleRepository() error = %v", err)
	}
	if read.Repository != *want {
		t.Fatalf("single read = %+v, want page row %+v", read.Repository, *want)
	}
	if !read.HasGoModule {
		t.Fatal("HasGoModule = false, want true for a stored go.mod")
	}
	if read.TotalCandidates != page.TotalCandidates {
		t.Fatalf("TotalCandidates = %d, want page total %d", read.TotalCandidates, page.TotalCandidates)
	}

	if _, err := store.GetGoModuleRepository(t.Context(), "repository:r_deadbeef"); !errors.Is(err, ErrGoModuleRepositoryNotFound) {
		t.Fatalf("unknown id error = %v, want %v", err, ErrGoModuleRepositoryNotFound)
	}
	if _, err := store.GetGoModuleRepository(t.Context(), "  "); !errors.Is(err, ErrGoModuleRepositoryNotFound) {
		t.Fatalf("blank id error = %v, want %v", err, ErrGoModuleRepositoryNotFound)
	}
	if _, err := database.ExecContext(t.Context(),
		`UPDATE ingestion_scopes SET active_generation_id = NULL WHERE source_key = $1`, goID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetGoModuleRepository(t.Context(), goID); !errors.Is(err, ErrGoModuleRepositoryNoActiveGeneration) {
		t.Fatalf("missing generation error = %v, want %v", err, ErrGoModuleRepositoryNoActiveGeneration)
	}
}
