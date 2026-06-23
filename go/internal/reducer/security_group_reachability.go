package reducer

import (
	"sort"
	"strconv"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/graph/edgetype"
)

// Closed relationship-type vocabulary for the SecurityGroup -> SecurityGroupRule
// edge. Direction drives the type; both members are static tokens validated by
// the cypher writer against a fixed allowlist before interpolation, exactly like
// the observability COVERS signal vocabulary (#391 PR3).
const (
	// securityGroupAllowsIngressRelType is the static Cypher relationship type for
	// an inbound rule edge.
	securityGroupAllowsIngressRelType = string(edgetype.AllowsIngress)
	// securityGroupAllowsEgressRelType is the static Cypher relationship type for
	// an outbound rule edge.
	securityGroupAllowsEgressRelType = string(edgetype.AllowsEgress)
)

// Closed target-label vocabulary for the SecurityGroupRule -[:TO]-> endpoint
// edge. The endpoint label is heterogeneous (the rule's source family decides
// it) but bounded: each label names a node the prerequisite materialization
// already committed, and the cypher writer validates it against this same closed
// set before interpolating it into the MATCH label position (#388 PR3 pattern).
const (
	securityGroupEndpointLabelCidrBlock     = "CidrBlock"
	securityGroupEndpointLabelCloudResource = "CloudResource"
	securityGroupEndpointLabelPrefixList    = "PrefixList"
)

// securityGroupRuleResourceType is the CloudResource resource_type the EC2
// security-group anchor (and a referenced security group) is keyed under, so the
// reachability extractor recomputes the exact uid the AWS resource materializer
// committed.
const securityGroupRuleResourceType = "aws_ec2_security_group"

// SecurityGroupReachabilityResult is the bounded, deterministic output of one
// generation's reachability extraction: the rule nodes to upsert, the
// SecurityGroup -> SecurityGroupRule edges, the SecurityGroupRule -> endpoint
// edges, and a tally of why rules were skipped. Every slice is deduplicated by
// node/edge identity and sorted for byte-stable batched writes across retries
// and reprojections.
type SecurityGroupReachabilityResult struct {
	RuleNodes         []map[string]any
	SGRuleEdges       []map[string]any
	RuleEndpointEdges []map[string]any
	Tally             securityGroupReachabilityTally
}

// securityGroupReachabilityTally is the honest accounting surface for skipped
// rules. Each counter names a distinct graceful-degradation reason so an
// operator can tell whether reachability edges are missing because a security
// group was not scanned, a referenced group was not scanned, or a rule reported
// no usable source — never because the reducer silently dropped a rule.
type securityGroupReachabilityTally struct {
	skippedUnresolvedAnchor   int
	skippedUnresolvedEndpoint int
	skippedUnknownSource      int
}

// total returns the count of rules that produced no graph truth.
func (t securityGroupReachabilityTally) total() int {
	return t.skippedUnresolvedAnchor + t.skippedUnresolvedEndpoint + t.skippedUnknownSource
}

