// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package schedulereplay

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/replay/cassette"
)

// Fact kinds this file maps into projection work items. Each is drawn from the
// fact-kind-registry entries for the two shared-conflict-key reducer domains
// (projection:incident_repository_correlation, projection:supply_chain_impact):
// specs/fact-kind-registry.v1.yaml.
const (
	factKindIncidentRecord        = "incident.record"
	factKindWorkItemRecord        = "work_item.record"
	factKindWorkItemExternalLink  = "work_item.external_link"
	factKindVulnerabilityCVE      = "vulnerability.cve"
	factKindVulnerabilityAffected = "vulnerability.affected_package"
	factKindScannerWorkerAnalysis = "scanner_worker.analysis"
	factKindVulnerabilitySupp     = "vulnerability.suppression"
)

// Projection hook names, copied verbatim from the fact-kind-registry entries
// that share the two reducer_domain values under proof here. IntentIDs are
// prefixed with the owning hook so a scenario can assert cross-hook fan-in
// (>=2 distinct hooks) rather than accidentally collapsing to one.
const (
	hookIncidentContextReadModel          = "incident_context_read_model"
	hookWorkItemEvidenceReadModel         = "work_item_evidence_read_model"
	hookSupplyChainImpact                 = "supply_chain_impact"
	hookVulnerabilitySourceState          = "vulnerability_source_state"
	hookVulnerabilitySuppressionAdmission = "vulnerability_suppression_admission"
)

// Canonical node labels the two shared-conflict-key projection cassettes
// produce. Each label is owned by exactly one projection hook (no two hooks
// upsert the same label), so cross-hook interaction only happens through
// edges — the property the ordering gate is proving.
const (
	nodeLabelIncident      = "Incident"
	nodeLabelWorkItem      = "WorkItem"
	nodeLabelVulnerability = "Vulnerability"
	nodeLabelPackage       = "Package"
	nodeLabelFinding       = "Finding"
	nodeLabelSuppression   = "Suppression"
)

// nodeLabelOwningHook maps each canonical node label to the single projection
// hook that owns (creates) it. It backs the cross-hook cassette guard in
// LoadProjectionWorkItems: single-writer ownership is what keeps the ordering
// snapshots deterministic, and an edge is cross-hook exactly when its From and
// To labels resolve to different owners here.
var nodeLabelOwningHook = map[string]string{
	nodeLabelIncident:      hookIncidentContextReadModel,
	nodeLabelWorkItem:      hookWorkItemEvidenceReadModel,
	nodeLabelVulnerability: hookVulnerabilitySourceState,
	nodeLabelPackage:       hookVulnerabilitySourceState,
	nodeLabelFinding:       hookSupplyChainImpact,
	nodeLabelSuppression:   hookVulnerabilitySuppressionAdmission,
}

// LoadProjectionWorkItems reads a committed replayschedule cassette through the
// real cassette.Source seam and returns one WorkItem per recorded fact, so the
// ordering scenario's inputs track recorded fact shapes rather than inline
// synthesis (the same no-inline-synthesis invariant LoadWorkItems upholds for
// the R-5 offline-tier cassette). It fails loudly — rather than returning a
// partial or empty schedule that would look green — on an unopenable cassette,
// an unrecognized fact kind, or a fact missing a required cross-reference key.
//
// Unlike LoadWorkItems (which goes through the offlinetier git-shape
// materializer), this loader decodes fact payloads itself: the two
// shared-conflict-key projections it covers
// (projection:incident_repository_correlation, projection:supply_chain_impact)
// have no offline-tier materializer, so the cassette -> WorkItem seam lives
// here instead.
func LoadProjectionWorkItems(cassettePath string) ([]WorkItem, error) {
	src, err := cassette.NewSource(cassettePath)
	if err != nil {
		return nil, fmt.Errorf("open cassette %q: %w", cassettePath, err)
	}
	var items []WorkItem
	for {
		gen, ok, err := src.Next(context.Background())
		if err != nil {
			return nil, fmt.Errorf("read cassette %q generation: %w", cassettePath, err)
		}
		if !ok {
			break
		}
		for env := range gen.Facts {
			item, err := projectionWorkItemFromEnvelope(env)
			if err != nil {
				return nil, fmt.Errorf("cassette %q: %w", cassettePath, err)
			}
			items = append(items, item)
		}
		if gen.FactStreamErr != nil {
			if err := gen.FactStreamErr(); err != nil {
				return nil, fmt.Errorf("cassette %q fact stream error: %w", cassettePath, err)
			}
		}
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("cassette %q yielded no projection work items", cassettePath)
	}
	if err := assertCrossHookEdge(cassettePath, items); err != nil {
		return nil, err
	}
	return items, nil
}

