// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materialization

import (
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/code/call/shared"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
)

// RunsInEvidenceSource labels RUNS_IN edges so retraction and re-projection only
// ever touch edges this emitter owns.
const RunsInEvidenceSource = "reducer/runs-in"

// runsInAmbiguousConfidence is the honest confidence for every RUNS_IN edge.
//
// The code-call materialization stage that builds these intents loads only
// repository and file facts; it never runs the workload admission/correlation
// that determines how many Workload nodes a Repository ultimately DEFINES (that
// happens in a separate workload-materialization handler). Because the stage
// cannot prove a repo defines exactly one Workload, it never asserts an exact
// single binding: every edge is a candidate member of the repo's workload set
// and carries ambiguous=true with this deliberately conservative confidence.
const runsInAmbiguousConfidence = 0.5

// buildRunsInIntentRows binds proven route-handler Functions to the deployed
// runtime they run in by emitting one ordering-safe shared-projection intent per
// (handler Function uid, repo) (#2722).
//
// It scopes to exactly the same entrypoint Functions handles_route resolves: for
// every framework route_entry it reuses resolveHandlesRouteFunction, so a binding
// is produced only for an exact, unambiguous handler resolution and never for a
// guessed or every-Function-in-the-repo binding. The edge target is resolved at
// graph-MATCH time through the Repository the handler belongs to: the Cypher fans
// out to every Workload that Repository DEFINES.
//
// Ambiguity is "represented, not collapsed". The intent stage cannot count a
// repo's materialized Workloads (admission/correlation runs in a different
// handler over different facts), so it conservatively marks every edge
// ambiguous=true: the edge is a candidate, never an asserted single-workload
// truth. A consumer that wants exactness derives it by counting the MATCH
// fan-out at query time — a repo that DEFINES exactly one Workload yields exactly
// one edge.
func buildRunsInIntentRows(
	envelopes []facts.Envelope,
	index shared.EntityIndex,
	contextByRepoID map[string]sharedintent.ProjectionContext,
	createdAt time.Time,
	evidenceSource string,
) []sharedintent.Row {
	if len(envelopes) == 0 || len(contextByRepoID) == 0 {
		return nil
	}
	if evidenceSource == "" {
		evidenceSource = RunsInEvidenceSource
	}

	intents := make([]sharedintent.Row, 0)
	// seen dedupes by (functionID, repositoryID): a handler that serves several
	// routes binds to its runtime exactly once.
	seen := make(map[string]struct{})
	for _, env := range envelopes {
		if env.FactKind != factload.FactKindFile {
			continue
		}
		repositoryID := payloadcore.PayloadStr(env.Payload, "repo_id")
		if repositoryID == "" {
			continue
		}
		context, ok := contextByRepoID[repositoryID]
		if !ok {
			continue
		}
		fileData, ok := env.Payload["parsed_file_data"].(map[string]any)
		if !ok {
			continue
		}
		relativePath := payloadcore.PayloadStr(env.Payload, "relative_path")
		rawPath := payloadcore.AnyToString(fileData["path"])
		pathKeys := shared.PathKeys(rawPath, relativePath)

		for _, entry := range handlesRouteEntries(fileData) {
			handler := strings.TrimSpace(payloadcore.AnyToString(entry["handler"]))
			routePath := strings.TrimSpace(payloadcore.AnyToString(entry["path"]))
			if handler == "" || routePath == "" {
				continue
			}
			framework := strings.TrimSpace(payloadcore.AnyToString(entry["framework"]))
			functionID, method := resolveHandlesRouteFunction(index, repositoryID, pathKeys, framework, handler)
			if functionID == "" {
				continue
			}
			dedupeKey := functionID + "\x00" + repositoryID
			if _, exists := seen[dedupeKey]; exists {
				continue
			}
			seen[dedupeKey] = struct{}{}

			payload := map[string]any{
				"function_id":       functionID,
				"repo_id":           repositoryID,
				"relative_path":     relativePath,
				"evidence_source":   evidenceSource,
				"resolution_method": method,
				"confidence":        runsInAmbiguousConfidence,
				"ambiguous":         true,
			}

			intents = append(intents, sharedintent.Build(sharedintent.Input{
				ProjectionDomain: reducercontract.DomainRunsIn,
				PartitionKey:     functionID + "->" + repositoryID,
				ScopeID:          context.ScopeID,
				AcceptanceUnitID: context.ResolveAcceptanceUnitID(repositoryID),
				RepositoryID:     repositoryID,
				SourceRunID:      context.SourceRunID,
				GenerationID:     context.GenerationID,
				Payload:          payload,
				CreatedAt:        createdAt,
			}))
		}
	}

	sort.SliceStable(intents, func(i, j int) bool {
		if intents[i].RepositoryID != intents[j].RepositoryID {
			return intents[i].RepositoryID < intents[j].RepositoryID
		}
		return intents[i].IntentID < intents[j].IntentID
	})
	return intents
}
