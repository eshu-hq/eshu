// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package exposure

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

// Severity is the closed, lowercase severity vocabulary a sink contributes to an
// exposure finding. The string values match the normalized severity strings used
// elsewhere in the query surface (internal/exports.Severity) so a sink severity
// is wire-stable across the API, MCP, and console.
type Severity string

const (
	// SeverityCritical is the highest baseline severity (privileged escalation).
	SeverityCritical Severity = "critical"
	// SeverityHigh is a high baseline severity (effective privileged action,
	// secret read, internet egress).
	SeverityHigh Severity = "high"
	// SeverityMedium is a medium baseline severity (data-store access).
	SeverityMedium Severity = "medium"
	// SeverityLow is the lowest baseline severity.
	SeverityLow Severity = "low"
)

// SinkKind names a category in the closed, curated cloud-sink catalog. A sink is
// the privileged or externally-observable terminal of a code-to-cloud exposure
// path: the point where untrusted input, once it arrives, causes harm. Unlike
// code-only taint tools whose sink is always an AST node, an Eshu sink can be a
// correlated cloud fact (an IAM action, a reachable secret), which is the
// differentiator described in #2704.
type SinkKind string

const (
	// SinkIAMPrivilegedAction is reaching an IAM principal that can perform,
	// escalate to, or assume a privileged cloud identity/action.
	SinkIAMPrivilegedAction SinkKind = "iam_privileged_action"
	// SinkSecretReference is reaching a node that can read a secret.
	SinkSecretReference SinkKind = "secret_reference"
	// SinkSQLTable is reaching a SQL table (data read/write).
	SinkSQLTable SinkKind = "sql_table"
	// SinkShellExec is reaching a shell/command-execution sink.
	SinkShellExec SinkKind = "shell_exec"
	// SinkInternetEndpoint is reaching an endpoint exposed to the public internet
	// (egress/exfiltration terminal), modeled by the security-group graph.
	SinkInternetEndpoint SinkKind = "internet_exposed_endpoint"
	// SinkConfigSecurityKey is untrusted data reaching a security-relevant
	// configuration key such as TLS verification or filesystem permissions.
	SinkConfigSecurityKey SinkKind = "config_security_key"
	// SinkIaCMisconfiguration is untrusted or templated data reaching an IaC
	// configuration that can create a security misconfiguration.
	SinkIaCMisconfiguration SinkKind = "iac_misconfiguration"
)

// SinkPredicate is a target-node property constraint that must hold for an edge
// to qualify as a sink. Values are compared as strings; callers normalize graph
// scalars (e.g. a boolean is_internet) to "true"/"false" before matching.
type SinkPredicate struct {
	// Key is the target-node property name.
	Key string
	// Value is the required string value of that property.
	Value string
}

// SinkSpec is one curated cloud-sink recognition rule: the sink kind it
// recognizes, the graph relationship + target node label that qualify a node as
// reaching that sink, optional target-property predicates, the baseline severity
// reaching it contributes, and a provenance citation to where the edge is
// authored. Specs are closed-vocabulary and declarative, mirroring
// reducer.iamEscalationCatalog.
type SinkSpec struct {
	// Kind is the closed-vocabulary sink category.
	Kind SinkKind
	// DisplayName is the human-facing label for surfaces and findings.
	DisplayName string
	// Relationship is the graph relationship type whose presence (to TargetLabel)
	// marks the source node as reaching this sink. Empty for non-graph-backed
	// kinds.
	Relationship string
	// TargetLabel is the node label the qualifying relationship must point to.
	// Empty for non-graph-backed kinds.
	TargetLabel string
	// TargetPredicates are property constraints on the target node that must all
	// hold for the edge to qualify. Empty means any target with the label.
	TargetPredicates []SinkPredicate
	// BaselineSeverity is the severity reaching this sink contributes before any
	// path-specific aggravation (e.g. a missing auth check).
	BaselineSeverity Severity
	// GraphBacked is false for a sink kind that is part of the closed vocabulary
	// but has no materialized graph edge yet. Such specs declare no
	// relationship/target and are never matched by MatchSink.
	GraphBacked bool
	// Provenance cites where the qualifying edge/label is authored (a reducer or
	// graph file), or — for non-graph-backed kinds — the follow-up that will
	// materialize it. It keeps the catalog auditable.
	Provenance string
}

