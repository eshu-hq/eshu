// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
)

// TestServiceCatalogSupportsShapesOnLabCorpus is proof 4 of the repository-arm ruling:
// the service-catalog loader's statement on three call shapes over an existing lab corpus
// (B2C_REPO_ARM_DSN) holding repo:hotrepo, repo:hotrepo2 and repo:hotrepo3 of 160,000
// supports each and repo:mid-1 to repo:mid-200 of at least 1,000 each: one repository;
// three repositories; 200 repositories plus one of 160,000. The loader drains one
// repository at a time, so every repository's pages are measured: no temp, at most 50 ms
// warm, at most 15,000 buffers.
//
// It is a lab proof, not a CI proof. The file is not named *_live_test.go and carries no
// live-postgres-readiness header, because the readiness runner fails a shard on any skip
// and this test always skips without the lab corpus. CI covers the loader's call shape
// through TestSupplyChainAdvisoriesForReposManySmallRepositoriesLive.
func TestServiceCatalogSupportsShapesOnLabCorpus(t *testing.T) {
	dsn := os.Getenv("B2C_REPO_ARM_DSN")
	if dsn == "" {
		t.Skip("B2C_REPO_ARM_DSN is not set; the three call shapes need the lab corpus")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open lab database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("conn: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	mid := make([]string, 0, 201)
	for i := 1; i <= 200; i++ {
		mid = append(mid, fmt.Sprintf("repo:mid-%d", i))
	}
	for name, repos := range map[string][]string{
		"one repository of 160,000":            {"repo:hotrepo"},
		"three repositories of 160,000":        {"repo:hotrepo", "repo:hotrepo2", "repo:hotrepo3"},
		"200 repositories plus one of 160,000": append(append([]string{}, mid...), "repo:hotrepo"),
	} {
		t.Run(name, func(t *testing.T) {
			// The loader drains one repository id at a time (GetSupplyChainAdvisoriesForRepos):
			// a single 200-key drain read 92,259 buffers a page, past the bound. The bound
			// holds for every repository's pages, so the largest page is what is reported.
			worst := 0
			for _, repo := range repos {
				for _, mode := range []string{"force_generic_plan", "force_custom_plan"} {
					page1, midPage, last := repositoryArmPages(t, ctx, db, conn, mode, supplyChainAdvisoriesForReposArms, []string{repo})
					for page, got := range map[string]int{"page 1": page1, "midpoint": midPage, "last page": last} {
						if got > worst {
							worst = got
						}
						if got > 15000 {
							t.Errorf("%s: %s %s %s read %d buffers, want at most 15,000", name, repo, mode, page, got)
						}
					}
				}
			}
			t.Logf("%s: %d drains, largest page %d buffers", name, len(repos), worst)
		})
	}
}
