// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build replayseed

package postgres

// Seeder for the #10882 built-binary replay (scripts/dev/10882-replay). It is a
// build-tagged test program, compiled once with `go test -c -tags replayseed`
// at the same commit as the product binaries and run by the driver, one mode per
// test. It touches only the disposable database named by ESHU_REPLAY_DSN.
//
// Modes (each is one Test function, selected with -test.run):
//
//	TestReplaySeedBacklog   the reducer backlog: the #10862 mix plus source A's
//	                        ~13,000 aged claimable rows (the 10x shape)
//	TestReplaySeedProjector source A's projector generations, through the
//	                        production IngestionStore.CommitScopeGeneration
//	TestReplayFeedB         source B's steady fresh generations, fixed seed and
//	                        cadence, through the same production commit path
//	TestReplayCompleteA     the harness catch-up step: claims A's aged reducer rows
//	                        and acks them through ReducerQueue.ClaimBatch/AckBatch
//
// What the seed is NOT: reducer facts are synthetic (the #10862 mix has no real
// facts), so no reducer binary runs against it; fact_records equality on seeded
// generations holds by construction and is not a no-loss proof.

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/facts"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// replayDomainA is the reducer domain of source A's seeded rows. The mix never
// uses it and no projector intent producer targets it, so a ClaimDomains-pinned
// queue claims only seeded A rows.
const replayDomainA = reducercontract.DomainCloudAssetResolution

// replayEnv reads one required environment value.
func replayEnv(t *testing.T, name string) string {
	t.Helper()
	value := os.Getenv(name)
	if value == "" {
		t.Skipf("set %s to run the replay seeder", name)
	}
	return value
}

// replayEnvInt reads an optional integer environment value.
func replayEnvInt(t *testing.T, name string, fallback int) int {
	t.Helper()
	raw := os.Getenv(name)
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		t.Fatalf("%s = %q, want an integer: %v", name, raw, err)
	}
	return value
}

// replayEnvDuration reads an optional duration environment value.
func replayEnvDuration(t *testing.T, name string, fallback time.Duration) time.Duration {
	t.Helper()
	raw := os.Getenv(name)
	if raw == "" {
		return fallback
	}
	value, err := time.ParseDuration(raw)
	if err != nil {
		t.Fatalf("%s = %q, want a duration: %v", name, raw, err)
	}
	return value
}

// replayDB opens the disposable replay database.
func replayDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := replayEnv(t, "ESHU_REPLAY_DSN")
	database, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open replay database: %v", err)
	}
	database.SetMaxOpenConns(4)
	t.Cleanup(func() { _ = database.Close() })
	if err := database.PingContext(context.Background()); err != nil {
		t.Fatalf("ping replay database: %v", err)
	}
	return database
}

var _ = stdlib.GetDefaultDriver // register the pgx database/sql driver

// replaySource names one replay source: its source_system and the collector
// kind its scopes carry. Source A and B swap between git and a hosted family
// across arms.
type replaySource struct {
	System    string
	Collector scope.CollectorKind
}

func replaySourceFromEnv(t *testing.T, prefix string) replaySource {
	t.Helper()
	return replaySource{
		System:    replayEnv(t, "ESHU_REPLAY_SOURCE_"+prefix),
		Collector: scope.CollectorKind(replayEnv(t, "ESHU_REPLAY_COLLECTOR_"+prefix)),
	}
}