// ExtractSecurityGroupReachability resolves each aws_security_group_rule fact's
// SG anchor and source endpoint against an in-memory CloudResource join index
// built from the scope generation's aws_resource facts (mirroring the AWS
// relationship edge join, #805 §5.1) and recomputes CidrBlock / PrefixList
// endpoint uids with the SAME uid funcs the endpoint materializer used (#1135
// PR2a). It never fabricates a node: a rule whose SG anchor or source endpoint is
// not a materialized node in this scope is counted in the tally and produces no
// rule node and no edges (graceful degradation, no dangle).
//
// Each live rule that fully resolves yields one :SecurityGroupRule node (keyed by
// the port-precise rule uid), one SG -> rule edge (relationship type from
// direction), and one rule -> endpoint edge (TO, with the endpoint label from the
// source family). Tombstoned rules and unknown-source rules are skipped. The
// returned slices are deduplicated and sorted so the batched write is stable.
func ExtractSecurityGroupReachability(
	resourceEnvelopes []facts.Envelope,
	ruleEnvelopes []facts.Envelope,
) SecurityGroupReachabilityResult {
	result := SecurityGroupReachabilityResult{}
	if len(ruleEnvelopes) == 0 {
		return result
	}

	index := buildCloudResourceJoinIndex(resourceEnvelopes)

	ruleNodesByUID := make(map[string]map[string]any)
	sgEdgesByKey := make(map[string]map[string]any)
	toEdgesByKey := make(map[string]map[string]any)

	for _, env := range ruleEnvelopes {
		if env.FactKind != facts.AWSSecurityGroupRuleFactKind {
			continue
		}
		// A tombstoned rule no longer grants reachability; the prior-generation
		// retract path owns removing any edges a prior live rule wrote.
		if env.IsTombstone {
			continue
		}

		sourceKind := payloadString(env.Payload, "source_kind")
		if sourceKind == securityGroupRuleSourceUnknown {
			// A rule that reported no CIDR, prefix list, or referenced group has no
			// endpoint to point an edge at. It is preserved as a fact upstream; here
			// it materializes nothing rather than fabricating a phantom endpoint.
			result.Tally.skippedUnknownSource++
			continue
		}

		accountID := payloadString(env.Payload, "account_id")
		region := payloadString(env.Payload, "region")
		groupID := payloadString(env.Payload, "group_id")

		// Resolve the SG anchor to its committed CloudResource node. The anchor uid
		// is recomputed the same way the AWS resource materializer keyed it, then
		// confirmed present in the join index so an unscanned group never dangles.
		sgUID, ok := resolveSecurityGroupNode(index, accountID, region, groupID)
		if !ok {
			result.Tally.skippedUnresolvedAnchor++
			continue
		}

		endpointUID, endpointLabel, ok := resolveSecurityGroupRuleEndpoint(index, env.Payload, accountID, region, sourceKind)
		if !ok {
			result.Tally.skippedUnresolvedEndpoint++
			continue
		}

		direction := payloadString(env.Payload, "direction")
		relType, ok := securityGroupRuleRelationshipType(direction)
		if !ok {
			// A direction outside {ingress, egress} cannot pick a closed-vocabulary
			// relationship type; treat it as an unknown source rather than guess.
			result.Tally.skippedUnknownSource++
			continue
		}

		ipProtocol := payloadString(env.Payload, "ip_protocol")
		fromPort := normalizeSecurityGroupRulePort(env.Payload["from_port"])
		toPort := normalizeSecurityGroupRulePort(env.Payload["to_port"])
		ruleUID := securityGroupRuleUIDFromTokens(sgUID, direction, ipProtocol, fromPort, toPort, sourceKind, payloadString(env.Payload, "source_value"))

		ruleNodesByUID[ruleUID] = map[string]any{
			"uid":              ruleUID,
			"sg_uid":           sgUID,
			"name":             securityGroupRuleDisplayName(direction, ipProtocol, fromPort, toPort),
			"direction":        direction,
			"ip_protocol":      ipProtocol,
			"from_port":        fromPort,
			"to_port":          toPort,
			"source_kind":      sourceKind,
			"is_internet":      payloadBool(env.Payload, "is_internet"),
			"source_fact_id":   env.FactID,
			"stable_fact_key":  env.StableFactKey,
			"source_system":    env.SourceRef.SourceSystem,
			"source_record_id": env.SourceRef.SourceRecordID,
			"collector_kind":   env.CollectorKind,
		}

		sgEdgeKey := relType + ":" + sgUID + "->" + ruleUID
		sgEdgesByKey[sgEdgeKey] = map[string]any{
			"sg_uid":            sgUID,
			"rule_uid":          ruleUID,
			"relationship_type": relType,
		}

		toEdgeKey := ruleUID + "->" + endpointLabel + ":" + endpointUID
		toEdgesByKey[toEdgeKey] = map[string]any{
			"rule_uid":     ruleUID,
			"target_uid":   endpointUID,
			"target_label": endpointLabel,
		}
	}

	result.RuleNodes = sortReachabilityRows(ruleNodesByUID, "uid")
	result.SGRuleEdges = sortReachabilitySGEdges(sgEdgesByKey)
	result.RuleEndpointEdges = sortReachabilityToEdges(toEdgesByKey)
	return result
}