// sinkCatalog is the curated, documented set of cloud-sink recognition rules
// this Level 1 capability uses. Every entry's relationship, target, and
// provenance are verified against the reducer/graph materializers cited inline.
// The catalog is intentionally conservative and closed: a sink is only one of
// these categories, recognized only by a declared edge. It is a
// package-level value built once; callers MUST NOT mutate it.
var sinkCatalog = []SinkSpec{
	// IAM-privileged action sinks. The three IAM edges all terminate on a
	// :CloudResource node (the resource/role/identity an effective grant reaches).
	{
		Kind:             SinkIAMPrivilegedAction,
		DisplayName:      "IAM effective privileged action",
		Relationship:     "CAN_PERFORM",
		TargetLabel:      "CloudResource",
		BaselineSeverity: SeverityHigh,
		GraphBacked:      true,
		Provenance:       "reducer/iam_can_perform_materialization.go (principal :CloudResource -[:CAN_PERFORM]-> resource :CloudResource)",
	},
	{
		Kind:             SinkIAMPrivilegedAction,
		DisplayName:      "IAM privilege escalation",
		Relationship:     "CAN_ESCALATE_TO",
		TargetLabel:      "CloudResource",
		BaselineSeverity: SeverityCritical,
		GraphBacked:      true,
		Provenance:       "reducer/iam_escalation_materialization.go (principal -[:CAN_ESCALATE_TO]-> target :CloudResource)",
	},
	{
		Kind:             SinkIAMPrivilegedAction,
		DisplayName:      "IAM role assumption",
		Relationship:     "CAN_ASSUME",
		TargetLabel:      "CloudResource",
		BaselineSeverity: SeverityHigh,
		GraphBacked:      true,
		Provenance:       "reducer/iam_can_assume_edge_rows.go (principal -[:CAN_ASSUME]-> role/user :CloudResource)",
	},
	// Secret-reference sink: a node granted read on a secret metadata path.
	{
		Kind:             SinkSecretReference,
		DisplayName:      "Secret read access",
		Relationship:     "SECRETS_IAM_GRANTS_SECRET_READ",
		TargetLabel:      "SecretsIAMSecretMetadataPath",
		BaselineSeverity: SeverityHigh,
		GraphBacked:      true,
		Provenance:       "reducer/secrets_iam_graph_projection_extract.go (-[:SECRETS_IAM_GRANTS_SECRET_READ]-> :SecretsIAMSecretMetadataPath)",
	},
	// SQL table sink: a function that queries a SQL table. The SQL relationship
	// materializer promotes parser embedded-query evidence into
	// Function-[:QUERIES_TABLE]->SqlTable only when the referenced table resolves
	// unambiguously.
	{
		Kind:             SinkSQLTable,
		DisplayName:      "SQL table access",
		Relationship:     "QUERIES_TABLE",
		TargetLabel:      "SqlTable",
		BaselineSeverity: SeverityMedium,
		GraphBacked:      true,
		Provenance:       "reducer/sql_relationship_materialization.go and storage/cypher/edge_writer_sql.go (Function-[:QUERIES_TABLE]->SqlTable)",
	},
	// Internet-exposed endpoint sink: a security-group rule that reaches the public
	// internet (0.0.0.0/0 or ::/0), captured by the is_internet flag on the CIDR
	// block so no raw address is compared.
	{
		Kind:             SinkInternetEndpoint,
		DisplayName:      "Internet-exposed endpoint",
		Relationship:     "TO",
		TargetLabel:      "CidrBlock",
		TargetPredicates: []SinkPredicate{{Key: "is_internet", Value: "true"}},
		BaselineSeverity: SeverityHigh,
		GraphBacked:      true,
		Provenance:       "reducer/security_group_reachability.go (:SecurityGroupRule -[:TO]-> :CidrBlock{is_internet:true})",
	},
	// Shell-exec sink: a function that constructs a command through a recognized
	// Go, Python, or Node command-execution API. The materializer records
	// structural call-site metadata only, never command text or arguments.
	{
		Kind:             SinkShellExec,
		DisplayName:      "Shell/command execution",
		Relationship:     "EXECUTES_SHELL",
		TargetLabel:      "ShellCommand",
		BaselineSeverity: SeverityCritical,
		GraphBacked:      true,
		Provenance:       "reducer/shell_exec_materialization.go and storage/cypher/edge_writer_shell_exec.go (Function-[:EXECUTES_SHELL]->ShellCommand)",
	},
	// Config/IaC sinks are closed-vocabulary #3191 fixtures but intentionally
	// non-GraphBacked for now. The current value-flow fixpoint graph loader only
	// reaches Function-anchored cloud-action permission sinks; it has no
	// Function-anchored config or IaC materializer path yet.
	{
		Kind:             SinkConfigSecurityKey,
		DisplayName:      "security-relevant config key",
		BaselineSeverity: SeverityHigh,
		GraphBacked:      false,
		Provenance:       "#3191 non-GraphBacked fixture: keep unresolved until a Function-anchored config sink materializer and ValueFlowFixpointEvidenceLoader path exist",
	},
	{
		Kind:             SinkIaCMisconfiguration,
		DisplayName:      "IaC misconfiguration",
		BaselineSeverity: SeverityHigh,
		GraphBacked:      false,
		Provenance:       "#3191 non-GraphBacked fixture: keep unresolved until Terraform/Helm misconfiguration evidence has a Function-anchored materializer and ValueFlowFixpointEvidenceLoader path",
	},
}

