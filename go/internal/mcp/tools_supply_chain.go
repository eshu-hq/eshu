// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

func supplyChainTools() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        "get_vulnerability_scanner_read_contract",
			Description: "Return the API/MCP vulnerability scanner read contract, including supported filters, unsupported filters, route consistency rules, backing read models, remediation packet schema, and missing-evidence semantics.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"route": map[string]any{
						"type":        "string",
						"description": "Optional route slice to inspect.",
						"enum":        []string{"impact_findings", "impact_count", "impact_inventory", "impact_explain", "security_alert_reconciliations", "security_alert_count", "security_alert_inventory", "scanner_report"},
					},
				},
			},
		},
		{
			Name:        "list_container_image_identities",
			Description: "List reducer-owned container image identity facts by digest, image reference, source repository bridge, OCI repository, or outcome. Populated by the opt-in oci_registry collector (off in a default deploy; enable with ESHU_COLLECTOR_INSTANCES_JSON plus container-registry credentials), so a default git-only deploy returns an empty page.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"digest": map[string]any{
						"type":        "string",
						"description": "Image digest such as sha256:...",
					},
					"image_ref": map[string]any{
						"type":        "string",
						"description": "Original image reference observed in source or runtime evidence.",
					},
					"repository_id": map[string]any{
						"type":        "string",
						"description": "OCI repository identity such as oci-registry://registry.example/team/api.",
					},
					"source_repository_id": map[string]any{
						"type":        "string",
						"description": "source repository id or selector for bridge reads; this is not an OCI image repository identity.",
					},
					"outcome": map[string]any{
						"type":        "string",
						"description": "Optional reducer identity outcome filter.",
						"enum":        []string{"exact_digest", "tag_resolved"},
					},
					"after_identity_id": map[string]any{
						"type":        "string",
						"description": "Identity ID from next_cursor when continuing a truncated page.",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum identity rows to return.",
						"default":     50,
						"minimum":     1,
						"maximum":     200,
					},
				},
			},
		},
		{
			Name:        "list_container_image_tag_history",
			Description: "List the bounded, ordered ContainerImageTagObservation history captured for one repository_id+tag (issue #5459): what digest the tag was first observed as, and the order its digests changed. repository_id and tag are both required; the server composes the image_ref anchor from them. A tag that flips back to a previously observed digest (A -> B -> A) collapses onto the same observation node rather than producing a new event, and first_observed_at is a set-once value that holds the FIRST projected observation rather than a full chronological event log -- see the route's doc comment for both limitations. Populated by the opt-in oci_registry collector (off in a default deploy; enable with ESHU_COLLECTOR_INSTANCES_JSON plus container-registry credentials), so a default git-only deploy returns an empty page.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"repository_id": map[string]any{
						"type":        "string",
						"description": "OCI repository identity such as oci-registry://registry.example/team/api. Required; must carry the oci-registry:// prefix.",
					},
					"tag": map[string]any{
						"type":        "string",
						"description": "Tag observed for the image, such as 1.0.0. Required.",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum tag-observation rows to return.",
						"default":     50,
						"minimum":     1,
						"maximum":     200,
					},
					"offset": map[string]any{
						"type":        "integer",
						"description": "Row offset for continuation; use next_cursor.offset from a truncated page.",
						"default":     0,
						"minimum":     0,
					},
				},
			},
		},
		{
			Name:        "list_supply_chain_impact_findings",
			Description: "List reducer-owned vulnerability impact findings by CVE, package, repository, image digest, or impact status. The default precise profile requires supported exact-version evidence such as npm, Maven, Cargo, Pub pubspec.lock, NuGet, or Swift Package.resolved. Each row carries `vulnerable_range`, a reachability envelope with states such as reachable, not_called, unknown, unavailable, and missing_evidence, and an advisory-only remediation block (issue #595). Reachability does not change impact truth; JavaScript/TypeScript parser/SCIP package API evidence is partial prioritization evidence, and not_called is emitted only when an ecosystem-specific scanner proves stronger semantics. Suppression decisions (VEX, operator-policy, provider dismissal evidence) are attached to each row; set include_suppressed=true to surface findings hidden by operator suppression and use suppression_state to filter by a specific decision. Findings are seeded by the opt-in vulnerability_intelligence and security_alert collectors (off in a default deploy; enable with ESHU_COLLECTOR_INSTANCES_JSON plus advisory-feed or provider credentials), so a default git-only deploy returns an empty page whose readiness envelope reports not_configured rather than a false fresh-zero.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"cve_id": map[string]any{
						"type":        "string",
						"description": "CVE or advisory identifier to inspect.",
					},
					"advisory_id": map[string]any{
						"type":        "string",
						"description": "Exact source advisory identifier such as GHSA, OSV, GLAD, vendor advisory, or CVE id.",
					},
					"ghsa_id": map[string]any{
						"type":        "string",
						"description": "GHSA advisory identifier alias for advisory_id.",
					},
					"osv_id": map[string]any{
						"type":        "string",
						"description": "OSV advisory identifier alias for advisory_id.",
					},
					"package_id": map[string]any{
						"type":        "string",
						"description": "Normalized package identity such as pkg:npm/example.",
					},
					"repository_id": map[string]any{
						"type":        "string",
						"description": "Canonical repository id or human repository selector: name, repo slug, indexed path, local path, or remote URL.",
					},
					"subject_digest": map[string]any{
						"type":        "string",
						"description": "Image or artifact digest from SBOM/runtime evidence.",
					},
					"image_ref": map[string]any{
						"type":        "string",
						"description": "Exact image reference stored on reducer-owned impact findings.",
					},
					"impact_status": map[string]any{
						"type":        "string",
						"description": "Optional reducer impact status filter.",
						"enum":        []string{"affected_exact", "affected_derived", "possibly_affected", "not_affected_known_fixed", "unknown_impact"},
					},
					"ecosystem": map[string]any{
						"type":        "string",
						"description": "Package ecosystem from reducer-owned impact facts, such as npm, maven, cargo, nuget, pypi, rubygems, swift, or rpm.",
					},
					"workload_id": map[string]any{
						"type":        "string",
						"description": "Reducer-admitted workload anchor. Missing runtime mapping remains missing evidence.",
					},
					"service_id": map[string]any{
						"type":        "string",
						"description": "Reducer-admitted service anchor derived from workload/service evidence.",
					},
					"environment": map[string]any{
						"type":        "string",
						"description": "Reducer-admitted environment anchor. Environment names are not inferred from tags or repository names. Normalization follows the environment-alias contract.",
					},
					"severity": map[string]any{
						"type":        "string",
						"description": "CVSS-derived severity bucket for impact findings.",
						"enum":        []string{"critical", "high", "medium", "low", "none"},
					},
					"profile": map[string]any{
						"type":        "string",
						"description": "Detection profile filter. precise (default) returns only findings backed by an exact installed-version anchor; comprehensive also returns range-only, SBOM/CPE-derived, malformed, and missing-version rows. Unsupported ecosystems remain readiness gaps.",
						"enum":        []string{"precise", "comprehensive"},
						"default":     "precise",
					},
					"priority_bucket": map[string]any{
						"type":        "string",
						"description": "Optional reducer triage priority filter. Priority explains urgency and does not change impact truth.",
						"enum":        []string{"critical", "high", "medium", "low", "informational"},
					},
					"min_priority_score": map[string]any{
						"type":        "integer",
						"description": "Minimum reducer priority score from 0 through 100. Zero is the default no-op value and does not bound a request by itself.",
						"default":     0,
						"minimum":     0,
						"maximum":     100,
					},
					"sort": map[string]any{
						"type":        "string",
						"description": "Optional result ordering. Priority sorts are secondary triage views over reducer-owned impact facts.",
						"enum":        []string{"finding_id", "priority", "priority_score_desc", "priority_score_asc"},
					},
					"suppression_state": map[string]any{
						"type":        "string",
						"description": "Optional reducer suppression-state filter. Operator-asserted suppressions (not_affected, accepted_risk, false_positive, ignored) require include_suppressed=true to appear.",
						"enum":        []string{"active", "not_affected", "accepted_risk", "false_positive", "ignored", "expired", "provider_dismissed", "scope_mismatch"},
					},
					"include_suppressed": map[string]any{
						"type":        "boolean",
						"description": "Include findings hidden by operator-asserted VEX or policy suppression (not_affected, accepted_risk, false_positive, ignored). Expired, provider-dismissed, and scope-mismatched findings are visible regardless because they keep audit signal.",
						"default":     false,
					},
					"after_finding_id": map[string]any{
						"type":        "string",
						"description": "Finding ID from next_cursor when continuing a truncated page.",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum impact rows to return.",
						"default":     50,
						"minimum":     1,
						"maximum":     200,
					},
				},
			},
		},
		{
			Name:        "list_advisory_evidence",
			Description: "List source-only advisory evidence by CVE, advisory, package, repository, service, or workload. Repository/service/workload scopes derive advisory anchors from reducer-owned impact findings only; provider-alert-only rows are not promoted into owned vulnerability truth.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"cve_id": map[string]any{
						"type":        "string",
						"description": "CVE identifier to inspect across advisory sources.",
					},
					"advisory_id": map[string]any{
						"type":        "string",
						"description": "Source advisory identifier such as GHSA, OSV, GLAD, NVD/CVE, or vendor advisory id.",
					},
					"package_id": map[string]any{
						"type":        "string",
						"description": "Normalized package identity such as pkg:npm/example.",
					},
					"repository_id": map[string]any{
						"type":        "string",
						"description": "Canonical repository id or human repository selector: name, repo slug, indexed path, local path, or remote URL. The API resolves this to reducer-owned impact findings before reading advisory source facts.",
					},
					"service_id": map[string]any{
						"type":        "string",
						"description": "Reducer-admitted service anchor used to select impact findings before reading advisory source facts.",
					},
					"workload_id": map[string]any{
						"type":        "string",
						"description": "Reducer-admitted workload anchor used to select impact findings before reading advisory source facts.",
					},
					"source": map[string]any{
						"type":        "string",
						"description": "Optional source filter such as ghsa, osv, nvd, glad, first_epss, or cisa_kev.",
					},
					"after_advisory_key": map[string]any{
						"type":        "string",
						"description": "Advisory key from next_cursor when continuing a truncated page.",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum canonical advisory rows to return.",
						"default":     50,
						"minimum":     1,
						"maximum":     200,
					},
				},
			},
		},
		{
			Name:        "explain_supply_chain_impact",
			Description: "Explain one reducer-owned vulnerability finding or bounded advisory/package/repository/image/workload/service path with evidence, anchors, remediation, freshness, missing-evidence reasons, and ambiguous-scope refusal envelopes. The remediation block reports current observed version, vulnerable range, first patched version, whether the manifest range admits that fix, direct/transitive designation, parent package needed for a transitive upgrade, and an exact/partial/unknown confidence. Remediation is strictly advisory; Eshu does not auto-open pull requests.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"finding_id": map[string]any{
						"type":        "string",
						"description": "Exact reducer-owned finding id. Preferred when known.",
					},
					"advisory_id": map[string]any{
						"type":        "string",
						"description": "Advisory identifier such as GHSA, OSV, GLAD, vendor advisory, or CVE id.",
					},
					"cve_id": map[string]any{
						"type":        "string",
						"description": "CVE identifier when advisory_id is not the canonical CVE field.",
					},
					"package_id": map[string]any{
						"type":        "string",
						"description": "Normalized package identity such as pkg:npm/example.",
					},
					"repository_id": map[string]any{
						"type":        "string",
						"description": "Repository identifier from package consumption evidence.",
					},
					"subject_digest": map[string]any{
						"type":        "string",
						"description": "Image or artifact digest from SBOM/runtime evidence.",
					},
					"image_ref": map[string]any{
						"type":        "string",
						"description": "Exact image reference stored on reducer-owned impact findings.",
					},
					"workload_id": map[string]any{
						"type":        "string",
						"description": "Reducer-admitted workload anchor. Missing runtime mapping remains missing evidence.",
					},
					"service_id": map[string]any{
						"type":        "string",
						"description": "Reducer-admitted service anchor derived from workload/service evidence.",
					},
				},
			},
		},
		{
			Name:        "list_security_alert_reconciliations",
			Description: "List reducer-owned provider security alert reconciliations by repository, provider, package, CVE, or GHSA anchor while keeping provider state separate from Eshu impact state. Rows expose Eshu-owned package evidence under eshu_package, including observed_version and missing_evidence, and include reason_code plus structured missing_evidence details for provider_only, stale, unsupported, and ambiguous outcomes. Built from provider alert facts emitted by the opt-in security_alert collector (off in a default deploy; enable with ESHU_COLLECTOR_INSTANCES_JSON plus provider credentials), so a default git-only deploy returns an empty page.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"repository_id": map[string]any{
						"type":        "string",
						"description": "Canonical repository id or human repository selector: name, repo slug, indexed path, local path, or remote URL.",
					},
					"provider": map[string]any{
						"type":        "string",
						"description": "Provider source such as github_dependabot.",
					},
					"package_id": map[string]any{
						"type":        "string",
						"description": "Normalized package identity such as npm://registry.npmjs.org/example.",
					},
					"cve_id": map[string]any{
						"type":        "string",
						"description": "CVE identifier reported by the provider alert.",
					},
					"ghsa_id": map[string]any{
						"type":        "string",
						"description": "GHSA identifier reported by the provider alert.",
					},
					"provider_state": map[string]any{
						"type":        "string",
						"description": "Provider-reported alert state filter for an anchored request.",
						"enum":        []string{"open", "fixed", "dismissed", "auto_dismissed"},
					},
					"reconciliation_status": map[string]any{
						"type":        "string",
						"description": "Reducer comparison filter for an anchored request.",
						"enum":        []string{"matched", "unmatched", "stale", "dismissed", "fixed", "provider_only", "unsupported", "ambiguous"},
					},
					"after_reconciliation_id": map[string]any{
						"type":        "string",
						"description": "Reconciliation ID from next_cursor when continuing a truncated page.",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum reconciliation rows to return.",
						"default":     50,
						"minimum":     1,
						"maximum":     200,
					},
				},
			},
		},
		{
			Name:        "list_sbom_attestation_attachments",
			Description: "List reducer-owned SBOM and attestation attachment evidence by repository, workload, service, image digest, or document identity. Inspect attachment_scope and missing_evidence before treating parse-only rows as image evidence. SBOM warning summaries are returned as a bounded preview with aggregate warning_summary_count and warning_summaries_truncated. Each row also carries dependency_relationships (declared dependency edges between components) and external_references (advisory/website/VCS links), each bounded to a write-time cap per document with an honest dependency_relationship_count/external_reference_count and a _truncated flag when the cap was hit; these are declared-evidence rows, not resolved graph truth, so a from/to/component id may not match an indexed component. Each row also carries slsa_provenance_predicate_type and slsa_provenance_builder_id joined from a matching attestation.slsa_provenance fact (empty when none joined). Populated by the opt-in sbom_attestation collector (off in a default deploy; enable with ESHU_COLLECTOR_INSTANCES_JSON plus SBOM document URLs or OCI referrer credentials), so a default git-only deploy returns an empty page.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"subject_digest": map[string]any{
						"type":        "string",
						"description": "Image or artifact digest that anchors the SBOM or attestation attachment.",
					},
					"digest": map[string]any{
						"type":        "string",
						"description": "Alias for subject_digest when the caller has an image digest.",
					},
					"repository_id": map[string]any{
						"type":        "string",
						"description": "Canonical source repository id or human repository selector: name, repo slug, indexed path, local path, or remote URL. Missing repository-to-image evidence is returned as missing_evidence.",
					},
					"workload_id": map[string]any{
						"type":        "string",
						"description": "Reducer-admitted workload anchor. Missing workload-to-image evidence is returned as missing_evidence.",
					},
					"service_id": map[string]any{
						"type":        "string",
						"description": "Reducer-admitted service anchor. Missing service-to-image evidence is returned as missing_evidence.",
					},
					"document_id": map[string]any{
						"type":        "string",
						"description": "SBOM document or attestation statement ID for exact document lookup.",
					},
					"document_digest": map[string]any{
						"type":        "string",
						"description": "Digest of the SBOM document or attestation statement.",
					},
					"attachment_status": map[string]any{
						"type":        "string",
						"description": "Optional reducer attachment status filter.",
						"enum":        []string{"attached_verified", "attached_unverified", "attached_parse_only", "subject_mismatch", "ambiguous_subject", "unknown_subject", "unparseable"},
					},
					"artifact_kind": map[string]any{
						"type":        "string",
						"description": "Optional artifact kind filter.",
						"enum":        []string{"sbom", "attestation"},
					},
					"after_attachment_id": map[string]any{
						"type":        "string",
						"description": "Attachment ID from next_cursor when continuing a truncated page.",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum attachment rows to return.",
						"default":     50,
						"minimum":     1,
						"maximum":     200,
					},
				},
			},
		},
	}
}
