// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// RepositoryAlert is the schema-version-1 typed payload for the
// "security_alert.repository_alert" fact kind: one repository-scoped security
// alert reported by an external provider such as GitHub Dependabot. It is
// provider source evidence, not graph truth: the alert's own state (open,
// fixed, dismissed) is never itself promoted as canonical truth.
//
// It DOES contribute to two reducer surfaces. It drives the security-alert
// reconciliation read surface, and it also seeds supply-chain-impact findings
// (appendSecurityAlertImpactFindings) on the reducer's CanonicalWrites path.
// The typed decode is output-preserving on both: it mirrors the existing wire
// payload exactly, so it changes neither which impact findings a valid alert
// seeds nor what is promoted, and it introduces no field that turns alert
// state into new supply-chain-impact truth. The only behavior change is that a
// malformed alert (missing the required RepositoryID) now dead-letters as a
// per-fact input_invalid quarantine on both surfaces instead of seeding an
// empty-identity finding or a blank-repository reconciliation row.
//
// RepositoryID is the only required field. Both collector paths that emit this
// kind always set it: the per-repository GitHub Dependabot envelope
// (securityalerts.NewGitHubDependabotAlertEnvelope) derives it from the
// committed scope (falling back to the org-wide RepositoryID), and it is the
// key the collector's stable-fact-key hash is built on. On the reducer side it
// is the repository/provider join anchor for reconciliation
// (SecurityAlertReconciliationFactFilter keys on it) and the gate the
// supply-chain-impact seeder requires before a security alert can contribute a
// finding (securityAlertCanSeedImpact). A collector regression that drops the
// key now dead-letters as input_invalid instead of producing a
// blank-repository reconciliation row or an empty-identity impact finding.
//
// Every other field is OPTIONAL. The reducer reads all of them, but two
// distinct collector paths emit this one kind with different field coverage:
// the Dependabot envelope always sets provider/advisory/dependency fields but
// never the collection-coverage or source_freshness fields, while the
// alert-runtime source path (securityalerts/alertruntime/source.go) additionally
// stamps repository_name, source_freshness, and the collection_* coverage
// fields. Requiring any of those would dead-letter half of this kind's real
// traffic, so a present-but-empty or absent value on any optional field is a
// valid decode that the reducer's existing read side already tolerates.
//
// Normalization discipline: the reducer applies its own trim/drop-empty
// normalization to the CVSS, EPSS, and CWEs container fields after decode (the
// same securityAlertMap/securityAlertStringMap/securityAlertStringMapSlice
// helpers it used pre-typing), so this struct models the raw shapes the
// collector emits and does not itself prune empty keys — the decode stays a
// faithful mirror of the wire payload and the reducer's post-decode
// normalization keeps its output byte-identical.
type RepositoryAlert struct {
	// RepositoryID is the per-repository scope the provider alert belongs to.
	// Required — the reducer's repository/provider join anchor for both the
	// reconciliation read surface and the supply-chain-impact seed gate.
	RepositoryID string `json:"repository_id"`

	// Provider names the alert source (for example "github_dependabot").
	// Optional: always set by the current collectors.
	Provider *string `json:"provider,omitempty"`

	// ProviderAlertID is the provider-stable alert identifier. Optional:
	// always set by the Dependabot collector.
	ProviderAlertID *string `json:"provider_alert_id,omitempty"`

	// ProviderAlertNumber is the provider's numeric alert identifier. Optional:
	// set by the Dependabot collector; absent decodes to zero, matching the
	// reducer's pre-typing securityAlertInt64 fallback.
	ProviderAlertNumber *int64 `json:"provider_alert_number,omitempty"`

	// ProviderState is the provider-reported alert state ("open", "fixed",
	// "dismissed", "auto_dismissed", ...). Optional: the reducer lower-cases it
	// and treats an empty value as an active, non-terminal alert.
	ProviderState *string `json:"provider_state,omitempty"`

	// RepositoryName is the human-readable repository name. Optional: set by
	// the org alert-runtime source path; the reducer falls back to deriving one
	// from RepositoryID when absent.
	RepositoryName *string `json:"repository_name,omitempty"`

	// PackageID is the canonical cross-source package identity the collector
	// derives from the alert's affected package. Optional.
	PackageID *string `json:"package_id,omitempty"`

	// Ecosystem is the affected package's ecosystem ("npm", "pypi", ...).
	// Optional.
	Ecosystem *string `json:"ecosystem,omitempty"`

	// PackageName is the affected package's normalized name. Optional.
	PackageName *string `json:"package_name,omitempty"`

	// ManifestPath is the manifest/lockfile path the provider attributes the
	// alert to. Optional.
	ManifestPath *string `json:"manifest_path,omitempty"`

	// DependencyScope classifies the dependency's declared scope (runtime,
	// development, ...). Optional.
	DependencyScope *string `json:"dependency_scope,omitempty"`

	// Relationship classifies how the dependency was declared (direct,
	// transitive, ...). Optional.
	Relationship *string `json:"relationship,omitempty"`

	// GHSAID is a single GHSA advisory identifier. Optional: the reducer merges
	// it with GHSAIDs (scalar + slice) into one sorted, de-duplicated set,
	// matching the pre-typing payloadStrings("ghsa_id", "ghsa_ids") read.
	GHSAID *string `json:"ghsa_id,omitempty"`

	// GHSAIDs lists the GHSA advisory identifiers. Optional: the current
	// collectors emit this slice form; the reducer merges it with GHSAID.
	GHSAIDs []string `json:"ghsa_ids,omitempty"`

	// CVEID is a single CVE identifier. Optional: merged with CVEIDs the same
	// way GHSAID is merged with GHSAIDs.
	CVEID *string `json:"cve_id,omitempty"`

	// CVEIDs lists the CVE identifiers. Optional: the current collectors emit
	// this slice form.
	CVEIDs []string `json:"cve_ids,omitempty"`

	// VulnerableRange is the provider's vulnerable version range expression.
	// Optional.
	VulnerableRange *string `json:"vulnerable_range,omitempty"`

	// PatchedVersion is the provider's first patched version. Optional.
	PatchedVersion *string `json:"patched_version,omitempty"`

	// Severity is the provider-reported severity label. Optional: the reducer
	// lower-cases it.
	Severity *string `json:"severity,omitempty"`

	// CVSS carries the provider's CVSS score/vector object. Optional: an open
	// object (score, vector) the reducer normalizes after decode.
	CVSS map[string]any `json:"cvss,omitempty"`

	// EPSS carries the provider's EPSS percentage/percentile object. Optional:
	// a string-valued object the reducer normalizes after decode.
	EPSS map[string]string `json:"epss,omitempty"`

	// CWEs lists the provider's CWE identifier/name objects. Optional: a slice
	// of string-valued objects the reducer normalizes after decode.
	CWEs []map[string]string `json:"cwes,omitempty"`

	// Summary is the provider advisory summary. Optional.
	Summary *string `json:"summary,omitempty"`

	// Description is the provider advisory description. Optional.
	Description *string `json:"description,omitempty"`

	// SourceURL is the provider's alert URL. Optional.
	SourceURL *string `json:"source_url,omitempty"`

	// CreatedAt is the provider's alert creation timestamp (RFC 3339).
	// Optional.
	CreatedAt *string `json:"created_at,omitempty"`

	// UpdatedAt is the provider's alert last-updated timestamp (RFC 3339).
	// Optional: the reducer parses it to decide whether owned dependency
	// evidence is newer than the provider alert.
	UpdatedAt *string `json:"updated_at,omitempty"`

	// FixedAt is the provider's alert fixed timestamp (RFC 3339). Optional.
	FixedAt *string `json:"fixed_at,omitempty"`

	// DismissedAt is the provider's alert dismissed timestamp (RFC 3339).
	// Optional.
	DismissedAt *string `json:"dismissed_at,omitempty"`

	// SourceFreshness is the collector's freshness classification for the
	// alert's coverage ("active", "partial", ...). Optional: set by the
	// alert-runtime source path; the reducer derives a fallback from
	// CollectionCoverageState when absent.
	SourceFreshness *string `json:"source_freshness,omitempty"`

	// CollectionCoverageState is the collector's coverage-completeness state
	// ("complete", "incomplete"). Optional: set by the alert-runtime source
	// path.
	CollectionCoverageState *string `json:"collection_coverage_state,omitempty"`

	// CollectionTruncated reports whether provider paging was truncated.
	// Optional: set by the alert-runtime source path.
	CollectionTruncated *bool `json:"collection_truncated,omitempty"`

	// CollectionPagesFetched is the number of provider pages fetched. Optional:
	// set by the alert-runtime source path.
	CollectionPagesFetched *int64 `json:"collection_pages_fetched,omitempty"`

	// CollectionStateFilter is the provider alert state filter the collector
	// requested ("open", ...). Optional: set by the alert-runtime source path.
	CollectionStateFilter *string `json:"collection_state_filter,omitempty"`

	// CollectionIncompleteReasons lists why coverage was incomplete. Optional:
	// set by the alert-runtime source path.
	CollectionIncompleteReasons []string `json:"collection_incomplete_reasons,omitempty"`

	// CorrelationAnchors lists durable non-empty identity anchors. Optional.
	CorrelationAnchors []string `json:"correlation_anchors,omitempty"`

	// CollectorInstanceID is the collector boundary token. Optional.
	CollectorInstanceID *string `json:"collector_instance_id,omitempty"`
}
