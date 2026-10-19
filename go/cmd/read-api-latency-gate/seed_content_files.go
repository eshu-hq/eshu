// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"crypto/md5"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Story and search seed corpus for the #7009 pilot operations (issue #6797's
// gate extended to path-parameter routes and MCP tools). The plan is pure
// and deterministic: content derives from repository and file indexes, so a
// re-seed builds byte-identical rows. Sizes are representative, not maximal:
// the story anchor plus related set must make the deployment-artifact stages
// do real list/hydrate/parse work, and the search documents must make the
// unscoped walk visit real windows, while the whole seed stays inside the CI
// job's time budget (COPY, like every other seeder here).
const (
	// SeedStoryAnchorID is the repository id the story pilot reads. It is
	// stable across runs so the pilot operation can name it.
	SeedStoryAnchorID = "seed-story-anchor"
	// SeedStoryAnchorName is the graph repository name of the anchor.
	SeedStoryAnchorName = "Seed Story Anchor"
	// SeedStoryRelatedCount is the related-repository count of the story
	// corpus. Thirty-two related repositories make the story's related-set
	// fan-out (list, hydrate and parse per repository) measurable without
	// paying the full QA-scale related set on every CI run.
	SeedStoryRelatedCount = 32
	// SeedStoryRepositoryCount is the graph Repository node count the story
	// corpus adds: the anchor plus every related repository. The
	// mcp_index_status pilot asserts repository_count, so it names this
	// constant instead of zero (see validateIndexStatus).
	SeedStoryRepositoryCount = SeedStoryRelatedCount + 1

	// SeedSearchFileToken is the common token of the seeded file documents.
	// About one file in three carries it, spread across every search
	// repository, so the unscoped file walk fills a limit-10 page from its
	// key-ordered windows and answers exact.
	SeedSearchFileToken = "gateseedcommon"
	// SeedSearchRareToken is carried by three files of one repository only,
	// the selective control next to the common token.
	SeedSearchRareToken = "gateseedrare"
	// SeedSearchRepoCount and SeedSearchFilesPerRepo size the file-search
	// corpus: 6,000 documents across 12 repositories.
	SeedSearchRepoCount    = 12
	SeedSearchFilesPerRepo = 500
	// SeedEntityToken is carried in the source_cache of every seeded gate
	// entity, so the unscoped entity walk fills its page from the probe.
	SeedEntityToken = "gateentitytoken"
	// SeedEntityRowCount sizes the gate entity corpus. Its entity_type is
	// GateSeedEntity, which no other seeder uses, so the existing
	// per-entity-type count verifier does not see these rows.
	SeedEntityRowCount = 300
	// SeedEntityType is the entity_type of the gate entity rows.
	SeedEntityType = "GateSeedEntity"
)

// SeedStoryRelatedID returns the repository id of related repository i.
func SeedStoryRelatedID(i int) string {
	return fmt.Sprintf("seed-story-rel-%02d", i)
}

// SeedSearchRepoID returns the repository id of search repository i.
func SeedSearchRepoID(i int) string {
	return fmt.Sprintf("seed-search-%02d", i)
}

// contentFileColumns is the content_files column list contentFileCopyRows
// shapes, in order. content_lower is a generated column the seed never
// writes; indexed_at has no default so the seed supplies it.
var contentFileColumns = []string{
	"repo_id", "relative_path", "commit_sha", "content",
	"content_hash", "line_count", "language", "artifact_type", "indexed_at",
}

// ContentFileSeedRow is one planned content_files row.
type ContentFileSeedRow struct {
	RepoID       string
	RelativePath string
	Content      string
	Language     string
	ArtifactType string
}

// contentFileCopyRows shapes rows for CopyFrom with the seed time.
func contentFileCopyRows(rows []ContentFileSeedRow, now time.Time) [][]any {
	out := make([][]any, 0, len(rows))
	for _, row := range rows {
		sum := md5.Sum([]byte(row.RepoID + "\x00" + row.RelativePath + "\x00" + row.Content))
		lines := strings.Count(row.Content, "\n") + 1
		out = append(out, []any{
			row.RepoID, row.RelativePath, "seed",
			row.Content, fmt.Sprintf("%x", sum), lines,
			row.Language, row.ArtifactType, now,
		})
	}
	return out
}

// seedFillerGo is a small non-candidate source file.
func seedFillerGo(repoIdx, fileIdx int) ContentFileSeedRow {
	return ContentFileSeedRow{
		RelativePath: fmt.Sprintf("src/f%04d.go", fileIdx),
		Content: fmt.Sprintf("package src\n\n// filler for %d/%d\nfunc filler%d() int {\n\treturn %d\n}\n",
			repoIdx, fileIdx, fileIdx, repoIdx+fileIdx),
		Language:     "go",
		ArtifactType: "source",
	}
}