// assertCrossHookEdge enforces the shared-conflict-key cassette contract from
// this package's AGENTS.md: the cassette must yield at least one edge whose
// From and To node labels are owned by two DIFFERENT projection hooks. Every
// cross-hook edge in these fixtures hangs off a loader-optional payload field
// (global_id, linked_cve_id, linked_purl, evidence_ref), so without this guard
// a cassette edit dropping those fields would leave every ordering test green
// while the scenario silently degraded to a same-hook proof.
func assertCrossHookEdge(cassettePath string, items []WorkItem) error {
	for _, item := range items {
		for _, e := range item.Edges {
			fromHook, err := owningHookForNodeKey(e.From)
			if err != nil {
				return fmt.Errorf("cassette %q: edge From %w", cassettePath, err)
			}
			toHook, err := owningHookForNodeKey(e.To)
			if err != nil {
				return fmt.Errorf("cassette %q: edge To %w", cassettePath, err)
			}
			if fromHook != toHook {
				return nil
			}
		}
	}
	return fmt.Errorf(
		"cassette %q has no cross-hook edge: every edge stays within one projection hook's node labels, "+
			"so the cassette cannot prove shared-conflict-key ordering (see schedulereplay AGENTS.md)",
		cassettePath)
}

// owningHookForNodeKey resolves a node key ("Label/ID", per Node.Key) to the
// projection hook owning its label. An unknown label is a hard error: it would
// mean a work-item mapper emitted a node this file's ownership table does not
// cover, leaving the cross-hook guard blind to it.
func owningHookForNodeKey(key string) (string, error) {
	label, _, ok := strings.Cut(key, "/")
	if !ok || label == "" {
		return "", fmt.Errorf("node key %q has no label prefix", key)
	}
	hook, ok := nodeLabelOwningHook[label]
	if !ok {
		return "", fmt.Errorf("node key %q has label %q with no owning projection hook", key, label)
	}
	return hook, nil
}

// projectionWorkItemFromEnvelope maps one recorded fact envelope to exactly one
// WorkItem, dispatching on FactKind. An envelope whose kind is not one of the
// seven this file recognizes is a hard error: silently skipping an unknown kind
// would let a cassette edit quietly stop exercising a hook without failing any
// test.
func projectionWorkItemFromEnvelope(env facts.Envelope) (WorkItem, error) {
	switch env.FactKind {
	case factKindIncidentRecord:
		return incidentRecordWorkItem(env)
	case factKindWorkItemRecord:
		return workItemRecordWorkItem(env)
	case factKindWorkItemExternalLink:
		return workItemExternalLinkWorkItem(env)
	case factKindVulnerabilityCVE:
		return vulnerabilityCVEWorkItem(env)
	case factKindVulnerabilityAffected:
		return vulnerabilityAffectedPackageWorkItem(env)
	case factKindScannerWorkerAnalysis:
		return scannerWorkerAnalysisWorkItem(env)
	case factKindVulnerabilitySupp:
		return vulnerabilitySuppressionWorkItem(env)
	default:
		return WorkItem{}, fmt.Errorf("unknown fact kind %q for projection work items", env.FactKind)
	}
}

// intentID builds the "<projection_hook>:<stable key>" IntentID the design
// requires: the hook that owns the fact, plus the fact's own StableFactKey
// (already unique per cassette.Fact.validate()).
func intentID(hook string, env facts.Envelope) string {
	return hook + ":" + env.StableFactKey
}

