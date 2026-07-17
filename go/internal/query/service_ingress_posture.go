// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"sort"
	"strconv"
	"strings"
)

// Ingress posture truth states. They are deliberately three-valued so the
// console never collapses "no protection", "no evidence", and "collector absent"
// into one tile. The distinction matters:
//   - observed-positive (protected/terminated): the graph has a WAF/ACM edge.
//   - observed-negative (unprotected/not_terminated): the graph ran the
//     collector for that resource but found no protection edge.
//   - unresolved (unproven): either no internet-facing edge resource is
//     materialized, OR the AWS collector slice for a known resource has not yet
//     been collected, so no evidence either way exists.
const (
	// ingressPostureProtected means an internet-facing edge resource carries a
	// materialized WAF web-ACL edge (observed positive).
	ingressPostureProtected = "protected"
	// ingressPostureUnprotected means an internet-facing edge resource exists in
	// the graph AND was returned by the collector, but carries no WAF web-ACL
	// edge (observed negative).
	ingressPostureUnprotected = "unprotected"
	// ingressPostureTerminated means an internet-facing edge resource carries a
	// materialized ACM certificate edge (observed positive).
	ingressPostureTerminated = "terminated"
	// ingressPostureNotTerminated means an internet-facing edge resource exists
	// AND was returned by the collector, but carries no ACM certificate edge
	// (observed negative).
	ingressPostureNotTerminated = "not_terminated"
	// ingressPostureUnproven means either no internet-facing edge resource is
	// materialized for this service, or the AWS collector slice for a known
	// resource has not yet been collected. Neither WAF nor TLS posture can be
	// proven or disproven (unresolved — absence of collector ≠ absence of
	// protection).
	ingressPostureUnproven = "unproven"
)

// ingressEdgeResourceTypes is the closed set of CloudResource resource_type /
// kind values that can front internet traffic and can carry a WAF web-ACL or an
// ACM certificate edge. A resource outside this set is not treated as an ingress
// edge for posture purposes, so a private VPC resource never inflates the tile.
var ingressEdgeResourceTypes = map[string]struct{}{
	"aws_cloudfront_distribution": {},
	"aws_elbv2_load_balancer":     {},
	"aws_elb_load_balancer":       {},
	"aws_apigateway_rest_api":     {},
	"aws_apigateway_stage":        {},
	"aws_apigateway_domain_name":  {},
	"aws_apigatewayv2_api":        {},
}

// ingressEdgeResource is one internet-facing edge resource considered for WAF/TLS
// posture, keyed by its stable graph identity.
type ingressEdgeResource struct {
	// id is the resource's stable graph identity.
	id string
	// name is the resource's display name.
	name string
	// resourceType is the AWS resource type (e.g. aws_elbv2_load_balancer).
	resourceType string
}

// ingressEdgeProtection records, per edge-resource id, whether a WAF web-ACL
// and/or an ACM certificate edge is materialized in the graph, and whether the
// AWS collector slice for this resource has been collected at all.
type ingressEdgeProtection struct {
	// collectorPresent is true when the graph query returned a row for this
	// resource id. If false, the AWS collector slice for this resource has not
	// yet been collected, so the absence of a protection edge is ambiguous —
	// the posture is unproven, not observed-negative.
	collectorPresent bool
	// wafProtected is true when an AWS_wafv2_web_acl_protects_resource edge
	// terminates on the edge resource. Only meaningful when collectorPresent is
	// true.
	wafProtected bool
	// tlsTerminated is true when an AWS_acm_certificate_used_by_resource edge
	// terminates on the edge resource. Only meaningful when collectorPresent is
	// true.
	tlsTerminated bool
}