// resolveSecurityGroupNode recomputes the CloudResource uid for a security group
// and confirms it is a materialized node in this scope generation. It returns
// ok=false when the group was not scanned, so an unresolved anchor degrades
// gracefully instead of dangling against a node that does not exist.
func resolveSecurityGroupNode(index cloudResourceJoinIndex, accountID, region, groupID string) (string, bool) {
	if groupID == "" {
		return "", false
	}
	uid := cloudResourceUID(accountID, region, securityGroupRuleResourceType, groupID)
	if _, ok := index.byResourceID[groupID]; ok {
		// The bare group id is the resource_id the EC2 scanner emits, so a hit
		// confirms the node committed. The recomputed uid is byte-identical to the
		// indexed uid (same account/region/type/id inputs), so either is the node.
		return uid, true
	}
	return "", false
}

// resolveSecurityGroupRuleEndpoint resolves the rule's source endpoint to a
// committed node uid plus its heterogeneous (but closed-vocabulary) target label.
// CIDR endpoints recompute the CidrBlock uid; prefix-list endpoints recompute the
// PrefixList uid; a referenced security group resolves through the same
// CloudResource join index as the anchor (same account/region trust boundary). A
// referenced group that was not scanned returns ok=false so the edge degrades
// gracefully rather than fabricating an endpoint.
func resolveSecurityGroupRuleEndpoint(
	index cloudResourceJoinIndex,
	payload map[string]any,
	accountID, region, sourceKind string,
) (string, string, bool) {
	switch sourceKind {
	case securityGroupRuleSourceCIDRIPv4, securityGroupRuleSourceCIDRIPv6:
		canonical, ok := canonicalizeCIDR(payloadString(payload, "source_value"))
		if !ok {
			return "", "", false
		}
		family := cidrBlockAddressFamilyIPv4
		if sourceKind == securityGroupRuleSourceCIDRIPv6 {
			family = cidrBlockAddressFamilyIPv6
		}
		return cidrBlockUID(canonical, family), securityGroupEndpointLabelCidrBlock, true
	case securityGroupRuleSourcePrefixList:
		prefixListID := payloadString(payload, "source_value")
		if prefixListID == "" {
			return "", "", false
		}
		return prefixListUID(accountID, region, prefixListID), securityGroupEndpointLabelPrefixList, true
	case securityGroupRuleSourceSecurityGroup:
		referencedGroupID := payloadString(payload, "source_value")
		uid, ok := resolveSecurityGroupNode(index, accountID, region, referencedGroupID)
		if !ok {
			return "", "", false
		}
		return uid, securityGroupEndpointLabelCloudResource, true
	default:
		return "", "", false
	}
}

// securityGroupRuleSourceSecurityGroup mirrors the scanner's
// referenced-security-group source kind. It is intentionally duplicated from the
// collector envelope (which the reducer must not import) so the reducer keys on
// the same normalized discriminator.
const securityGroupRuleSourceSecurityGroup = "referenced_security_group"

// securityGroupRuleSourceUnknown mirrors the scanner's unknown source kind: a
// rule whose describe response carried no CIDR, prefix list, or referenced group.
const securityGroupRuleSourceUnknown = "unknown"

// securityGroupRuleRelationshipType maps a normalized rule direction to its
// closed-vocabulary Cypher relationship type, returning ok=false for any
// direction outside {ingress, egress} so an unexpected value never fabricates a
// new relationship type.
func securityGroupRuleRelationshipType(direction string) (string, bool) {
	switch direction {
	case "ingress":
		return securityGroupAllowsIngressRelType, true
	case "egress":
		return securityGroupAllowsEgressRelType, true
	default:
		return "", false
	}
}

// normalizeSecurityGroupRulePort renders a rule port into a canonical, type-stable
// token for the rule uid and node property. Ports arrive as int32 from the
// in-memory scanner path but as float64 after a Postgres JSON roundtrip, so this
// folds every integral representation to the same decimal string. A nil port (an
// all-protocols / all-ports rule omits the range) renders as the empty string so
// it stays distinct from real port 0 and never collapses to "0".
func normalizeSecurityGroupRulePort(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case int:
		return strconv.FormatInt(int64(typed), 10)
	case int32:
		return strconv.FormatInt(int64(typed), 10)
	case int64:
		return strconv.FormatInt(typed, 10)
	case float64:
		// JSON numbers decode to float64; ports are integral, so truncate the
		// already-integral value rather than formatting a float with a decimal.
		return strconv.FormatInt(int64(typed), 10)
	case string:
		return typed
	default:
		return ""
	}
}