// seedModuleTF is a Terraform file with a module block, which the HCL
// extractor turns into a config-path row.
func seedModuleTF(dir string, idx int) ContentFileSeedRow {
	return ContentFileSeedRow{
		RelativePath: fmt.Sprintf("%s/mod-%02d.tf", dir, idx),
		Content:      fmt.Sprintf("module \"m%d\" {\n  source = \"./mod%d\"\n}\n", idx, idx),
		Language:     "hcl",
		ArtifactType: "source",
	}
}

// seedComposeFile is a Docker Compose file under its conventional name.
func seedComposeFile(dir string) ContentFileSeedRow {
	return ContentFileSeedRow{
		RelativePath: dir + "/docker-compose.yml",
		Content:      "services:\n  web:\n    image: example/web:seed\n    ports: [\"8080:8080\"]\n",
		Language:     "yaml",
		ArtifactType: "source",
	}
}

// seedKustomization is a kustomization naming one resource beside it.
func seedKustomization(dir string) ContentFileSeedRow {
	return ContentFileSeedRow{
		RelativePath: dir + "/kustomization.yaml",
		Content:      "apiVersion: kustomize.config.k8s.io/v1beta1\nkind: Kustomization\nresources: [app.yaml]\n",
		Language:     "yaml",
		ArtifactType: "source",
	}
}

// seedWorkflowFile is a GitHub Actions workflow with an image reference,
// which the workflow and CI/CD-evidence stages read.
func seedWorkflowFile(idx int) ContentFileSeedRow {
	return ContentFileSeedRow{
		RelativePath: fmt.Sprintf(".github/workflows/seed-%02d.yml", idx),
		Content:      "name: seed\non: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/checkout@v4\n      - run: echo seed\n",
		Language:     "yaml",
		ArtifactType: "source",
	}
}

// BuildStoryContentRows plans the story corpus: the anchor's file list plus
// every related repository's list. Anchor candidates exercise the
// compose/workflow/runtime anchor loaders; related candidates exercise the
// per-repository config load under the floor-plus-budget allowance.
func BuildStoryContentRows() []ContentFileSeedRow {
	rows := make([]ContentFileSeedRow, 0, 620+SeedStoryRelatedCount*120)
	for i := 0; i < 480; i++ {
		row := seedFillerGo(0, i)
		row.RepoID = SeedStoryAnchorID
		rows = append(rows, row)
	}
	for i := 0; i < 60; i++ {
		row := seedWorkflowFile(i)
		row.RepoID = SeedStoryAnchorID
		rows = append(rows, row)
	}
	for i := 0; i < 20; i++ {
		for _, row := range []ContentFileSeedRow{
			seedComposeFile(fmt.Sprintf("deploy/c%02d", i)),
			seedKustomization(fmt.Sprintf("deploy/k%02d", i)),
			{RelativePath: fmt.Sprintf("deploy/k%02d/app.yaml", i), Content: "app: seed\n", Language: "yaml", ArtifactType: "source"},
			seedModuleTF("infra", i),
		} {
			row.RepoID = SeedStoryAnchorID
			rows = append(rows, row)
		}
	}
	for r := 0; r < SeedStoryRelatedCount; r++ {
		repoID := SeedStoryRelatedID(r)
		for i := 0; i < 100; i++ {
			row := seedFillerGo(r+1, i)
			row.RepoID = repoID
			rows = append(rows, row)
		}
		for i := 0; i < 12; i++ {
			row := seedModuleTF("infra", i)
			row.RepoID = repoID
			rows = append(rows, row)
		}
		for i := 0; i < 4; i++ {
			for _, row := range []ContentFileSeedRow{
				seedComposeFile(fmt.Sprintf("deploy/c%02d", i)),
				seedKustomization(fmt.Sprintf("deploy/k%02d", i)),
			} {
				row.RepoID = repoID
				rows = append(rows, row)
			}
		}
	}
	return rows
}

// BuildSearchContentRows plans the file-search corpus: SeedSearchRepoCount
// repositories of SeedSearchFilesPerRepo two-KiB documents. The common token
// lands in about one file in three, spread across every repository, so the
// unscoped walk meets matches in its first windows; the rare token lands in
// three files of repository zero only.
func BuildSearchContentRows() []ContentFileSeedRow {
	rows := make([]ContentFileSeedRow, 0, SeedSearchRepoCount*SeedSearchFilesPerRepo)
	for r := 0; r < SeedSearchRepoCount; r++ {
		repoID := SeedSearchRepoID(r)
		for g := 0; g < SeedSearchFilesPerRepo; g++ {
			var body strings.Builder
			for k := 0; k < 60; k++ {
				sum := md5.Sum([]byte(fmt.Sprintf("%d:%d:%d", r, g, k)))
				fmt.Fprintf(&body, "%x ", sum)
			}
			if (g*7+r)%10 < 3 {
				body.WriteString(SeedSearchFileToken)
			}
			if r == 0 && g < 3 {
				body.WriteString(" " + SeedSearchRareToken)
			}
			rows = append(rows, ContentFileSeedRow{
				RepoID:       repoID,
				RelativePath: fmt.Sprintf("src/file-%05d.go", g),
				Content:      body.String(),
				Language:     "go",
				ArtifactType: "source",
			})
		}
	}
	return rows
}