// replayGeneration builds one deterministic seeded generation: a repository
// fact and file and content_entity facts, the shapes the projector's canonical
// builder reads. The id comes from the source prefix and ordinal only, so every
// arm seeds byte-identical generations.
func replayGeneration(src replaySource, kind string, ordinal int, observed time.Time) (scope.IngestionScope, scope.ScopeGeneration, []facts.Envelope) {
	scopeID := fmt.Sprintf("replay-%s-%s-%05d", kind, src.System, ordinal)
	generationID := scopeID + "-g1"
	repoID := "repo-" + scopeID
	sc := scope.IngestionScope{
		ScopeID: scopeID, SourceSystem: src.System, ScopeKind: scope.KindRepository,
		CollectorKind: src.Collector, PartitionKey: "replay",
	}
	gen := scope.ScopeGeneration{
		GenerationID: generationID, ScopeID: scopeID, ObservedAt: observed, IngestedAt: observed,
		Status: scope.GenerationStatusPending, TriggerKind: scope.TriggerKindSnapshot,
		FreshnessHint: "replay-" + generationID,
	}
	envelopes := []facts.Envelope{{
		FactID: generationID + "-repo", ScopeID: scopeID, GenerationID: generationID, FactKind: "repository",
		StableFactKey: "repository:" + repoID, ObservedAt: observed,
		Payload: map[string]any{"repo_id": repoID, "path": "/replay/" + scopeID, "name": scopeID},
	}}
	for i := 0; i < 12; i++ {
		rel := fmt.Sprintf("src/file%02d.go", i)
		envelopes = append(envelopes,
			facts.Envelope{
				FactID: fmt.Sprintf("%s-file-%02d", generationID, i), ScopeID: scopeID, GenerationID: generationID,
				FactKind: "file", StableFactKey: fmt.Sprintf("file:%s:%s", repoID, rel), ObservedAt: observed,
				Payload: map[string]any{
					"repo_id": repoID, "relative_path": rel, "language": "go",
					"parsed_file_data": map[string]any{},
				},
			},
			facts.Envelope{
				FactID: fmt.Sprintf("%s-entity-%02d", generationID, i), ScopeID: scopeID, GenerationID: generationID,
				FactKind: "content_entity", StableFactKey: fmt.Sprintf("entity:%s:%s:fn", repoID, rel), ObservedAt: observed,
				Payload: map[string]any{
					"entity_id": fmt.Sprintf("content-entity:%s:%02d", scopeID, i), "repo_id": repoID,
					"relative_path": rel, "entity_type": "Function", "entity_name": fmt.Sprintf("Fn%02d", i),
					"start_line": float64(1), "end_line": float64(9),
				},
			})
	}
	return sc, gen, envelopes
}

// commitReplayGeneration commits one generation through the production store.
func commitReplayGeneration(ctx context.Context, t *testing.T, store IngestionStore, src replaySource, kind string, ordinal int) {
	t.Helper()
	sc, gen, envelopes := replayGeneration(src, kind, ordinal, time.Now().UTC())
	stream := make(chan facts.Envelope, len(envelopes))
	for _, envelope := range envelopes {
		stream <- envelope
	}
	close(stream)
	if err := store.CommitScopeGeneration(ctx, sc, gen, stream); err != nil {
		t.Fatalf("commit %s: %v", gen.GenerationID, err)
	}
}

// TestReplaySeedProjector commits source A's seeded projector generations.
// The count must reach the projector worker count (arbiter item 7); the driver
// passes ESHU_REPLAY_A_GENERATIONS and records it in the arm manifest.
func TestReplaySeedProjector(t *testing.T) {
	database := replayDB(t)
	src := replaySourceFromEnv(t, "A")
	count := replayEnvInt(t, "ESHU_REPLAY_A_GENERATIONS", 48)
	store := NewIngestionStore(SQLDB{DB: database})
	ctx := context.Background()
	for ordinal := 1; ordinal <= count; ordinal++ {
		commitReplayGeneration(ctx, t, store, src, "a", ordinal)
	}
	t.Logf("replay seed: committed %d projector generations for source %s", count, src.System)
}

// TestReplayFeedB commits source B's fresh generations at a fixed cadence. The
// total and the cadence are fixed (not time-driven), so every arm feeds the
// same B generations, and the queue-wait comparison has the same population.
func TestReplayFeedB(t *testing.T) {
	database := replayDB(t)
	src := replaySourceFromEnv(t, "B")
	count := replayEnvInt(t, "ESHU_REPLAY_B_COUNT", 240)
	cadence := replayEnvDuration(t, "ESHU_REPLAY_B_CADENCE", 2*time.Second)
	store := NewIngestionStore(SQLDB{DB: database})
	ctx := context.Background()
	ticker := time.NewTicker(cadence)
	defer ticker.Stop()
	for ordinal := 1; ordinal <= count; ordinal++ {
		commitReplayGeneration(ctx, t, store, src, "b", ordinal)
		if ordinal < count {
			<-ticker.C
		}
	}
	t.Logf("replay feeder: committed %d generations for source %s at %s", count, src.System, cadence)
}
