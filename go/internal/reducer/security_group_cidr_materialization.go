// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"log/slog"
	"net/netip"
	"sort"
	"strings"
	"time"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/truth"
	log "github.com/eshu-hq/eshu/go/pkg/log"
	awsv1 "github.com/eshu-hq/eshu/sdk/go/factschema/aws/v1"
)

// Security-group rule source-kind discriminators the reducer keys on. They
// mirror the scanner envelope's normalized source kinds so the reducer does not
// re-derive which target family a rule names. Referenced-security-group and
// unknown endpoints are intentionally absent: a referenced group already has a
// CloudResource node and is not re-materialized here, and an unknown source
// materializes no endpoint node.
const (
	securityGroupRuleSourceCIDRIPv4   = "cidr_ipv4"
	securityGroupRuleSourceCIDRIPv6   = "cidr_ipv6"
	securityGroupRuleSourcePrefixList = "prefix_list"
)

// Endpoint-kind labels for the security-group endpoint materialization counter.
const (
	securityGroupEndpointKindCidrBlock  = "cidr_block"
	securityGroupEndpointKindPrefixList = "prefix_list"
)

// CidrBlock address-family property values, derived from the rule's source kind
// rather than re-parsed so the node's family always matches the rule's intent.
const (
	cidrBlockAddressFamilyIPv4 = "ipv4"
	cidrBlockAddressFamilyIPv6 = "ipv6"
)

// internetCIDRIPv4 and internetCIDRIPv6 are the canonical open-to-the-world
// CIDRs. They are already masked, so the canonical form equals the literal.
const (
	internetCIDRIPv4 = "0.0.0.0/0"
	internetCIDRIPv6 = "::/0"
)

// securityGroupCidrMaterializationDomainDefinition returns the additive
// definition for security-group CIDR and prefix-list endpoint node
// materialization. It is additive (not part of DefaultDomainDefinitions) because
// the handler requires an explicitly wired SecurityGroupEndpointNodeWriter and
// FactLoader; registering it without them would silently drop every intent. The
// network-reachability edge slice (#1135 PR2b) joins against the nodes this
// domain commits. See issue #1135.
func securityGroupCidrMaterializationDomainDefinition() DomainDefinition {
	return DomainDefinition{
		Domain:  DomainSecurityGroupCidrMaterialization,
		Summary: "materialize aws_security_group_rule CIDR and prefix-list endpoints into canonical CidrBlock and PrefixList graph nodes",
		Ownership: OwnershipShape{
			CrossSource:    true,
			CrossScope:     true,
			CanonicalWrite: true,
		},
		TruthContract: truth.Contract{
			CanonicalKind: "security_group_cidr_materialization",
			SourceLayers: []truth.Layer{
				truth.LayerObservedResource,
			},
		},
	}
}

// securityGroupEndpointEvidenceSource tags CidrBlock and PrefixList nodes written
// by this reducer so the prior-generation retract path (and the future edge
// projection) can scope its writes to reducer-owned security-group endpoint
// materialization.
const securityGroupEndpointEvidenceSource = "reducer/security-group-endpoints"

// SecurityGroupEndpointNodeWriter persists canonical CidrBlock and PrefixList
// graph nodes from extracted node rows. Implementations MUST be idempotent by
// node uid so reducer retries and duplicate facts converge on one node rather
// than duplicating or fabricating graph state.
type SecurityGroupEndpointNodeWriter interface {
	WriteCidrBlockNodes(ctx context.Context, rows []map[string]any, evidenceSource string) error
	WritePrefixListNodes(ctx context.Context, rows []map[string]any, evidenceSource string) error
}

// SecurityGroupCidrMaterializationHandler reduces one security-group endpoint
// materialization follow-up into canonical CidrBlock and PrefixList node writes.
// It loads the scope generation's aws_security_group_rule facts, projects their
// CIDR and managed-prefix-list source endpoints into deterministic node rows,
// and hands the bounded batches to the node writer.
//
// This handler is the endpoint-node substrate that the #1135 network-reachability
// edge projection (PR2b) joins against. It intentionally does not write edges:
// the ALLOWS_INGRESS/EGRESS edges are resolved against these nodes in a separate,
// gated stage, mirroring how the AWS relationship edge projection (#805) joins
// against CloudResource nodes. The referenced-security-group endpoint already has
// a CloudResource node and is not re-materialized here.
//
// After the canonical node writes succeed, the handler publishes the
// GraphProjectionKeyspaceSecurityGroupEndpointUID /
// GraphProjectionPhaseCanonicalNodesCommitted readiness phase. The edge slice
// gates its projection on this phase, so edges never resolve against a generation
// whose endpoint nodes have not yet committed.
type SecurityGroupCidrMaterializationHandler struct {
	FactLoader FactLoader
	NodeWriter SecurityGroupEndpointNodeWriter
	// PhasePublisher records the canonical-nodes-committed readiness phase that
	// gates the network-reachability edge projection. A nil publisher is a no-op so
	// the additive domain stays safe to register before the edge slice is wired.
	PhasePublisher GraphProjectionPhasePublisher
	// Instruments records the endpoint-nodes-materialized counter. Nil-safe.
	Instruments *telemetry.Instruments
}

