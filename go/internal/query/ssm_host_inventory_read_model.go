// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"sort"
	"strconv"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/ssmhost"
)

// Closed SSM host inventory statuses. Status is always derived at read time
// from committed graph nodes overlaid with the active generation's admitted
// facts, never stored, so the graph and this surface agree by construction.
const (
	ssmHostStatusSuccessful = "successful"
	ssmHostStatusPartial    = "partial"
	ssmHostStatusStale      = "stale"
	ssmHostStatusUnknown    = "unknown"
)

// Closed status reasons. Reasons are static strings: capture times and host
// identities travel as data fields, never inside reason text.
const (
	ssmHostReasonProjected      = "complete capture projected"
	ssmHostReasonPartial        = "latest capture incomplete or incoherent"
	ssmHostReasonStale          = "committed capture past the stale-age bound"
	ssmHostReasonPending        = "newer capture admitted, projection pending"
	ssmHostReasonSilent         = "no capture in the active generation"
	ssmHostReasonTruncatedFacts = "admitted-fact window exhausted before coherence"
)

// Committed reads run as four label-anchored statements, never a bare
// MATCH with a label disjunction in WHERE (which cannot seek). Seeds page
// first (DISTINCT tuples, deterministic order, LIMIT) per label, then node
// rows follow their hosts, so a host never splits across a page. Every leg
// shares one ordering — partition, account, region, instance, byte-wise —
// which must match the Go HostKey sort below; the merged-union paging proof
// depends on all three sides (both seed legs and the admitted-fact host
// rank) agreeing on that order.
//
// Hosts page first from [0, offset+limit+1): the merged committed-plus-active
// universe pages in Go after the overlay, so the committed fetch covers the
// page prefix rather than the page itself. Deep cursors rescan that prefix;
// cursors only ever come from this read's own NextCursor, so walks stay
// linear.
const ssmCommitSeedAppsCypher = `MATCH (seed:SSMHostApplication)
WHERE ($all_scopes OR seed.scope_id IN $scope_ids)
  AND ($instance_id = '' OR seed.instance_id = $instance_id)
  AND ($account_id = '' OR seed.account_id = $account_id)
  AND ($region = '' OR seed.region = $region)
RETURN DISTINCT seed.partition AS h_partition, seed.account_id AS h_account_id, seed.region AS h_region, seed.instance_id AS h_instance_id
ORDER BY h_partition, h_account_id, h_region, h_instance_id
LIMIT $seed_limit`

// ssmCommitSeedPlatsCypher seeds the platform-only leg: hosts with platform
// observations but no application node. The read subtracts app-seeded hosts
// in Go (a correlated NOT EXISTS would gamble on NornicDB subquery support).
const ssmCommitSeedPlatsCypher = `MATCH (seed:SSMHostPlatform)
WHERE ($all_scopes OR seed.scope_id IN $scope_ids)
  AND ($instance_id = '' OR seed.instance_id = $instance_id)
  AND ($account_id = '' OR seed.account_id = $account_id)
  AND ($region = '' OR seed.region = $region)
RETURN DISTINCT seed.partition AS h_partition, seed.account_id AS h_account_id, seed.region AS h_region, seed.instance_id AS h_instance_id
ORDER BY h_partition, h_account_id, h_region, h_instance_id
LIMIT $seed_limit`

// ssmCommitNodesAppsCypher fetches one page's application nodes with their
// paired platforms on the OPTIONAL MATCH leg. One row per (app, paired
// platform): the read deduplicates applications by uid.
const ssmCommitNodesAppsCypher = `UNWIND $hosts AS h
MATCH (app:SSMHostApplication)
WHERE app.partition = h.partition
  AND app.account_id = h.account_id
  AND app.region = h.region
  AND app.instance_id = h.instance_id
  AND ($all_scopes OR app.scope_id IN $scope_ids)
OPTIONAL MATCH (app)-[rel:REPORTED_WITH]->(plat:SSMHostPlatform)
RETURN app AS app, plat AS plat`

// ssmCommitNodesPlatsCypher fetches one page's platform nodes, including the
// platform-only hosts the application leg cannot return. Same-host platforms
// merge by uid with the paired leg; the remainder become platform-only
// committed hosts.
const ssmCommitNodesPlatsCypher = `UNWIND $hosts AS h
MATCH (plat:SSMHostPlatform)
WHERE plat.partition = h.partition
  AND plat.account_id = h.account_id
  AND plat.region = h.region
  AND plat.instance_id = h.instance_id
  AND ($all_scopes OR plat.scope_id IN $scope_ids)
RETURN plat AS plat_only`

// ssmHostView is one host's served inventory: the committed observations
// with the read-time status overlay.
type ssmHostView struct {
	Partition             string           `json:"partition"`
	AccountID             string           `json:"account_id"`
	Region                string           `json:"region"`
	InstanceID            string           `json:"instance_id"`
	Status                string           `json:"status"`
	StatusReason          string           `json:"status_reason"`
	CaptureTime           string           `json:"capture_time"`
	ScopeID               string           `json:"scope_id"`
	GenerationID          string           `json:"generation_id"`
	Applications          []map[string]any `json:"applications"`
	ApplicationsTruncated bool             `json:"applications_truncated"`
	Platforms             []map[string]any `json:"platforms"`
	FactsTruncated        bool             `json:"facts_truncated"`
	MalformedFacts        int              `json:"malformed_facts"`
}