// edgeResourcesFromCloudResources extracts the internet-facing edge resources
// from a service's materialized cloud_resources, de-duplicated by id and sorted
// for stable output. It never invents a resource: only rows whose resource_type
// or kind is in the closed ingress edge set are returned.
func edgeResourcesFromCloudResources(cloudResources []map[string]any) []ingressEdgeResource {
	seen := map[string]struct{}{}
	edges := make([]ingressEdgeResource, 0, len(cloudResources))
	for _, resource := range cloudResources {
		resourceType := firstNonEmptyString(
			StringVal(resource, "resource_type"),
			StringVal(resource, "kind"),
		)
		if _, ok := ingressEdgeResourceTypes[resourceType]; !ok {
			continue
		}
		id := firstNonEmptyString(
			StringVal(resource, "id"),
			StringVal(resource, "resource_id"),
			StringVal(resource, "arn"),
			StringVal(resource, "name"),
		)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		edges = append(edges, ingressEdgeResource{
			id:           id,
			name:         StringVal(resource, "name"),
			resourceType: resourceType,
		})
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].name != edges[j].name {
			return edges[i].name < edges[j].name
		}
		return edges[i].id < edges[j].id
	})
	return edges
}

// buildIngressPosture assembles the honest WAF/TLS posture block for a service's
// internet-facing edge resources. It is a pure function: the caller supplies the
// edge resources and the per-resource protection map loaded from the graph.
//
// Truth mapping:
//   - no edge resources → unproven (nothing to observe)
//   - edge resource present but collectorPresent=false → unproven per edge
//     (the AWS collector slice for that resource has not yet been collected;
//     absence of collector ≠ absence of protection)
//   - edge resource present, collectorPresent=true, flag=false → observed-negative
//   - edge resource present, collectorPresent=true, flag=true → observed-positive
//
// The rolled-up tile is positive only when every edge is observed-positive.
// Any unproven edge rolls the tile to unproven. Any observed-negative edge (with
// all others observed or positive) rolls the tile to the negative state. This
// ordering (unproven beats negative) prevents a partially-collected stack from
// appearing fully protected when collector slices are still pending.
func buildIngressPosture(
	edges []ingressEdgeResource,
	protection map[string]ingressEdgeProtection,
) map[string]any {
	if len(edges) == 0 {
		return map[string]any{
			"waf_coverage":    ingressPostureUnproven,
			"tls_termination": ingressPostureUnproven,
			"edge_count":      0,
			"waf_protected":   0,
			"tls_terminated":  0,
			"truth_basis":     "observed",
			"reason":          "no internet-facing edge resource is materialized for this service; WAF and TLS posture cannot be proven or disproven",
		}
	}

	wafProtected := 0
	tlsTerminated := 0
	wafUnproven := 0
	tlsUnproven := 0
	edgeRows := make([]map[string]any, 0, len(edges))
	for _, edge := range edges {
		p := protection[edge.id]
		wafState := edgeWAFState(p)
		tlsState := edgeTLSState(p)
		switch wafState {
		case ingressPostureProtected:
			wafProtected++
		case ingressPostureUnproven:
			wafUnproven++
		}
		switch tlsState {
		case ingressPostureTerminated:
			tlsTerminated++
		case ingressPostureUnproven:
			tlsUnproven++
		}
		edgeRows = append(edgeRows, map[string]any{
			"id":              edge.id,
			"name":            edge.name,
			"resource_type":   edge.resourceType,
			"waf_coverage":    wafState,
			"tls_termination": tlsState,
		})
	}

	return map[string]any{
		"waf_coverage":    rollupState(wafProtected, wafUnproven, len(edges), ingressPostureProtected, ingressPostureUnprotected),
		"tls_termination": rollupState(tlsTerminated, tlsUnproven, len(edges), ingressPostureTerminated, ingressPostureNotTerminated),
		"edge_count":      len(edges),
		"waf_protected":   wafProtected,
		"tls_terminated":  tlsTerminated,
		"truth_basis":     "observed",
		"edges":           edgeRows,
		"reason":          ingressPostureReason(wafProtected, wafUnproven, tlsTerminated, tlsUnproven, len(edges)),
	}
}