// Handle executes one security-group endpoint materialization intent.
func (h SecurityGroupCidrMaterializationHandler) Handle(
	ctx context.Context,
	intent Intent,
) (Result, error) {
	totalStart := time.Now()
	if intent.Domain != DomainSecurityGroupCidrMaterialization {
		return Result{}, fmt.Errorf(
			"security group cidr materialization handler does not accept domain %q",
			intent.Domain,
		)
	}
	if h.FactLoader == nil {
		return Result{}, fmt.Errorf("security group cidr materialization fact loader is required")
	}
	if h.NodeWriter == nil {
		return Result{}, fmt.Errorf("security group cidr materialization node writer is required")
	}

	loadStart := time.Now()
	envelopes, err := loadFactsForKinds(
		ctx,
		h.FactLoader,
		intent.ScopeID,
		intent.GenerationID,
		[]string{facts.AWSSecurityGroupRuleFactKind},
	)
	if err != nil {
		return Result{}, fmt.Errorf("load facts for security group cidr materialization: %w", err)
	}
	loadDuration := time.Since(loadStart)

	extractStart := time.Now()
	cidrRows, prefixRows, err := ExtractSecurityGroupEndpointRows(envelopes)
	if err != nil {
		// A malformed aws_security_group_rule payload (a missing required identity
		// field) is a classified input_invalid decode failure; dead-letter the
		// intent instead of materializing an endpoint node from a malformed rule.
		return Result{}, err
	}
	extractDuration := time.Since(extractStart)

	writeStart := time.Now()
	if len(cidrRows) > 0 {
		if err := h.NodeWriter.WriteCidrBlockNodes(ctx, cidrRows, securityGroupEndpointEvidenceSource); err != nil {
			return Result{}, fmt.Errorf("write canonical cidr block nodes: %w", err)
		}
	}
	if len(prefixRows) > 0 {
		if err := h.NodeWriter.WritePrefixListNodes(ctx, prefixRows, securityGroupEndpointEvidenceSource); err != nil {
			return Result{}, fmt.Errorf("write canonical prefix list nodes: %w", err)
		}
	}
	writeDuration := time.Since(writeStart)

	// Publish the canonical-nodes-committed readiness phase only after both node
	// writes succeed (or are a legitimate no-op for an empty generation). The edge
	// slice gates its projection on this phase: publishing before a successful
	// write would let edges resolve against nodes that never committed, and not
	// publishing on an empty generation would block the edge slice forever.
	phasePublishStart := time.Now()
	if err := publishIntentGraphPhase(
		ctx,
		h.PhasePublisher,
		intent,
		GraphProjectionKeyspaceSecurityGroupEndpointUID,
		GraphProjectionPhaseCanonicalNodesCommitted,
		time.Now().UTC(),
	); err != nil {
		return Result{}, fmt.Errorf("publish canonical security group endpoint nodes phase: %w", err)
	}
	phasePublishDuration := time.Since(phasePublishStart)

	h.recordEndpointsMaterialized(ctx, securityGroupEndpointKindCidrBlock, len(cidrRows))
	h.recordEndpointsMaterialized(ctx, securityGroupEndpointKindPrefixList, len(prefixRows))
	logSecurityGroupCidrMaterializationCompleted(ctx, securityGroupCidrMaterializationTiming{
		intent:               intent,
		factCount:            len(envelopes),
		cidrCount:            len(cidrRows),
		prefixCount:          len(prefixRows),
		loadDuration:         loadDuration,
		extractDuration:      extractDuration,
		writeDuration:        writeDuration,
		phasePublishDuration: phasePublishDuration,
		totalDuration:        time.Since(totalStart),
	})

	return Result{
		IntentID: intent.IntentID,
		Domain:   DomainSecurityGroupCidrMaterialization,
		Status:   ResultStatusSucceeded,
		EvidenceSummary: fmt.Sprintf(
			"materialized %d cidr block + %d prefix list node(s) from %d security group rule fact(s)",
			len(cidrRows),
			len(prefixRows),
			len(envelopes),
		),
		CanonicalWrites: len(cidrRows) + len(prefixRows),
	}, nil
}