// ssmHostInventoryReadModel is the paged host inventory read. MalformedFacts
// counts admitted facts the read skipped as undecodable; the reducer already
// quarantined each of them at projection time, and the read never fails for
// a malformed sibling.
type ssmHostInventoryReadModel struct {
	Hosts          []ssmHostView `json:"hosts"`
	MalformedFacts int           `json:"malformed_facts"`
	NextCursor     string        `json:"next_cursor"`
}

// ReadSSMHostInventory serves one SSM host inventory page: committed
// observation nodes from the graph overlaid with the active generation's
// admitted facts assessed through the same verdicts the reducer projects
// with. A host appears when committed nodes or active facts exist for it;
// anything else is absent, never an empty host.
//
// Hosts are the page unit and the page slices the merged committed-plus-active
// universe: the limit binds active-only hosts exactly like committed ones,
// and NextCursor derives from the merged length, never from one side alone.
// Status filters apply after paging in Go (status needs the admitted-fact
// overlay), so a status-filtered page may be short.
//
// Paging exactness: with K = offset+limit+1, the merged universe's first K
// hosts are contained in the union of each side's first K (a merged-prefix
// member has fewer than K predecessors on its own side), and both sides are
// fetched prefix-first in the same tuple order — so the Go-side prefix,
// slice, and has-more signal are exact.
func ReadSSMHostInventory(
	ctx context.Context,
	graph GraphQuery,
	store ssmHostFactsStore,
	filter ssmHostInventoryFilter,
	bounds ssmhost.Bounds,
	now time.Time,
) (ssmHostInventoryReadModel, error) {
	if err := bounds.Validate(); err != nil {
		return ssmHostInventoryReadModel{}, err
	}
	offset, limit, prefix, ok := ssmHostPageBounds(filter)
	if !ok {
		// Overflowed cursor: astronomically beyond any universe, served
		// as a beyond-the-end empty page like the old SKIP-beyond-end.
		return ssmHostInventoryReadModel{}, nil
	}
	seeded, err := readSSMCommittedSeeds(ctx, graph, filter, prefix)
	if err != nil {
		return ssmHostInventoryReadModel{}, err
	}
	active, err := store.SSMHostActiveFacts(ctx, filter)
	if err != nil {
		return ssmHostInventoryReadModel{}, err
	}
	observations, signals := ssmObservationsFromFacts(active)
	projections, _ := ssmhost.ProjectHosts(observations.apps, observations.platforms, nil, bounds, now)
	verdicts := make(map[string]ssmhost.HostProjection, len(projections))
	for _, projection := range projections {
		verdicts[ssmhost.HostKey(projection.Host)] = projection
	}
	merged := mergeSSMHostKeys(seeded, verdicts, prefix)
	model := ssmHostInventoryReadModel{MalformedFacts: malformedTotal(signals)}
	if len(merged) == prefix {
		model.NextCursor = strconv.Itoa(offset + limit)
	}
	page := merged
	if offset < len(page) {
		page = page[offset:]
	} else {
		page = nil
	}
	if len(page) > limit {
		page = page[:limit]
	}
	committed, err := readSSMCommittedNodes(ctx, graph, filter, page)
	if err != nil {
		return ssmHostInventoryReadModel{}, err
	}
	model.Hosts = assembleSSMHostViews(committed, verdicts, signals, page, filter, bounds, now)
	return model, nil
}

// ssmHostPageBounds resolves the clamped paging triple shared by the
// committed seeds, the admitted-fact host-rank cap, and the Go-side merge:
// the offset (negative cursors clamp to zero; the handler already rejects
// them), the limit (defaulted when unset), and the page-prefix host budget
// offset+limit+1. ok=false means the prefix overflowed: the cursor sits
// beyond any servable universe. SQL and Go derive the identical budget here
// so the merged-union page stays exact.
func ssmHostPageBounds(filter ssmHostInventoryFilter) (offset, limit, prefix int, ok bool) {
	offset = filter.Offset
	if offset < 0 {
		offset = 0
	}
	limit = filter.Limit
	if limit <= 0 {
		limit = ssmHostInventoryDefaultLimit
	}
	prefix = offset + limit + 1
	if prefix <= offset {
		return 0, 0, 0, false
	}
	return offset, limit, prefix, true
}

// mergeSSMHostKeys unions the committed seed keys with the verdict keys in
// HostKey order, truncated to the page prefix. Both inputs arrive
// prefix-first in that same order, so the truncation keeps exactly the
// merged universe's first prefix hosts.
func mergeSSMHostKeys(seeded []string, verdicts map[string]ssmhost.HostProjection, prefix int) []string {
	seen := make(map[string]bool, len(seeded)+len(verdicts))
	merged := make([]string, 0, len(seeded)+len(verdicts))
	for _, key := range seeded {
		if !seen[key] {
			seen[key] = true
			merged = append(merged, key)
		}
	}
	for key := range verdicts {
		if !seen[key] {
			seen[key] = true
			merged = append(merged, key)
		}
	}
	sort.Strings(merged)
	if len(merged) > prefix {
		merged = merged[:prefix]
	}
	return merged
}

func malformedTotal(signals ssmHostReadSignals) int {
	total := 0
	for _, count := range signals.malformedByHost {
		total += count
	}
	return total
}