// edgeWAFState maps a single edge's WAF observation to the three-valued state
// vocab. If the AWS collector slice has not been collected for the resource
// (collectorPresent=false), the result is unproven — absence of collector is
// not the same as absence of WAF protection.
func edgeWAFState(p ingressEdgeProtection) string {
	if !p.collectorPresent {
		return ingressPostureUnproven
	}
	if p.wafProtected {
		return ingressPostureProtected
	}
	return ingressPostureUnprotected
}

// edgeTLSState maps a single edge's TLS observation to the three-valued state
// vocab. If the AWS collector slice has not been collected for the resource
// (collectorPresent=false), the result is unproven — absence of collector is
// not the same as absence of TLS termination.
func edgeTLSState(p ingressEdgeProtection) string {
	if !p.collectorPresent {
		return ingressPostureUnproven
	}
	if p.tlsTerminated {
		return ingressPostureTerminated
	}
	return ingressPostureNotTerminated
}

// rollupState rolls per-edge three-valued observations into one tile state.
// Ordering: all-positive → positive; any unproven → unproven (collector absent
// beats observed-negative so a partially-collected stack stays honest); any
// negative → negative. This ordering prevents a mix of collected-negative and
// uncollected edges from appearing fully protected.
func rollupState(positive, unproven, total int, positiveState, negativeState string) string {
	if positive >= total && total > 0 {
		return positiveState
	}
	if unproven > 0 {
		return ingressPostureUnproven
	}
	return negativeState
}

// ingressPostureReason explains the rolled-up posture for an operator,
// distinguishing between collector-absent resources and observed-negative ones.
func ingressPostureReason(wafProtected, wafUnproven, tlsTerminated, tlsUnproven, total int) string {
	var b strings.Builder
	b.WriteString("observed across ")
	b.WriteString(plural(total, "internet-facing edge resource", "internet-facing edge resources"))
	b.WriteString(": ")
	b.WriteString(plural(wafProtected, "WAF web-ACL edge", "WAF web-ACL edges"))
	if wafUnproven > 0 {
		b.WriteString(" (")
		b.WriteString(plural(wafUnproven, "resource", "resources"))
		b.WriteString(" collector-absent)")
	}
	b.WriteString(", ")
	b.WriteString(plural(tlsTerminated, "ACM certificate edge", "ACM certificate edges"))
	if tlsUnproven > 0 {
		b.WriteString(" (")
		b.WriteString(plural(tlsUnproven, "resource", "resources"))
		b.WriteString(" collector-absent)")
	}
	return b.String()
}

// plural renders "n singular" for n == 1, "n plural" otherwise.
func plural(n int, singular, pluralForm string) string {
	if n == 1 {
		return "1 " + singular
	}
	return strconv.Itoa(n) + " " + pluralForm
}

// loadServiceIngressPosture loads the WAF/TLS protection state for a service's
// internet-facing edge resources from the materialized graph and assembles the
// honest posture block. It runs three bounded single-clause set queries — the
// base set (which requested edges exist / collectorPresent), the WAF-protected
// set, and the ACM-terminated set — only when the service has at least one
// internet-facing edge resource, and merges them by membership in Go: an edge in
// the base set but absent from a protection set is a genuine observed-negative,
// distinct from an edge absent from the base set entirely (missing data). The
// prior single multi-clause OPTIONAL MATCH aggregation is mis-executed on the
// pinned NornicDB build (returns a null edge_id and reports every edge as
// protected); see docs/internal/evidence/5287-ingress-posture-nornicdb.md (#5287).
func loadServiceIngressPosture(
	ctx context.Context,
	graph GraphQuery,
	cloudResources []map[string]any,
) (map[string]any, error) {
	edges := edgeResourcesFromCloudResources(cloudResources)
	if len(edges) == 0 {
		return buildIngressPosture(nil, nil), nil
	}
	// protection maps each edge resource id to its collector observation. An id
	// that is absent from the map means the graph did not return a row for it,
	// which means the AWS collector slice for that resource has not yet been
	// collected. collectorPresent=false keeps the tile honest (unproven) rather
	// than falsely reporting observed-negative.
	protection := map[string]ingressEdgeProtection{}
	if graph != nil {
		ids := make([]string, 0, len(edges))
		for _, edge := range edges {
			ids = append(ids, edge.id)
		}
		params := map[string]any{"edge_ids": ids}
		baseSet, err := ingressPostureEdgeSet(ctx, graph, ingressPostureBaseCypher, params)
		if err != nil {
			return nil, err
		}
		wafSet, err := ingressPostureEdgeSet(ctx, graph, ingressPostureWafCypher, params)
		if err != nil {
			return nil, err
		}
		tlsSet, err := ingressPostureEdgeSet(ctx, graph, ingressPostureTLSCypher, params)
		if err != nil {
			return nil, err
		}
		// A row in the base set means the graph observed the resource
		// (collectorPresent); the waf/tls sets carry the protection facts. A
		// resource in the base set but absent from a protection set is a genuine
		// observed-negative (flag false), distinct from a resource absent from
		// the base set entirely (missing collector data, left out of the map).
		for id := range baseSet {
			protection[id] = ingressEdgeProtection{
				collectorPresent: true,
				wafProtected:     wafSet[id],
				tlsTerminated:    tlsSet[id],
			}
		}
	}
	return buildIngressPosture(edges, protection), nil
}