// gateEntityColumns is the content_entities column list gateEntityCopyRows
// shapes, matching the seeder the infra rows use.
var gateEntityColumns = []string{
	"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
	"start_line", "end_line", "source_cache", "metadata", "indexed_at",
}

// gateEntityCopyRows shapes the gate entity rows for CopyFrom:
// SeedEntityRowCount rows whose source_cache all carry the entity token,
// spread across the first three search repositories so the unscoped entity
// walk fills its page from the probe.
func gateEntityCopyRows(now time.Time) [][]any {
	rows := make([][]any, 0, SeedEntityRowCount)
	for i := 0; i < SeedEntityRowCount; i++ {
		repoID := SeedSearchRepoID(i % 3)
		rows = append(rows, []any{
			fmt.Sprintf("gate-entity-%04d", i),
			repoID,
			fmt.Sprintf("src/ent-%04d.go", i),
			SeedEntityType,
			fmt.Sprintf("gate_entity_%04d", i),
			1, 20,
			fmt.Sprintf("func gate_entity_%04d() int {\n\t// %s marker %d\n\treturn %d\n}\n", i, SeedEntityToken, i, i),
			"{}",
			now,
		})
	}
	return rows
}

// SeedContentFiles bulk-inserts the story, file-search and entity-search
// corpora. analyzeSeededTables refreshes the plans afterwards, so the sweep
// reads plan against fresh statistics.
func SeedContentFiles(ctx context.Context, pool *pgxpool.Pool, now time.Time) error {
	story := BuildStoryContentRows()
	if _, err := pool.CopyFrom(ctx,
		pgx.Identifier{"content_files"},
		contentFileColumns,
		pgx.CopyFromRows(contentFileCopyRows(story, now)),
	); err != nil {
		return fmt.Errorf("seed story content_files: %w", err)
	}
	search := BuildSearchContentRows()
	if _, err := pool.CopyFrom(ctx,
		pgx.Identifier{"content_files"},
		contentFileColumns,
		pgx.CopyFromRows(contentFileCopyRows(search, now)),
	); err != nil {
		return fmt.Errorf("seed search content_files: %w", err)
	}
	if _, err := pool.CopyFrom(ctx,
		pgx.Identifier{"content_entities"},
		gateEntityColumns,
		pgx.CopyFromRows(gateEntityCopyRows(now)),
	); err != nil {
		return fmt.Errorf("seed gate content_entities: %w", err)
	}
	return nil
}

// ExpectedContentFileCounts is the per-prefix row count a correctly seeded
// content_files table holds.
func ExpectedContentFileCounts() map[string]int {
	return map[string]int{
		"seed-story-":  len(BuildStoryContentRows()),
		"seed-search-": len(BuildSearchContentRows()),
	}
}

// VerifyContentFileCounts reads back the per-prefix content_files row counts
// and fails when any differs, so a seed that silently under-creates cannot
// leave a shrunken corpus reading as GREEN.
func VerifyContentFileCounts(ctx context.Context, pool *pgxpool.Pool, expected map[string]int) error {
	for prefix, want := range expected {
		var got int64
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM content_files WHERE repo_id LIKE $1", prefix+"%").Scan(&got); err != nil {
			return fmt.Errorf("count content_files %s: %w", prefix, err)
		}
		if int(got) != want {
			return fmt.Errorf("content_files %s rows = %d, want %d", prefix, got, want)
		}
	}
	var entities int64
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM content_entities WHERE entity_type = $1", SeedEntityType).Scan(&entities); err != nil {
		return fmt.Errorf("count gate content_entities: %w", err)
	}
	if int(entities) != SeedEntityRowCount {
		return fmt.Errorf("gate content_entities rows = %d, want %d", entities, SeedEntityRowCount)
	}
	return nil
}

// VerifyContentSearchReadiness runs the two readiness functions the unscoped
// searches gate on and fails fast naming the check, so a gate run against a
// database whose trigram indexes are not valid reports the seed gap instead
// of sweeping 503s.
func VerifyContentSearchReadiness(ctx context.Context, pool *pgxpool.Pool) error {
	if _, err := pool.Exec(ctx, "SELECT eshu_require_content_substring_indexes_ready()"); err != nil {
		return fmt.Errorf("content substring index readiness: %w", err)
	}
	if _, err := pool.Exec(ctx, "SELECT eshu_require_content_lower_trgm_index_ready()"); err != nil {
		return fmt.Errorf("content lowercase trigram index readiness: %w", err)
	}
	return nil
}
