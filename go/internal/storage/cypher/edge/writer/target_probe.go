// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package writer

import (
	"context"

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// targetMissProbeDomains are the shared-projection domains whose batch write
// resolves a cross-acceptance-unit runtime target committed by a different
// handler, and whose rows therefore reach WriteEdges only after a presence
// gate proved the target committable. A miss for these rows is always worth
// a retry, never a silent completion.
var targetMissProbeDomains = map[string]bool{
	reducer.DomainHandlesRoute:        true,
	reducer.DomainRunsIn:              true,
	reducer.DomainDeployableUnitEdges: true,
}

// buildTargetPresenceProbeStatements returns one existence probe per distinct
// runtime target of a routed batch, or false for domains outside the guard.
// Every statement is a single MATCH clause over literal parameters with
// RETURN 1 LIMIT 1, so the ProbeExecutor boolean is the verdict directly:
// found means that target exists, not-found means it is absent and the batch
// must be deferred. The caller ANDs the verdicts: the first miss fails the
// batch.
//
// Shape discipline (docs/public/reference/nornicdb-query-pitfalls.md): one
// single-clause statement per target, no WITH, no OPTIONAL MATCH, no
// subquery, no UNWIND, no comma-separated pattern. The previous shape folded
// every distinct target into one statement as sequential anchored MATCH
// clauses; on the pinned NornicDB that two-clause shape answers present when
// only the first anchor exists (#8315: source Repository present, deployment
// Repository absent still returned a row, while the same shape on Neo4j and
// the single-clause shape on both backends answer correctly). A
// comma-separated pattern is not a fix — the backend treats it like two
// clauses. Secondary targets are therefore joined in Go, one cheap indexed
// point lookup each, mirroring the post-write verifier's single-pattern
// probes (deployable_unit_verify.go). Correlated COUNT{}/EXISTS{} subqueries
// over UNWIND rows are rejected by the backend, and a WHERE attached to a
// WITH is not evaluated as a filter, so neither can carry this verdict. The
// probe shape is proven live in both directions on both backends (see the
// #6184 run notes and the #8315 live mixed-presence tests); the unit tests
// pin the builder output.
func buildTargetPresenceProbeStatements(domain string, rows []map[string]any) ([]sourcecypher.Statement, bool) {
	if !targetMissProbeDomains[domain] {
		return nil, false
	}
	var stmts []sourcecypher.Statement
	seen := make(map[string]struct{}, len(rows))
	emit := func(key string, match string, kv ...any) {
		if _, dup := seen[key]; dup {
			return
		}
		seen[key] = struct{}{}
		// Each statement carries only its own target's parameters, so the
		// names stay short and never collide across statements.
		params := make(map[string]any, len(kv)/2)
		for i := 0; i < len(kv); i += 2 {
			params[kv[i].(string)] = kv[i+1]
		}
		stmts = append(stmts, sourcecypher.Statement{
			Operation:  sourcecypher.OperationCanonicalProbe,
			Cypher:     "MATCH " + match + "\nRETURN 1 LIMIT 1",
			Parameters: params,
		})
	}
	switch domain {
	case reducer.DomainHandlesRoute:
		// The write MATCHes both (f:Function {uid}) and (e:Endpoint
		// {repo_id, path}); the probe anchors both, or a batch whose
		// Function node is absent (post-wipe ordering vs content
		// projection) passes the guard and completes a silent zero-edge
		// write (#6730 owner finding). Each anchor is its own statement:
		// the two anchors in one statement are the exact two-clause shape
		// that misreports on NornicDB (#8315).
		for _, row := range rows {
			repo, _ := row["repo_id"].(string)
			path, _ := row["path"].(string)
			if repo == "" || path == "" {
				continue
			}
			emit(repo+"\x00"+path,
				"(e:Endpoint {repo_id: $repo_id, path: $path})",
				"repo_id", repo, "path", path)
			if uid, _ := row["function_entity_id"].(string); uid != "" {
				emit("f\x00"+uid,
					"(f:Function {uid: $uid})",
					"uid", uid)
			}
		}
	case reducer.DomainRunsIn:
		for _, row := range rows {
			repo, _ := row["repo_id"].(string)
			if repo == "" {
				continue
			}
			emit(repo,
				"(:Repository {id: $repo_id})-[:DEFINES]->(:Workload)",
				"repo_id", repo)
		}
	case reducer.DomainDeployableUnitEdges:
		// The deployable-unit template MATCHes both endpoint Repositories;
		// the deployment node is committed by another scope's
		// materialization with no happens-before against this batch (#6184).
		// The source-present/target-absent mix is the #8315 CI failure: the
		// old two-anchor statement answered present and the write then
		// matched nothing while reporting success.
		for _, row := range rows {
			if repo, _ := row["repo_id"].(string); repo != "" {
				emit(repo,
					"(:Repository {id: $repo_id})",
					"repo_id", repo)
			}
			if repo, _ := row["deployment_repo_id"].(string); repo != "" {
				emit(repo,
					"(:Repository {id: $repo_id})",
					"repo_id", repo)
			}
		}
	default:
		return nil, false
	}
	if len(stmts) == 0 {
		return nil, false
	}
	return stmts, true
}

// checkBatchTargetsPresent probes one routed batch for runtime-target
// completeness before its write statements run. It returns nil when the
// domain is unguarded, the executor has no probe capability, or every target
// is present. A probe failure or a detected miss emits the operator signal
// and returns a retryable error; the caller must run no write statement for
// the batch afterwards.
func (w *EdgeWriter) checkBatchTargetsPresent(
	ctx context.Context,
	prober sourcecypher.ProbeExecutor,
	domain string,
	evidenceSource string,
	rows []map[string]any,
	sampleRepoID string,
	sampleIntentID string,
) error {
	probes, ok := buildTargetPresenceProbeStatements(domain, rows)
	if !ok {
		return nil
	}
	for _, probe := range probes {
		targetPresent, err := prober.ExecuteProbe(ctx, probe)
		if err != nil {
			if w.Logger != nil {
				w.Logger.Warn(
					"shared edge target probe failed, deferring unverified batch",
					"domain", domain,
					"evidence_source", evidenceSource,
					"batch_rows", len(rows),
					"sample_intent_id", sampleIntentID,
					"error", err,
				)
			}
			return &targetProbeError{
				domain:         domain,
				batchRows:      len(rows),
				sampleIntentID: sampleIntentID,
				err:            err,
			}
		}
		if targetPresent {
			continue
		}
		if w.Instruments != nil && w.Instruments.SharedEdgeTargetMiss != nil {
			w.Instruments.SharedEdgeTargetMiss.Add(ctx, 1, metric.WithAttributes(
				telemetry.AttrDomain(domain),
			))
		}
		if w.Logger != nil {
			w.Logger.Warn(
				"shared edge batch target absent, deferring batch",
				"domain", domain,
				"evidence_source", evidenceSource,
				"batch_rows", len(rows),
				"sample_repo_id", sampleRepoID,
				"sample_intent_id", sampleIntentID,
			)
		}
		return &targetMissingError{
			domain:         domain,
			batchRows:      len(rows),
			sampleRepoID:   sampleRepoID,
			sampleIntentID: sampleIntentID,
		}
	}
	return nil
}