// incidentRecordWorkItem maps an incident.record fact (hook:
// incident_context_read_model) to its Incident node. provider_incident_id is
// schema-required (sdk/go/factschema/schema/incident.record.v1.schema.json) and
// is the node identity every work_item.external_link cross-reference joins on.
func incidentRecordWorkItem(env facts.Envelope) (WorkItem, error) {
	providerIncidentID := payloadString(env.Payload, "provider_incident_id")
	if providerIncidentID == "" {
		return WorkItem{}, fmt.Errorf("incident.record %s: missing required provider_incident_id", env.StableFactKey)
	}
	node := Node{
		Label: nodeLabelIncident,
		ID:    providerIncidentID,
		Props: map[string]string{
			"provider": payloadString(env.Payload, "provider"),
			"status":   payloadString(env.Payload, "status"),
			"title":    payloadString(env.Payload, "title"),
		},
	}
	return WorkItem{IntentID: intentID(hookIncidentContextReadModel, env), Nodes: []Node{node}}, nil
}

// workItemRecordWorkItem maps a work_item.record fact (hook:
// work_item_evidence_read_model) to its WorkItem node. work_item_key is
// schema-required (sdk/go/factschema/schema/work_item.record.v1.schema.json).
func workItemRecordWorkItem(env facts.Envelope) (WorkItem, error) {
	workItemKey := payloadString(env.Payload, "work_item_key")
	if workItemKey == "" {
		return WorkItem{}, fmt.Errorf("work_item.record %s: missing required work_item_key", env.StableFactKey)
	}
	node := Node{
		Label: nodeLabelWorkItem,
		ID:    workItemKey,
		Props: map[string]string{
			"provider":  payloadString(env.Payload, "provider"),
			"summary":   payloadString(env.Payload, "summary"),
			"status":    payloadString(env.Payload, "status_name"),
			"issueType": payloadString(env.Payload, "issue_type_name"),
		},
	}
	return WorkItem{IntentID: intentID(hookWorkItemEvidenceReadModel, env), Nodes: []Node{node}}, nil
}

// workItemExternalLinkWorkItem maps a work_item.external_link fact (hook:
// work_item_evidence_read_model) to the cross-hook edge from the incident it
// links to (Incident, owned by incident_context_read_model) to the work item
// carrying the link (WorkItem, owned by this same hook). The real
// work_item.external_link schema
// (sdk/go/factschema/schema/work_item.external_link.v1.schema.json) has no
// dedicated incident-reference field, so this fixture repurposes the schema's
// existing global_id field (a generic cross-system identifier, additionalProperties: true)
// to carry the linked incident's provider_incident_id — a plausible real link
// shape (a PagerDuty incident URL/id recorded as a Jira remote link's
// global_id), not a new field. work_item_key is required by this loader (not by
// the schema, which allows it to be null) because it is the only way this
// fixture can identify which WorkItem node the edge attaches to; a link with a
// global_id but no work_item_key is unusable cross-reference data and fails
// loudly rather than being silently dropped. A link with no global_id at all is
// valid (a purely provenance link) and yields no edge.
func workItemExternalLinkWorkItem(env facts.Envelope) (WorkItem, error) {
	workItemKey := payloadString(env.Payload, "work_item_key")
	globalID := payloadString(env.Payload, "global_id")
	item := WorkItem{IntentID: intentID(hookWorkItemEvidenceReadModel, env)}
	if globalID == "" {
		return item, nil
	}
	if workItemKey == "" {
		return WorkItem{}, fmt.Errorf(
			"work_item.external_link %s: global_id %q present but work_item_key is missing",
			env.StableFactKey, globalID)
	}
	item.Edges = []Edge{{
		From: Node{Label: nodeLabelIncident, ID: globalID}.Key(),
		Rel:  "HAS_WORK_ITEM",
		To:   Node{Label: nodeLabelWorkItem, ID: workItemKey}.Key(),
	}}
	return item, nil
}

