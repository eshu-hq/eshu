// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package worker

import (
	"context"
	"fmt"
	"sort"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/gpphase"
	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
)

// sharedProjectionReadinessKeyForRow builds the readiness lookup key for one
// intent row. For the symbol→runtime domains (handles_route, runs_in) it uses
// the deterministic per-repo key (#2891) so the code-stage intent finds the
// workload-stage phase row across the source-run boundary. For every other
// domain it falls back to the intent-derived key (GraphProjectionPhaseKeyForRow),
// keeping code_calls and the semantic edge domains byte-identical to their
// pre-#2891 behavior.
func sharedProjectionReadinessKeyForRow(
	domain string,
	row sharedintent.Row,
	keyspace gpphase.Keyspace,
) (gpphase.PhaseKey, bool) {
	if domain == reducercontract.DomainHandlesRoute || domain == reducercontract.DomainRunsIn {
		repoID := sharedintent.RowRepoID(row)
		key := gpphase.WorkloadMaterializationRepoReadinessKey(row.ScopeID, repoID, row.GenerationID)
		if err := key.Validate(); err != nil {
			return gpphase.PhaseKey{}, false
		}
		return key, true
	}
	return GraphProjectionPhaseKeyForRow(row, row.GenerationID, keyspace)
}

// FilterRowsByReadiness partitions a domain's pending intent rows into three
// disjoint sets: rows ready to project, rows still blocked on a prerequisite
// graph-projection phase (deferred, re-enqueued), and rows that are terminally
// complete with no edge (drained without a write). Domains without a readiness
// gate pass through as ready. The readiness key is built under the domain's
// prerequisite keyspace (ReadinessKeyspace) so a multi-keyspace
// domain such as handles_route looks up the phase under the keyspace it was
// published in.
//
// endpointPresence adds a SECOND, domain-scoped gate that applies ONLY to
// DomainHandlesRoute (#2809). A handles_route intent carries the repo acceptance
// unit. The repo's single workload-materialization invocation publishes BOTH the
// workload-materialization phase under that repo acceptance unit AND the
// property-keyed (repo_id, path) presence for every :Endpoint it commits, before
// it returns — including the zero-candidate path, which publishes the phase with
// no endpoints. So once a handles_route row passes the phase gate, every endpoint
// that repo will ever produce in this generation already has a presence row.
// Therefore, among phase-ready rows:
//   - an endpoint that is PRESENT projects (readyRows);
//   - an endpoint that is ABSENT will never commit (route-only repo, or a route
//     whose endpoint was not materialized), so the row is TERMINAL — drained with
//     no edge (terminalRows), never deferred. Deferring it would stall the
//     shared-projection backlog forever because no producer can fill the key.
//
// A nil endpointPresence disables this second gate, so every other domain — and
// handles_route itself when presence is unwired — stays byte-identical to its
// pre-#2809 behavior.
func FilterRowsByReadiness(
	ctx context.Context,
	domain string,
	rows []sharedintent.Row,
	readinessLookup gpphase.ReadinessLookup,
	readinessPrefetch gpphase.ReadinessPrefetch,
	endpointPresence gpphase.EndpointPresenceLookup,
) (readyRows, blockedRows, terminalRows []sharedintent.Row, err error) {
	phase, gated := ReadinessPhase(domain)
	if !gated || len(rows) == 0 {
		return rows, nil, nil, nil
	}
	keyspace := ReadinessKeyspace(domain)

	lookup := readinessLookup
	if readinessPrefetch != nil {
		seen := make(map[gpphase.PhaseKey]struct{}, len(rows))
		keys := make([]gpphase.PhaseKey, 0, len(rows))
		for _, row := range rows {
			key, ok := sharedProjectionReadinessKeyForRow(domain, row, keyspace)
			if !ok {
				continue
			}
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			keys = append(keys, key)
		}
		resolvedLookup, prefetchErr := readinessPrefetch(ctx, keys, phase)
		if prefetchErr != nil {
			return nil, nil, nil, fmt.Errorf("prefetch graph projection readiness: %w", prefetchErr)
		}
		lookup = resolvedLookup
	}

	if lookup == nil {
		return rows, nil, nil, nil
	}

	readyRows = make([]sharedintent.Row, 0, len(rows))
	blockedRows = make([]sharedintent.Row, 0)
	for _, row := range rows {
		key, ok := sharedProjectionReadinessKeyForRow(domain, row, keyspace)
		if !ok {
			continue
		}
		ready, found := lookup(key, phase)
		if !found || !ready {
			blockedRows = append(blockedRows, row)
			continue
		}
		readyRows = append(readyRows, row)
	}

	// Second gate (symbol→runtime domains, #2809/#2855): among the phase-ready
	// rows, terminally drain the ones whose runtime target did not commit —
	// handles_route on its (repo_id, path) :Endpoint, runs_in on its repo's
	// :Workload presence. Because the phase gate already proves workload
	// materialization for the repo is done, an absent target will never appear, so
	// the row is complete with no edge — NOT deferred. A nil presence lookup is a
	// no-op, so every other domain — and these domains when presence is unwired —
	// is unaffected.
	if keyspace, keyFor, gated := symbolRuntimePresenceGate(domain); gated && endpointPresence != nil && len(readyRows) > 0 {
		// The per-repo refresh intent (#2898) carries no endpoint of its own — it
		// exists only to issue the single repo-wide retract — so exempt it from the
		// presence gate. Subjecting it would key on an empty path and drain it as
		// terminal-no-endpoint, so the repo-wide retract would never run.
		refreshReady, edgeReady := sharedintent.SplitRepoRefreshRows(readyRows)
		presentRows, absentRows, presenceErr := filterRowsByTargetPresence(ctx, edgeReady, endpointPresence, keyspace, keyFor)
		if presenceErr != nil {
			return nil, nil, nil, fmt.Errorf("look up %s target presence: %w", domain, presenceErr)
		}
		readyRows = append(refreshReady, presentRows...)
		terminalRows = absentRows
	}

	return readyRows, blockedRows, terminalRows, nil
}

