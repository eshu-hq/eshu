package query

import (
	"context"
	"sort"
	"strconv"
	"strings"
)

// Ingress posture truth states. They are deliberately three-valued so the
// console never collapses "no protection" and "no evidence" into one tile: a
// missing WAF edge on a materialized edge resource is an observed-negative
// (unprotected), while no materialized edge resource at all is unresolved.
const (
	// ingressPostureProtected means an internet-facing edge resource carries a
	// materialized WAF web-ACL edge (observed positive).
	ingressPostureProtected = "protected"
	// ingressPostureUnprotected means an internet-facing edge resource exists in
	// the graph but carries no WAF web-ACL edge (observed negative).
	ingressPostureUnprotected = "unprotected"
	// ingressPostureTerminated means an internet-facing edge resource carries a
	// materialized ACM certificate edge (observed positive).
	ingressPostureTerminated = "terminated"
	// ingressPostureNotTerminated means an internet-facing edge resource exists
	// but carries no ACM certificate edge (observed negative).
	ingressPostureNotTerminated = "not_terminated"
	// ingressPostureUnproven means no internet-facing edge resource is
	// materialized for this service, so neither WAF nor TLS can be proven or
	// disproven (unresolved).
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
// and/or an ACM certificate edge is materialized in the graph.
type ingressEdgeProtection struct {
	// wafProtected is true when an AWS_wafv2_web_acl_protects_resource edge
	// terminates on the edge resource.
	wafProtected bool
	// tlsTerminated is true when an AWS_acm_certificate_used_by_resource edge
	// terminates on the edge resource.
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
// edge resources and the per-resource protection map loaded from the graph. With
// no edge resources it returns an unproven posture (neither WAF nor TLS can be
// proven or disproven). With edge resources it reports protected/unprotected and
// terminated/not_terminated honestly, plus the counts an operator needs.
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
	edgeRows := make([]map[string]any, 0, len(edges))
	for _, edge := range edges {
		p := protection[edge.id]
		if p.wafProtected {
			wafProtected++
		}
		if p.tlsTerminated {
			tlsTerminated++
		}
		edgeRows = append(edgeRows, map[string]any{
			"id":              edge.id,
			"name":            edge.name,
			"resource_type":   edge.resourceType,
			"waf_coverage":    edgeWAFState(p.wafProtected),
			"tls_termination": edgeTLSState(p.tlsTerminated),
		})
	}

	return map[string]any{
		"waf_coverage":    coverageState(wafProtected, len(edges), ingressPostureProtected, ingressPostureUnprotected),
		"tls_termination": coverageState(tlsTerminated, len(edges), ingressPostureTerminated, ingressPostureNotTerminated),
		"edge_count":      len(edges),
		"waf_protected":   wafProtected,
		"tls_terminated":  tlsTerminated,
		"truth_basis":     "observed",
		"edges":           edgeRows,
		"reason":          ingressPostureReason(wafProtected, tlsTerminated, len(edges)),
	}
}

// edgeWAFState maps a single edge's WAF observation to the closed state vocab.
func edgeWAFState(protected bool) string {
	if protected {
		return ingressPostureProtected
	}
	return ingressPostureUnprotected
}

// edgeTLSState maps a single edge's TLS observation to the closed state vocab.
func edgeTLSState(terminated bool) string {
	if terminated {
		return ingressPostureTerminated
	}
	return ingressPostureNotTerminated
}

// coverageState rolls per-edge observations into one tile state. With every edge
// covered it is the positive state, with none covered the negative state, and
// with a mix it stays negative (the most cautious roll-up: any uncovered edge is
// an exposure an operator must see).
func coverageState(covered, total int, positive, negative string) string {
	if covered >= total && total > 0 {
		return positive
	}
	return negative
}

// ingressPostureReason explains the rolled-up posture for an operator.
func ingressPostureReason(wafProtected, tlsTerminated, total int) string {
	var b strings.Builder
	b.WriteString("observed across ")
	b.WriteString(plural(total, "internet-facing edge resource", "internet-facing edge resources"))
	b.WriteString(": ")
	b.WriteString(plural(wafProtected, "WAF web-ACL edge", "WAF web-ACL edges"))
	b.WriteString(", ")
	b.WriteString(plural(tlsTerminated, "ACM certificate edge", "ACM certificate edges"))
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
// honest posture block. It runs at most one bounded graph query, only when the
// service has at least one internet-facing edge resource. The query matches the
// two materialized AWS edges (AWS_wafv2_web_acl_protects_resource and
// AWS_acm_certificate_used_by_resource) terminating on the edge resource ids, so
// absence of an edge is reported as observed-negative, never as missing data.
func loadServiceIngressPosture(
	ctx context.Context,
	graph GraphQuery,
	cloudResources []map[string]any,
) (map[string]any, error) {
	edges := edgeResourcesFromCloudResources(cloudResources)
	if len(edges) == 0 {
		return buildIngressPosture(nil, nil), nil
	}
	protection := map[string]ingressEdgeProtection{}
	if graph != nil {
		ids := make([]string, 0, len(edges))
		for _, edge := range edges {
			ids = append(ids, edge.id)
		}
		rows, err := graph.Run(ctx, ingressPostureCypher, map[string]any{
			"edge_ids": ids,
		})
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			id := StringVal(row, "edge_id")
			if id == "" {
				continue
			}
			protection[id] = ingressEdgeProtection{
				wafProtected:  BoolVal(row, "waf_protected"),
				tlsTerminated: BoolVal(row, "tls_terminated"),
			}
		}
	}
	return buildIngressPosture(edges, protection), nil
}

// ingressPostureCypher matches the two materialized AWS protection edges on each
// candidate edge resource. It is anchored on the bounded $edge_ids list (the
// service's own internet-facing resources), so the scan is bounded by that set.
// OPTIONAL MATCH ensures a resource with no protection edge still returns a row
// with false flags (observed-negative), distinct from a resource that does not
// appear at all. The coalesce over id/uid/resource_id/arn matches the same
// identity columns the cloud-resource loader projects.
const ingressPostureCypher = `
MATCH (edge:CloudResource)
WHERE coalesce(edge.id, edge.uid, edge.resource_id, edge.arn, edge.name) IN $edge_ids
OPTIONAL MATCH (:CloudResource)-[:AWS_wafv2_web_acl_protects_resource]->(edge)
WITH edge, count(*) > 0 AS waf_protected
OPTIONAL MATCH (:CloudResource)-[:AWS_acm_certificate_used_by_resource]->(edge)
RETURN coalesce(edge.id, edge.uid, edge.resource_id, edge.arn, edge.name) AS edge_id,
       waf_protected AS waf_protected,
       count(*) > 0 AS tls_terminated`