// recordEndpointsMaterialized emits the SecurityGroupEndpointNodes counter so an
// operator can see how many CIDR/prefix endpoint nodes one generation committed,
// which is the substrate the later edge slice resolves against. A zero count is
// itself a signal (every rule named a referenced group, or carried an
// unparseable CIDR), so the counter is recorded even when no rows materialized.
func (h SecurityGroupCidrMaterializationHandler) recordEndpointsMaterialized(ctx context.Context, endpointKind string, count int) {
	if h.Instruments == nil || h.Instruments.SecurityGroupEndpointNodes == nil {
		return
	}
	h.Instruments.SecurityGroupEndpointNodes.Add(ctx, int64(count), metric.WithAttributes(
		telemetry.AttrDomain(string(DomainSecurityGroupCidrMaterialization)),
		telemetry.AttrEndpointKind(endpointKind),
	))
}

// ExtractSecurityGroupEndpointRows projects aws_security_group_rule fact
// envelopes into deterministic CidrBlock and PrefixList node rows. CIDR
// endpoints are canonicalized (masked, lowercased) and deduplicated by uid so
// rules naming the same network with different host bits or casing converge on a
// single node; prefix-list endpoints are deduplicated by their account/region
// scoped uid. Tombstoned rules, referenced-security-group and unknown sources,
// and unparseable CIDRs are dropped rather than fabricating an endpoint node. The
// returned rows are each sorted by uid for deterministic batch output.
func ExtractSecurityGroupEndpointRows(envelopes []facts.Envelope) (cidrRows, prefixRows []map[string]any, err error) {
	if len(envelopes) == 0 {
		return nil, nil, nil
	}

	cidrByUID := make(map[string]map[string]any)
	prefixByUID := make(map[string]map[string]any)
	for _, env := range envelopes {
		if env.FactKind != facts.AWSSecurityGroupRuleFactKind {
			continue
		}
		// A tombstoned rule no longer grants reachability, so its endpoint is not
		// materialized from this fact; the node may still exist from another live
		// rule, and the prior-generation retract path owns removal.
		if env.IsTombstone {
			continue
		}
		rule, decodeErr := decodeAWSSecurityGroupRule(env)
		if decodeErr != nil {
			return nil, nil, decodeErr
		}
		switch rule.SourceKind {
		case securityGroupRuleSourceCIDRIPv4, securityGroupRuleSourceCIDRIPv6:
			if row, uid, ok := cidrBlockNodeRow(env, rule); ok {
				cidrByUID[uid] = row
			}
		case securityGroupRuleSourcePrefixList:
			if row, uid, ok := prefixListNodeRow(env, rule); ok {
				prefixByUID[uid] = row
			}
		}
	}

	return sortRowsByUID(cidrByUID), sortRowsByUID(prefixByUID), nil
}

// cidrBlockNodeRow builds one CidrBlock node row from a decoded security-group
// rule, returning ok=false when the rule's CIDR cannot be canonicalized into a
// deterministic identity. The uid is a stable hash of the canonical CIDR plus
// the address family so the same network always resolves to the same node, which
// the later edge projection can recompute from a rule's resolved endpoint. The
// env supplies only the provenance scalars stamped onto the row.
func cidrBlockNodeRow(env facts.Envelope, rule awsv1.SecurityGroupRule) (map[string]any, string, bool) {
	canonical, ok := canonicalizeCIDR(rule.SourceValue)
	if !ok {
		return nil, "", false
	}
	family := cidrBlockAddressFamilyIPv4
	if rule.SourceKind == securityGroupRuleSourceCIDRIPv6 {
		family = cidrBlockAddressFamilyIPv6
	}
	uid := cidrBlockUID(canonical, family)
	row := map[string]any{
		"uid":              uid,
		"cidr":             canonical,
		"address_family":   family,
		"is_internet":      isInternetCIDR(canonical),
		"source_fact_id":   env.FactID,
		"stable_fact_key":  env.StableFactKey,
		"source_system":    env.SourceRef.SourceSystem,
		"source_record_id": env.SourceRef.SourceRecordID,
		"collector_kind":   env.CollectorKind,
	}
	return row, uid, true
}

