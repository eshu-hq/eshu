// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package writer

import (
	"context"

	"go.opentelemetry.io/otel/metric"

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"

	"github.com/eshu-hq/eshu/go/internal/graph/edgetype"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// repositoryStubCandidateLimit caps the source/target pairs one
// multi-row stub log line names, so a large batch cannot flood the log.
const repositoryStubCandidateLimit = 10

// repositoryStubUpsertCyphers are the shared-edge upserts that MERGE a
// Repository by id (#7446). When no node holds the id -- typically one the
// path-conflict retirement removed -- the MERGE creates a path-less stub, and
// the backend's NodesCreated counts it. These statements create no other
// node label, so NodesCreated counts Repository stubs only.
var repositoryStubUpsertCyphers = func() map[string]bool {
	set := map[string]bool{
		sourcecypher.BatchCanonicalRepoDependencyUpsertCypher: true,
		sourcecypher.BatchCanonicalSubmodulePinEdgeCypher:     true,
	}
	for _, relationshipType := range []edgetype.EdgeType{
		edgetype.DeploysFrom, edgetype.DiscoversConfigIn, edgetype.ProvisionsDependencyFor,
		edgetype.UsesModule, edgetype.ReadsConfigFrom,
	} {
		if cypher, ok := sourcecypher.BatchCanonicalTypedRepoRelationshipUpsertCypher(string(relationshipType)); ok {
			set[cypher] = true
		}
	}
	return set
}()

// repositoryStubWriter returns the closed writer label for a domain whose
// upserts can MERGE-create a Repository stub, and false for every other
// domain, which then pays nothing.
func repositoryStubWriter(domain string) (string, bool) {
	switch domain {
	case reducer.DomainRepoDependency:
		return telemetry.RepositoryStubWriterRepoDependency, true
	case reducer.DomainSubmodulePinEdges:
		return telemetry.RepositoryStubWriterSubmodulePin, true
	default:
		return "", false
	}
}

func isRepositoryStubUpsertEntry(entry sourcecypher.WriteCountEntry) bool {
	return repositoryStubUpsertCyphers[entry.Cypher]
}

// captureRepositoryStubs stashes a fresh filtered write-count collector for
// one execution unit (an ExecuteGroup call or one Execute call) of a
// stub-capable domain, and returns ctx unchanged (and a nil collector)
// otherwise. The collector forwards every entry to any collector the caller
// already stashed. The #6783 differential recorder is not one: with capture
// on it wraps the executor below the EdgeWriter and stashes its own
// collector, so the stub collector sees nothing and the DEBUG line below
// fires instead of the counter.
func captureRepositoryStubs(ctx context.Context, domain string) (context.Context, *sourcecypher.WriteCountsCollector) {
	if _, ok := repositoryStubWriter(domain); !ok {
		return ctx, nil
	}
	collector := sourcecypher.NewFilteredWriteCountsCollector(
		sourcecypher.WriteCountsCollectorFromContext(ctx), isRepositoryStubUpsertEntry)
	return sourcecypher.WithWriteCountsCollector(ctx, collector), collector
}

// reportRepositoryStubs publishes the Repository stubs one committed
// execution unit created (#7446). Call it only after the unit succeeded.
//
// A driver or executor retry re-runs every statement of the unit and
// reports it again, and a failed attempt may report only a prefix, so the
// committed attempt is the last k entries, where k is the unit's stub-capable
// statement count. Summing every entry would count a rolled-back attempt.
// With no entry at all the executor reported no write summary (a test
// executor, or differential capture on): a DEBUG line says the stubs were
// not counted and no counter moves.
func (w *EdgeWriter) reportRepositoryStubs(
	ctx context.Context,
	domain string,
	evidenceSource string,
	collector *sourcecypher.WriteCountsCollector,
	stmts []sourcecypher.Statement,
) {
	writerLabel, ok := repositoryStubWriter(domain)
	if collector == nil || !ok {
		return
	}
	expected := 0
	for _, stmt := range stmts {
		if repositoryStubUpsertCyphers[stmt.Cypher] {
			expected++
		}
	}
	if expected == 0 {
		return
	}
	entries := collector.Entries()
	if len(entries) == 0 {
		if w.Logger != nil {
			w.Logger.DebugContext(ctx, "canonical repository stubs not counted: executor reported no write summary",
				"writer", writerLabel, "evidence_source", evidenceSource, "statements", expected, "stubs_counted", false)
		}
		return
	}
	if len(entries) > expected {
		entries = entries[len(entries)-expected:]
	}
	for _, entry := range entries {
		created := entry.Counters.NodesCreated
		if created <= 0 {
			continue
		}
		if w.Instruments != nil && w.Instruments.CanonicalRepositoryStubsCreated != nil {
			w.Instruments.CanonicalRepositoryStubsCreated.Add(ctx, created,
				metric.WithAttributes(telemetry.AttrWriter(writerLabel)))
		}
		if w.Logger != nil {
			w.Logger.InfoContext(ctx, "canonical repository stub created",
				append([]any{"writer", writerLabel, "evidence_source", evidenceSource, "nodes_created", created},
					repositoryStubRowFields(writerLabel, entry.Parameters)...)...)
		}
	}
}

// repositoryStubRowFields names the source and target ids behind a stub.
// NodesCreated is per statement, not per row: a one-row statement is
// attributed exactly (attributed=true); a multi-row statement lists up to
// repositoryStubCandidateLimit candidate pairs (attributed=false).
func repositoryStubRowFields(writerLabel string, params map[string]any) []any {
	rows := statementRows(params)
	sourceKey, targetKey := "repo_id", "target_repo_id"
	if writerLabel == telemetry.RepositoryStubWriterSubmodulePin {
		sourceKey, targetKey = "parent_repo_id", "resolved_repo_id"
	}
	if len(rows) == 1 {
		return []any{
			"source_repo_id", sourcecypher.PayloadString(rows[0], sourceKey),
			"target_repo_id", sourcecypher.PayloadString(rows[0], targetKey),
			"generation_id", sourcecypher.PayloadString(rows[0], "generation_id"),
			"batch_rows", 1, "attributed", true,
		}
	}
	candidates := make([]string, 0, min(len(rows), repositoryStubCandidateLimit))
	for _, row := range rows {
		if len(candidates) == repositoryStubCandidateLimit {
			break
		}
		candidates = append(candidates, sourcecypher.PayloadString(row, sourceKey)+"->"+
			sourcecypher.PayloadString(row, targetKey)+"@"+sourcecypher.PayloadString(row, "generation_id"))
	}
	return []any{"batch_rows", len(rows), "candidates", candidates, "attributed", false}
}

// statementRows returns a batched statement's $rows parameter.
func statementRows(params map[string]any) []map[string]any {
	switch rows := params["rows"].(type) {
	case []map[string]any:
		return rows
	case []any:
		out := make([]map[string]any, 0, len(rows))
		for _, row := range rows {
			if m, ok := row.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	default:
		return nil
	}
}