// vulnerabilityCVEWorkItem maps a vulnerability.cve fact (hook:
// vulnerability_source_state) to its Vulnerability node. advisory_id is
// schema-required; cve_id is preferred as the node identity when present
// (sdk/go/factschema/schema/vulnerability.cve.v1.schema.json), matching how
// production impact findings prefer the CVE identity when one exists.
func vulnerabilityCVEWorkItem(env facts.Envelope) (WorkItem, error) {
	vulnID := vulnerabilityNodeID(env.Payload)
	if vulnID == "" {
		return WorkItem{}, fmt.Errorf("vulnerability.cve %s: missing both cve_id and advisory_id", env.StableFactKey)
	}
	node := Node{
		Label: nodeLabelVulnerability,
		ID:    vulnID,
		Props: map[string]string{
			"advisory_id":    payloadString(env.Payload, "advisory_id"),
			"source":         payloadString(env.Payload, "source"),
			"severity_label": payloadString(env.Payload, "severity_label"),
		},
	}
	return WorkItem{IntentID: intentID(hookVulnerabilitySourceState, env), Nodes: []Node{node}}, nil
}

// vulnerabilityAffectedPackageWorkItem maps a vulnerability.affected_package
// fact (hook: vulnerability_source_state) to its Package node plus the AFFECTS
// edge from the Vulnerability node the same hook owns. purl is optional in the
// real schema (package_id can stand alone), but this loader requires it because
// the fixture uses purl as the Package node identity (Package Manager URL is
// the one field guaranteed globally unique across ecosystems); a real
// package-id-only fact would need a different identity strategy, out of scope
// for this ordering fixture.
func vulnerabilityAffectedPackageWorkItem(env facts.Envelope) (WorkItem, error) {
	purl := payloadString(env.Payload, "purl")
	if purl == "" {
		return WorkItem{}, fmt.Errorf("vulnerability.affected_package %s: missing required (by this fixture) purl", env.StableFactKey)
	}
	vulnID := vulnerabilityNodeID(env.Payload)
	if vulnID == "" {
		return WorkItem{}, fmt.Errorf("vulnerability.affected_package %s: missing both cve_id and advisory_id", env.StableFactKey)
	}
	node := Node{
		Label: nodeLabelPackage,
		ID:    purl,
		Props: map[string]string{
			"ecosystem":    payloadString(env.Payload, "ecosystem"),
			"package_id":   payloadString(env.Payload, "package_id"),
			"package_name": payloadString(env.Payload, "package_name"),
		},
	}
	edge := Edge{From: Node{Label: nodeLabelVulnerability, ID: vulnID}.Key(), Rel: "AFFECTS", To: node.Key()}
	return WorkItem{IntentID: intentID(hookVulnerabilitySourceState, env), Nodes: []Node{node}, Edges: []Edge{edge}}, nil
}

