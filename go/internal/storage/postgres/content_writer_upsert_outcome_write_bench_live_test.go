// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8950 disposable PostgreSQL 18 measurement of what the content_files upsert outcome pre-read adds to a whole ContentWriter.Write of 1, 5, 20 and 50 small files, with and without ten entities per file, as an observed writer against an unobserved one interleaved on one database; opt-in (ESHU_CONTENT_UPSERT_OUTCOME_BENCH=1), otherwise a one-round smoke; enrolled in the live-postgres-readiness runner; bridges the family ESHU_GENERATION_RETENTION_PROOF_DSN onto ESHU_POSTGRES_TEST_DSN as the retention proofs do; wall time is logged, not gated
//eshu:readiness:env ESHU_AWS_DRIFT_ARN_PROOF_DISPOSABLE ESHU_AWS_DRIFT_ARN_PROOF_DSN ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE ESHU_DEFERRED_PARTITION_PROOF_DSN ESHU_GENERATION_RETENTION_PROOF_DISPOSABLE ESHU_GENERATION_RETENTION_PROOF_DSN ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN ESHU_REDUCER_FAIRNESS_PROOF_DSN

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/content"
)

// withEntities adds entitiesPerFile function entities to every record, the
// shape a parsed source file brings to a Write.
func withEntities(records []content.Record, entitiesPerFile int) []content.EntityRecord {
	var entities []content.EntityRecord
	for _, rec := range records {
		for i := range entitiesPerFile {
			entities = append(entities, content.EntityRecord{
				EntityID: fmt.Sprintf("%s#%d", rec.Path, i), Path: rec.Path, EntityType: "function",
				EntityName: fmt.Sprintf("fn%d", i), StartLine: i + 1, EndLine: i + 2,
			})
		}
	}
	return entities
}

// TestContentFileUpsertOutcomeWriteCostLive times whole ContentWriter.Write
// calls of 1, 5, 20 and 50 small files with an observed writer (the outcome
// counters on, with the shipped sampling policy) and an unobserved one, alternating on one
// database and one repository each, for a new-commit rewrite and for an
// identical replay. A Write with entities is the common shape; a Write of files
// alone is the cheapest and so the pessimistic one. It logs medians and the
// observed overhead as a share of the whole Write and asserts nothing.
// ESHU_CONTENT_UPSERT_OUTCOME_BENCH=1 runs 60 rounds; the default is a smoke.
func TestContentFileUpsertOutcomeWriteCostLive(t *testing.T) {
	rounds := 2
	if os.Getenv("ESHU_CONTENT_UPSERT_OUTCOME_BENCH") == "1" {
		rounds = 60
	}
	ctx, database := openContentUpsertGuardDatabase(t)
	// The observed writer runs the shipped sampling policy: batches under
	// contentFileOutcomeMinBatchRows rows are classified one in
	// contentFileOutcomeSampleEvery.
	observed, _ := newLiveOutcomeWriter(t, database)
	observed = observed.WithOutcomeSampling(contentFileOutcomeMinBatchRows, contentFileOutcomeSampleEvery)
	plain := NewContentWriter(SQLDB{DB: database})
	plain.Now = observed.Now

	for _, files := range []int{1, 5, 20, 50} {
		for _, entitiesPerFile := range []int{0, 10} {
			records := smallRowCorpus(files)
			stamp := func(commit string) []content.Record {
				out := make([]content.Record, len(records))
				for i, rec := range records {
					rec.Metadata = map[string]string{"commit_sha": commit, "language": "yaml"}
					out[i] = rec
				}
				return out
			}
			entities := withEntities(records, entitiesPerFile)
			write := func(w ContentWriter, repoID, gen, commit string) time.Duration {
				mat := content.Materialization{RepoID: repoID, ScopeID: "scope-" + repoID, GenerationID: gen, Records: stamp(commit), Entities: entities}
				start := time.Now()
				if _, err := w.Write(ctx, mat); err != nil {
					t.Fatalf("Write %s: %v", repoID, err)
				}
				return time.Since(start)
			}
			label := fmt.Sprintf("files=%d entities_per_file=%d", files, entitiesPerFile)
			obsRepo, plainRepo := "repo-8950-wc-obs-"+label, "repo-8950-wc-plain-"+label
			write(observed, obsRepo, "gen-0", "c-seed")
			write(plain, plainRepo, "gen-0", "c-seed")
			for _, phase := range []string{"rewrite_new_commit", "identical_replay"} {
				var obs, pl []time.Duration
				for round := range rounds {
					commit := fmt.Sprintf("c-%d", round)
					if phase == "identical_replay" {
						commit = fmt.Sprintf("c-%d", rounds-1)
					}
					first, second := observed, plain
					if round%2 == 1 {
						first, second = plain, observed
					}
					repoOf := func(w ContentWriter) string {
						if w.instruments != nil {
							return obsRepo
						}
						return plainRepo
					}
					d1 := write(first, repoOf(first), "gen-0", commit)
					d2 := write(second, repoOf(second), "gen-0", commit)
					if first.instruments != nil {
						obs, pl = append(obs, d1), append(pl, d2)
					} else {
						obs, pl = append(obs, d2), append(pl, d1)
					}
				}
				mo, mp := medianDuration(obs), medianDuration(pl)
				t.Logf("WRITEBENCH %s %s rounds=%d write_unobserved_ms=%.2f write_observed_ms=%.2f overhead_pct=%.2f",
					label, phase, rounds, float64(mp)/1e6, float64(mo)/1e6, 100*float64(mo-mp)/float64(mp))
			}
		}
	}
}