// SinkCatalog returns a defensive copy of the curated cloud-sink catalog so
// callers can range over it without risking mutation of the package-level value.
// The per-spec TargetPredicates slice is deep-copied so a caller cannot mutate
// the shared backing array of a predicate in place.
func SinkCatalog() []SinkSpec {
	out := make([]SinkSpec, len(sinkCatalog))
	copy(out, sinkCatalog)
	for i := range out {
		out[i].TargetPredicates = clonePredicates(out[i].TargetPredicates)
	}
	return out
}

// GraphBackedSinkSpecs returns only the catalog specs that recognize a
// materialized graph edge. The reachability tracer ranges over these to decide
// whether a traversed edge terminates on a sink; non-graph-backed kinds are
// reported unresolved by the tracer, never matched here. TargetPredicates are
// deep-copied for the same reason as SinkCatalog.
func GraphBackedSinkSpecs() []SinkSpec {
	out := make([]SinkSpec, 0, len(sinkCatalog))
	for _, spec := range sinkCatalog {
		if spec.GraphBacked {
			spec.TargetPredicates = clonePredicates(spec.TargetPredicates)
			out = append(out, spec)
		}
	}
	return out
}

// clonePredicates returns a deep copy of a predicate slice (nil for empty) so
// the package-level catalog's backing array is never shared with callers.
func clonePredicates(preds []SinkPredicate) []SinkPredicate {
	if len(preds) == 0 {
		return nil
	}
	out := make([]SinkPredicate, len(preds))
	copy(out, preds)
	return out
}

// MatchSink reports the first catalog spec recognizing an edge of relationship
// rel that points to a node labeled targetLabel carrying targetProps. Only
// graph-backed specs match; a non-graph-backed kind (shell-exec) never matches,
// so the tracer cannot fabricate a sink the graph does not model. ok is false
// when no spec qualifies. Matching is deterministic in catalog order.
func MatchSink(rel, targetLabel string, targetProps map[string]string) (SinkSpec, bool) {
	for _, spec := range sinkCatalog {
		if !spec.GraphBacked {
			continue
		}
		if spec.Relationship != rel || spec.TargetLabel != targetLabel {
			continue
		}
		if !predicatesSatisfied(spec.TargetPredicates, targetProps) {
			continue
		}
		// Clone the predicate slice so a caller that normalizes or edits the
		// returned spec's TargetPredicates cannot mutate the package-level
		// catalog, matching SinkCatalog/GraphBackedSinkSpecs.
		spec.TargetPredicates = clonePredicates(spec.TargetPredicates)
		return spec, true
	}
	return SinkSpec{}, false
}

// predicatesSatisfied reports whether every predicate holds against props. A
// missing property fails the predicate (conservative: an unproven constraint is
// not a match), which keeps a non-internet or unlabeled CIDR block from
// qualifying as an internet sink.
func predicatesSatisfied(predicates []SinkPredicate, props map[string]string) bool {
	for _, pred := range predicates {
		if props[pred.Key] != pred.Value {
			return false
		}
	}
	return true
}

// sinkCatalogVersionGolden pins the current content hash of the cloud-sink
// catalog. The well-formedness test fails when the catalog changes without a
// deliberate update to this constant, implementing the taintModelVersion
// discipline: a curated edit trips downstream re-evaluation.
const sinkCatalogVersionGolden = "1e6fb5ee59b81b38610afe33d1680fdbaf2b5d1540b4a4a97534575f922d14f9"

// SinkCatalogVersion returns a deterministic content hash over the curated
// cloud-sink catalog. Any change to the catalog (added, removed, or edited spec)
// changes this value so cached reachability findings can be invalidated and
// re-evaluated. The value is stable across process runs and independent of Go
// map iteration order.
func SinkCatalogVersion() string {
	return hashSinkSpecs(sinkCatalog)
}

// hashSinkSpecs computes the canonical content hash of a sink-spec slice. Each
// spec is serialized field-by-field into a stable line (predicates sorted), the
// lines are sorted, and the joined text is SHA-256 hashed. Sorting makes the
// hash order-independent so a reordering of equivalent entries does not churn the
// version, while any field change does.
func hashSinkSpecs(specs []SinkSpec) string {
	lines := make([]string, 0, len(specs))
	for _, spec := range specs {
		preds := make([]string, 0, len(spec.TargetPredicates))
		for _, pred := range spec.TargetPredicates {
			// Separate key/value with the unit separator and join predicates with
			// the group separator below so a key or value containing a printable
			// delimiter cannot collide with the serialization layout.
			preds = append(preds, pred.Key+"\x1f"+pred.Value)
		}
		sort.Strings(preds)
		lines = append(lines, strings.Join([]string{
			string(spec.Kind),
			spec.DisplayName,
			spec.Relationship,
			spec.TargetLabel,
			strings.Join(preds, "\x1d"),
			string(spec.BaselineSeverity),
			fmt.Sprintf("%t", spec.GraphBacked),
			spec.Provenance,
		}, "\x1f"))
	}
	sort.Strings(lines)
	sum := sha256.Sum256([]byte(strings.Join(lines, "\x1e")))
	return hex.EncodeToString(sum[:])
}