// scannerWorkerAnalysisWorkItem maps a scanner_worker.analysis fact (hook:
// supply_chain_impact) to its Finding node plus the two cross-hook edges to the
// Vulnerability and Package nodes vulnerability_source_state owns.
// target_locator_hash is schema-required
// (sdk/go/factschema/schema/scanner_worker.analysis.v1.schema.json) and is the
// Finding node identity. The real schema carries no cve/package reference field
// on this fact kind — a scan result's link to a specific CVE and package is
// established by other reducer evidence-path machinery, not a flat payload
// field — so this fixture adds two additional (schema-legal:
// additionalProperties: true) fields, linked_cve_id and linked_purl, purely to
// give this credential-free, in-memory ordering fixture the cross-hook join a
// real reducer forms through richer machinery. This is a deliberate, minimal,
// documented deviation from "faithful to the real schema" (see the executor's
// report): it does not claim scanner_worker.analysis carries these fields in
// production. Both are optional here; a Finding with neither carries no
// outgoing edge.
func scannerWorkerAnalysisWorkItem(env facts.Envelope) (WorkItem, error) {
	locatorHash := payloadString(env.Payload, "target_locator_hash")
	if locatorHash == "" {
		return WorkItem{}, fmt.Errorf("scanner_worker.analysis %s: missing required target_locator_hash", env.StableFactKey)
	}
	node := Node{
		Label: nodeLabelFinding,
		ID:    locatorHash,
		Props: map[string]string{
			"analyzer":        payloadString(env.Payload, "analyzer"),
			"image_reference": payloadString(env.Payload, "image_reference"),
			"image_digest":    payloadString(env.Payload, "image_digest"),
			"analysis_status": payloadString(env.Payload, "analysis_status"),
			"coverage_status": payloadString(env.Payload, "coverage_status"),
		},
	}
	item := WorkItem{IntentID: intentID(hookSupplyChainImpact, env), Nodes: []Node{node}}
	if cveID := payloadString(env.Payload, "linked_cve_id"); cveID != "" {
		item.Edges = append(item.Edges, Edge{
			From: node.Key(), Rel: "DETECTS", To: Node{Label: nodeLabelVulnerability, ID: cveID}.Key(),
		})
	}
	if purl := payloadString(env.Payload, "linked_purl"); purl != "" {
		item.Edges = append(item.Edges, Edge{
			From: node.Key(), Rel: "TARGETS_PACKAGE", To: Node{Label: nodeLabelPackage, ID: purl}.Key(),
		})
	}
	return item, nil
}

// vulnerabilitySuppressionWorkItem maps a vulnerability.suppression fact (hook:
// vulnerability_suppression_admission) to its Suppression node plus the
// cross-hook SUPPRESSES edge to the Finding the supply_chain_impact hook owns.
// vulnerability.suppression carries no committed JSON Schema; its payload keys
// (suppression_id, source, justification, author, authored_at, reason,
// evidence_ref, scope{cve_id, advisory_id, package_id, purl, repository_id})
// come from the reducer decode seam instead
// (go/internal/reducer/supply_chain_suppression_decode.go). This fixture
// repurposes evidence_ref (a generic "reference to the evidence this
// suppression concerns") to carry the target_locator_hash of the finding it
// suppresses, matching decodeVulnerabilitySuppression's own suppression_id ->
// StableFactKey fallback for a blank id.
func vulnerabilitySuppressionWorkItem(env facts.Envelope) (WorkItem, error) {
	suppressionID := payloadString(env.Payload, "suppression_id")
	if suppressionID == "" {
		suppressionID = env.StableFactKey
	}
	node := Node{
		Label: nodeLabelSuppression,
		ID:    suppressionID,
		Props: map[string]string{
			"source":        payloadString(env.Payload, "source"),
			"justification": payloadString(env.Payload, "justification"),
			"reason":        payloadString(env.Payload, "reason"),
		},
	}
	item := WorkItem{IntentID: intentID(hookVulnerabilitySuppressionAdmission, env), Nodes: []Node{node}}
	if findingID := payloadString(env.Payload, "evidence_ref"); findingID != "" {
		item.Edges = []Edge{{
			From: node.Key(), Rel: "SUPPRESSES", To: Node{Label: nodeLabelFinding, ID: findingID}.Key(),
		}}
	}
	return item, nil
}

// vulnerabilityNodeID picks the Vulnerability node identity: cve_id when
// present, falling back to advisory_id. Both vulnerability.cve and
// vulnerability.affected_package carry both fields, so the same rule applied to
// either payload yields the same identity for facts describing the same
// advisory.
func vulnerabilityNodeID(payload map[string]any) string {
	if id := payloadString(payload, "cve_id"); id != "" {
		return id
	}
	return payloadString(payload, "advisory_id")
}

// payloadString reads a string field from a decoded fact payload, tolerating a
// missing key, a nil value, or a non-string value (returns "" rather than
// panicking on an unexpected JSON type).
func payloadString(payload map[string]any, key string) string {
	if payload == nil {
		return ""
	}
	v, ok := payload[key]
	if !ok || v == nil {
		return ""
	}
	s, _ := v.(string)
	return strings.TrimSpace(s)
}
