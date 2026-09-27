// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/content"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// fenceBenchTriggers are migration 109's rolling-upgrade fence triggers.
var fenceBenchTriggers = []string{
	"content_entities_infra_dirty_insert",
	"content_entities_infra_dirty_update",
	"content_entities_infra_dirty_delete",
	"content_entities_infra_dirty_truncate",
}

// TestContentWriterLiveInfraFenceCost is the fence ruling's scripted
// benchmark: the real ContentWriter.Write over the real bootstrap schema
// (both trigram GIN indexes on content_entities), 50,000 K8sResource entities
// of one repository, on a derive-aware connection with the fence triggers
// installed against the same schema with them dropped. Five interleaved rounds
// at the default and the maximum entity batch size, each timing a first Write
// (insert path) and a second Write of the same entities (update path). It
// logs medians as No-Regression Evidence; it asserts only that the fenced
// session never marks a repository.
//
// Set ESHU_INFRA_FENCE_BENCH=1, ESHU_TEST_CONTENT_INDEX_POSTGRES_DSN (an
// administrative postgres-database DSN), and
// ESHU_TEST_CONTENT_INDEX_POSTGRES_DISPOSABLE=1 to run it.
func TestContentWriterLiveInfraFenceCost(t *testing.T) {
	if os.Getenv("ESHU_INFRA_FENCE_BENCH") != "1" {
		t.Skip("set ESHU_INFRA_FENCE_BENCH=1 to run the infra fence cost benchmark")
	}
	ctx, db := postgresproof.OpenDisposableDatabase(t,
		os.Getenv("ESHU_TEST_CONTENT_INDEX_POSTGRES_DSN"),
		os.Getenv("ESHU_TEST_CONTENT_INDEX_POSTGRES_DISPOSABLE"),
		30*time.Minute)
	if err := ApplyBootstrap(ctx, SQLDB{DB: db}); err != nil {
		t.Fatalf("apply bootstrap schema: %v", err)
	}

	const entities = 50000
	for _, batch := range []int{contentEntityBatchSize, 4000} {
		samples := map[string][]time.Duration{}
		for round := range 5 {
			order := []bool{true, false}
			if round%2 == 1 {
				order = []bool{false, true}
			}
			for _, fenced := range order {
				insert, update := fenceBenchRun(t, ctx, db, fenced, batch, entities)
				key := "dropped"
				if fenced {
					key = "fenced"
				}
				samples[key+"/insert"] = append(samples[key+"/insert"], insert)
				samples[key+"/update"] = append(samples[key+"/update"], update)
			}
		}
		for _, path := range []string{"insert", "update"} {
			dropped := fenceBenchMedian(samples["dropped/"+path])
			fenced := fenceBenchMedian(samples["fenced/"+path])
			t.Logf("No-Regression Evidence: batch=%d path=%s entities=%d triggers_dropped_median=%s fenced_median=%s delta=%+.2f%% (%+.3f us/row) fenced_range=%s-%s dropped_range=%s-%s",
				batch, path, entities, dropped, fenced,
				(float64(fenced)/float64(dropped)-1)*100,
				float64(fenced-dropped)/float64(time.Microsecond)/entities,
				slices.Min(samples["fenced/"+path]), slices.Max(samples["fenced/"+path]),
				slices.Min(samples["dropped/"+path]), slices.Max(samples["dropped/"+path]))
		}
	}
}

// fenceBenchRun resets the content and read model tables, installs or drops
// the fence triggers, and times two Writes of the same entities.
func fenceBenchRun(t *testing.T, ctx context.Context, db *sql.DB, fenced bool, batch, entities int) (time.Duration, time.Duration) {
	t.Helper()
	for _, stmt := range []string{
		"TRUNCATE infra_resource_entities",
		"TRUNCATE content_entities, content_files, content_file_references, content_file_secret_lines",
		"TRUNCATE infra_resource_entity_dirty_repos",
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	if fenced {
		if _, err := db.ExecContext(ctx, MigrationSQL("infra_resource_entities")); err != nil {
			t.Fatalf("install fence triggers: %v", err)
		}
	} else {
		for _, trigger := range fenceBenchTriggers {
			if _, err := db.ExecContext(ctx, "DROP TRIGGER IF EXISTS "+trigger+" ON content_entities"); err != nil {
				t.Fatalf("drop %s: %v", trigger, err)
			}
		}
	}
	if _, err := db.ExecContext(ctx, "CHECKPOINT"); err != nil {
		t.Fatalf("checkpoint: %v", err)
	}
	writer := NewContentWriter(SQLDB{DB: db}).WithEntityBatchSize(batch)
	var elapsed [2]time.Duration
	for pass, name := range []string{"n", "m"} {
		mat := fenceBenchMaterialization(entities, name)
		start := time.Now()
		if _, err := writer.Write(ctx, mat); err != nil {
			t.Fatalf("Write() pass %d error = %v", pass, err)
		}
		elapsed[pass] = time.Since(start)
	}
	var marks int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM infra_resource_entity_dirty_repos").Scan(&marks); err != nil {
		t.Fatalf("count marks: %v", err)
	}
	if marks != 0 {
		t.Fatalf("a derive-aware Write left %d fence marks", marks)
	}
	return elapsed[0], elapsed[1]
}

// fenceBenchMaterialization builds one repository of K8sResource entities,
// ten per file, with production-width source and metadata.
func fenceBenchMaterialization(entities int, namePrefix string) content.Materialization {
	source := strings.Repeat("kind: Deployment\nmetadata:\n  name: service\n", 6)
	mat := content.Materialization{RepoID: "repo-fence-bench", ScopeID: "scope-bench", GenerationID: "gen-bench"}
	for file := range entities / 10 {
		path := fmt.Sprintf("deploy/%05d.yaml", file)
		mat.Records = append(mat.Records, content.Record{Path: path, Body: source})
		for i := range 10 {
			n := file*10 + i
			mat.Entities = append(mat.Entities, content.EntityRecord{
				EntityID:    fmt.Sprintf("bench-%06d", n),
				Path:        path,
				EntityType:  "K8sResource",
				EntityName:  fmt.Sprintf("%s-%06d", namePrefix, n),
				StartLine:   1 + i*6,
				EndLine:     6 + i*6,
				Language:    "yaml",
				SourceCache: source,
				Metadata:    map[string]any{"kind": "Deployment", "provider": "kubernetes", "environment": "staging"},
			})
		}
	}
	return mat
}

func fenceBenchMedian(samples []time.Duration) time.Duration {
	sorted := slices.Clone(samples)
	slices.Sort(sorted)
	return sorted[len(sorted)/2]
}