// symbolRuntimePresenceGate reports the presence keyspace and per-row key
// function for the symbol→runtime shared-projection domains whose edge targets
// commit in the workload-materialization domain under a different acceptance unit
// (#2809 handles_route, #2855 runs_in). For every other domain it returns
// gated=false, so the second presence gate is skipped and the domain stays
// byte-identical to its phase-gate-only behavior.
func symbolRuntimePresenceGate(domain string) (gpphase.Keyspace, func(sharedintent.Row) string, bool) {
	switch domain {
	case reducercontract.DomainHandlesRoute:
		return gpphase.KeyspaceAPIEndpointRepoPath, gpphase.HandlesRouteEndpointPresenceKey, true
	case reducercontract.DomainRunsIn:
		return gpphase.KeyspaceRepoWorkloadPresence, gpphase.RunsInRepoWorkloadPresenceKey, true
	default:
		return "", nil, false
	}
}

// filterRowsByTargetPresence splits phase-ready symbol→runtime rows into the rows
// whose target is committed (present) and the rows whose target is absent. It
// backs both the handles_route endpoint-presence gate (#2809) and the runs_in
// repo-workload-presence gate (#2855): the caller supplies the presence keyspace
// and a per-row key function so the same bounded MissingUIDs lookup (ONE call
// over the distinct synthesized uids, never an N+1 per-row probe) serves either
// domain. A nil lookup disables the gate and returns every input row as present,
// so the gated path stays byte-identical to its pre-gate behavior when presence
// is unwired. The caller treats the absent set as TERMINAL (complete, no edge),
// not deferred: the phase gate already proves the repo's targets have all
// committed, so an absent target will never appear (moved here from the reducer
// root's endpoint_repo_path_presence.go, issue #6061).
func filterRowsByTargetPresence(
	ctx context.Context,
	rows []sharedintent.Row,
	presence gpphase.EndpointPresenceLookup,
	keyspace gpphase.Keyspace,
	keyFor func(sharedintent.Row) string,
) (present, absent []sharedintent.Row, err error) {
	if presence == nil || len(rows) == 0 {
		return rows, nil, nil
	}

	keyByRow := make([]string, len(rows))
	seen := make(map[string]struct{}, len(rows))
	uids := make([]string, 0, len(rows))
	for i, row := range rows {
		key := keyFor(row)
		keyByRow[i] = key
		if key == "" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		uids = append(uids, key)
	}
	sort.Strings(uids)

	missing, err := presence.MissingUIDs(ctx, keyspace, uids)
	if err != nil {
		return nil, nil, err
	}
	missingSet := make(map[string]struct{}, len(missing))
	for _, uid := range missing {
		missingSet[uid] = struct{}{}
	}

	present = make([]sharedintent.Row, 0, len(rows))
	absent = make([]sharedintent.Row, 0)
	for i, row := range rows {
		key := keyByRow[i]
		// A row with no derivable (repo_id, path) cannot be proven present and
		// cannot anchor a MERGE either, so it joins the absent (terminal) set.
		if key == "" {
			absent = append(absent, row)
			continue
		}
		if _, isMissing := missingSet[key]; isMissing {
			absent = append(absent, row)
			continue
		}
		present = append(present, row)
	}
	return present, absent, nil
}

// MaxIntentWaitSeconds reports the longest time any row has waited
// since it was created, used for shared-projection latency telemetry (moved
// here from the reducer root's maxSharedIntentWaitSeconds, issue #6061). The
// code-call projection runner (still at the reducer root) also calls it.
func MaxIntentWaitSeconds(now time.Time, rows []sharedintent.Row) float64 {
	var maxWait float64
	for _, row := range rows {
		if row.CreatedAt.IsZero() {
			continue
		}
		wait := now.Sub(row.CreatedAt).Seconds()
		if wait < 0 {
			wait = 0
		}
		if wait > maxWait {
			maxWait = wait
		}
	}
	return maxWait
}