// ingressPostureEdgeSet runs a single-clause ingress-posture query and returns
// the set of edge ids it matched. Empty/blank ids are skipped.
func ingressPostureEdgeSet(ctx context.Context, graph GraphQuery, cypher string, params map[string]any) (map[string]bool, error) {
	rows, err := graph.Run(ctx, cypher, params)
	if err != nil {
		return nil, err
	}
	set := make(map[string]bool, len(rows))
	for _, row := range rows {
		if id := StringVal(row, "edge_id"); id != "" {
			set[id] = true
		}
	}
	return set, nil
}

// ingressPostureEdgeIdentity is the shared coalesce over the identity columns
// the cloud-resource loader projects; every ingress-posture query resolves an
// edge to the same id.
const ingressPostureEdgeIdentity = `coalesce(edge.id, edge.uid, edge.resource_id, edge.arn, edge.name)`

// The ingress-posture reads are three single-clause set queries, merged in Go.
// The prior multi-clause form (`MATCH edge OPTIONAL MATCH waf WITH edge,
// count(*)>0 OPTIONAL MATCH acm RETURN …, count(*)>0`) is mis-executed on the
// pinned NornicDB build: the aggregate-over-OPTIONAL-MATCH between two clauses
// returns a null edge_id and reports both flags as true even for an unprotected
// edge (#5287). An `EXISTS { … }` subquery is equally broken — it does not
// correlate with the outer edge. So each fact is a bounded single-clause set:
//
//   - base: which requested edges exist in the graph (collectorPresent);
//   - waf: which of them are protected by a WAF web ACL;
//   - tls: which of them terminate an ACM certificate.
//
// The handler computes each edge's flags by set membership, preserving the
// collectorPresent (row present) vs observed-negative (present, flag false) vs
// missing (absent) distinction the tile relies on.
const ingressPostureBaseCypher = `
MATCH (edge:CloudResource)
WHERE ` + ingressPostureEdgeIdentity + ` IN $edge_ids
RETURN DISTINCT ` + ingressPostureEdgeIdentity + ` AS edge_id`

const ingressPostureWafCypher = `
MATCH (:CloudResource)-[:AWS_wafv2_web_acl_protects_resource]->(edge:CloudResource)
WHERE ` + ingressPostureEdgeIdentity + ` IN $edge_ids
RETURN DISTINCT ` + ingressPostureEdgeIdentity + ` AS edge_id`

const ingressPostureTLSCypher = `
MATCH (:CloudResource)-[:AWS_acm_certificate_used_by_resource]->(edge:CloudResource)
WHERE ` + ingressPostureEdgeIdentity + ` IN $edge_ids
RETURN DISTINCT ` + ingressPostureEdgeIdentity + ` AS edge_id`