// prefixListNodeRow builds one PrefixList node row from a decoded security-group
// rule, returning ok=false when the rule lacks a prefix-list id. The uid folds
// account_id and region into the identity because prefix-list ids are unique
// only within an account/region visibility scope. The env supplies only the
// provenance scalars stamped onto the row.
func prefixListNodeRow(env facts.Envelope, rule awsv1.SecurityGroupRule) (map[string]any, string, bool) {
	prefixListID := strings.TrimSpace(rule.SourceValue)
	if prefixListID == "" {
		return nil, "", false
	}
	accountID := rule.AccountID
	region := rule.Region
	uid := prefixListUID(accountID, region, prefixListID)
	row := map[string]any{
		"uid":              uid,
		"prefix_list_id":   prefixListID,
		"account_id":       accountID,
		"region":           region,
		"source_fact_id":   env.FactID,
		"stable_fact_key":  env.StableFactKey,
		"source_system":    env.SourceRef.SourceSystem,
		"source_record_id": env.SourceRef.SourceRecordID,
		"collector_kind":   env.CollectorKind,
	}
	return row, uid, true
}

// canonicalizeCIDR normalizes a CIDR string to its masked, lowercased network
// form (host bits zeroed, IPv6 zero-compressed) so two rules naming the same
// network with different host bits or casing converge on one endpoint node. It
// returns ok=false for an unparseable prefix so the caller materializes no node
// rather than fabricating a phantom endpoint.
func canonicalizeCIDR(value string) (string, bool) {
	prefix, err := netip.ParsePrefix(strings.TrimSpace(value))
	if err != nil {
		return "", false
	}
	return prefix.Masked().String(), true
}

// isInternetCIDR reports whether a canonical CIDR opens reachability to the
// public internet. Only the canonical open CIDRs count; canonicalization already
// masked host bits, so 0.0.0.0/0 and ::/0 are byte-stable here.
func isInternetCIDR(canonical string) bool {
	return canonical == internetCIDRIPv4 || canonical == internetCIDRIPv6
}

// cidrBlockUID computes the stable CidrBlock node identity from the canonical
// CIDR and its address family.
func cidrBlockUID(canonicalCIDR, addressFamily string) string {
	return facts.StableID("CidrBlock", map[string]any{
		"address_family": addressFamily,
		"cidr":           canonicalCIDR,
	})
}

// prefixListUID computes the stable PrefixList node identity. Prefix-list ids are
// unique only within an account/region scope, so the identity folds both in.
func prefixListUID(accountID, region, prefixListID string) string {
	return facts.StableID("PrefixList", map[string]any{
		"account_id":     accountID,
		"prefix_list_id": prefixListID,
		"region":         region,
	})
}

// sortRowsByUID returns the deduplicated rows sorted by uid for deterministic,
// byte-stable batch output across retries and reprojections.
func sortRowsByUID(byUID map[string]map[string]any) []map[string]any {
	if len(byUID) == 0 {
		return nil
	}
	uids := make([]string, 0, len(byUID))
	for uid := range byUID {
		uids = append(uids, uid)
	}
	sort.Strings(uids)

	rows := make([]map[string]any, 0, len(uids))
	for _, uid := range uids {
		rows = append(rows, byUID[uid])
	}
	return rows
}

// securityGroupCidrMaterializationTiming groups stage durations so the completion
// log can identify whether endpoint work is fact loading, extraction, or graph
// backend time.
type securityGroupCidrMaterializationTiming struct {
	intent               Intent
	factCount            int
	cidrCount            int
	prefixCount          int
	loadDuration         time.Duration
	extractDuration      time.Duration
	writeDuration        time.Duration
	phasePublishDuration time.Duration
	totalDuration        time.Duration
}

func logSecurityGroupCidrMaterializationCompleted(
	ctx context.Context,
	timing securityGroupCidrMaterializationTiming,
) {
	slog.InfoContext(
		ctx, "security group cidr materialization completed",
		log.ScopeID(timing.intent.ScopeID),
		log.GenerationID(timing.intent.GenerationID),
		log.Domain(string(timing.intent.Domain)),
		slog.Int("fact_count", timing.factCount),
		slog.Int("cidr_node_count", timing.cidrCount),
		slog.Int("prefix_list_node_count", timing.prefixCount),
		slog.Float64("load_facts_duration_seconds", timing.loadDuration.Seconds()),
		slog.Float64("extract_duration_seconds", timing.extractDuration.Seconds()),
		slog.Float64("graph_write_duration_seconds", timing.writeDuration.Seconds()),
		slog.Float64("phase_publish_duration_seconds", timing.phasePublishDuration.Seconds()),
		slog.Float64("total_duration_seconds", timing.totalDuration.Seconds()),
	)
}