// securityGroupRuleDisplayName builds the human-readable name stored on the
// :SecurityGroupRule node so the graph/entities networking inventory can
// display a meaningful label instead of an empty string. The format is
// direction/protocol/fromPort-toPort; an all-ports rule (empty port range)
// renders as direction/protocol/all.
func securityGroupRuleDisplayName(direction, ipProtocol, fromPort, toPort string) string {
	portRange := fromPort + "-" + toPort
	if fromPort == "" && toPort == "" {
		portRange = "all"
	}
	return direction + "/" + ipProtocol + "/" + portRange
}

// securityGroupRuleUID is the test-facing rule identity helper: it normalizes the
// port arguments (which tests pass as int32/float64) and delegates to the
// token-based identity so a test and the extractor agree on the uid regardless of
// port value type.
func securityGroupRuleUID(sgUID, direction, ipProtocol string, fromPort, toPort any, sourceKind, sourceValue string) string {
	return securityGroupRuleUIDFromTokens(
		sgUID,
		direction,
		ipProtocol,
		normalizeSecurityGroupRulePort(fromPort),
		normalizeSecurityGroupRulePort(toPort),
		sourceKind,
		sourceValue,
	)
}

// securityGroupRuleUIDFromTokens computes the stable :SecurityGroupRule node
// identity from already-normalized tokens. The identity folds the SG anchor uid,
// direction, protocol, normalized port range, and the normalized source so port
// and protocol live in the NODE key (Option D) — two ports key two nodes — rather
// than in a relationship-property MERGE that times out on NornicDB.
func securityGroupRuleUIDFromTokens(sgUID, direction, ipProtocol, fromPort, toPort, sourceKind, sourceValue string) string {
	return facts.StableID("SecurityGroupRule", map[string]any{
		"direction":    direction,
		"from_port":    fromPort,
		"ip_protocol":  ipProtocol,
		"sg_uid":       sgUID,
		"source_kind":  sourceKind,
		"source_value": sourceValue,
		"to_port":      toPort,
	})
}

// sortReachabilityRows returns the deduplicated node rows sorted by the named uid
// key for deterministic, byte-stable batch output.
func sortReachabilityRows(byUID map[string]map[string]any, uidKey string) []map[string]any {
	if len(byUID) == 0 {
		return nil
	}
	keys := make([]string, 0, len(byUID))
	for key := range byUID {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	rows := make([]map[string]any, 0, len(keys))
	for _, key := range keys {
		rows = append(rows, byUID[key])
	}
	return rows
}

// sortReachabilitySGEdges returns SG->rule edge rows sorted by
// (relationship_type, sg_uid, rule_uid) for stable batched writes.
func sortReachabilitySGEdges(byKey map[string]map[string]any) []map[string]any {
	rows := mapValues(byKey)
	if len(rows) == 0 {
		return nil
	}
	sort.Slice(rows, func(a, b int) bool {
		left := anyToString(rows[a]["relationship_type"]) + ":" + anyToString(rows[a]["sg_uid"]) + "->" + anyToString(rows[a]["rule_uid"])
		right := anyToString(rows[b]["relationship_type"]) + ":" + anyToString(rows[b]["sg_uid"]) + "->" + anyToString(rows[b]["rule_uid"])
		return left < right
	})
	return rows
}

// sortReachabilityToEdges returns rule->endpoint edge rows sorted by
// (rule_uid, target_label, target_uid) for stable batched writes.
func sortReachabilityToEdges(byKey map[string]map[string]any) []map[string]any {
	rows := mapValues(byKey)
	if len(rows) == 0 {
		return nil
	}
	sort.Slice(rows, func(a, b int) bool {
		left := anyToString(rows[a]["rule_uid"]) + "->" + anyToString(rows[a]["target_label"]) + ":" + anyToString(rows[a]["target_uid"])
		right := anyToString(rows[b]["rule_uid"]) + "->" + anyToString(rows[b]["target_label"]) + ":" + anyToString(rows[b]["target_uid"])
		return left < right
	})
	return rows
}

// mapValues collects the values of a deterministic edge map into a slice for
// sorting.
func mapValues(byKey map[string]map[string]any) []map[string]any {
	if len(byKey) == 0 {
		return nil
	}
	rows := make([]map[string]any, 0, len(byKey))
	for _, row := range byKey {
		rows = append(rows, row)
	}
	return rows
}
