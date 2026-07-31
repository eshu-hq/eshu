// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/correlation/cloudinventory"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// cloudRuntimeDriftAggregateRowFromStore maps one postgres-layer aggregate row
// (tagged by fact kind) onto the query package's provider-neutral row shape,
// dispatching to the existing multi-cloud mapping or the new AWS mapping
// below according to which fact kind actually produced the row.
func cloudRuntimeDriftAggregateRowFromStore(
	row postgres.CloudRuntimeDriftAggregateFindingRow,
) MultiCloudRuntimeDriftFindingRow {
	if row.FactKind == postgres.AWSCloudRuntimeDriftFindingFactKind {
		return awsCloudRuntimeDriftRowToNeutral(row.AWS)
	}
	return multiCloudRuntimeDriftRowFromStore(row.MultiCloud)
}

// awsCloudRuntimeDriftRowToNeutral maps one AWS-specific reducer finding row
// onto the provider-neutral MultiCloudRuntimeDriftFindingRow shape so
// list_cloud_runtime_drift_findings / POST /api/v0/cloud/runtime-drift/findings
// / export_cloud_runtime_drift_packet can aggregate it alongside GCP/Azure
// findings (#5759 follow-up P1: the neutral surface advertised aws coverage
// but only ever queried the multi-cloud fact kind, which
// excludeAWSOwnedRows guarantees never carries an AWS row).
//
// Field-shape reconciliation, stated explicitly because the two fact kinds'
// payloads are NOT the same shape:
//
//   - Identity: the neutral surface never exposes a raw provider locator
//     (RawIdentity is loaded but the view drops it, same as every other
//     provider). An AWS-origin row's canonical CloudResourceUID is resolved
//     through the SAME cloudinventory.ResolveProviderIdentity keyspace every
//     other provider on this surface uses, rather than leaking the raw ARN.
//     A malformed/unresolvable ARN yields an empty CloudResourceUID -- never
//     a fabricated one -- matching ResolveProviderIdentity's own "never
//     invent canonical truth" contract.
//   - Safety verdict (#5759 follow-up P1-1, hostile-review finding): status,
//     missing evidence, and warning flags are computed by
//     awsCloudRuntimeDriftDerivedStatus (iac_management_transform.go), the
//     SAME shared derivation awsRuntimeDriftRowToIaCManagement
//     (list_aws_runtime_drift_findings) uses -- NOT a verbatim copy of
//     row.WarningFlags. A naive copy silently under-reports the safety gate:
//     warningFlagsForManagementFinding adds security_sensitive_resource for
//     an iam/kms/secretsmanager/ssm/rds/certain-ec2-subtype/
//     elasticloadbalancing/cloudfront/route53 ARN and raw_tags_provenance_only
//     when tag evidence is present, and neither flag is ever written by the
//     reducer -- both are read-time-only classifications. Reusing the SAME
//     function (not a reimplementation) means the identical row always
//     produces the identical safety verdict on both surfaces; see
//     TestAWSCloudRuntimeDriftRowToNeutralSafetyGateMatchesAWSSurface.
//   - The six AWS-only IaC-source enrichment fields
//     (MatchedTerraformConfigFile/ModulePath, MatchedOtherIaCSource,
//     ServiceCandidates, EnvironmentCandidates, DependencyPaths) that
//     IaCManagementFindingRow also carries are DELIBERATELY NOT projected
//     here (#5759 follow-up P1-2, hostile-review finding): traced write to
//     read, reducerderivedv1.AWSCloudRuntimeDriftFinding has no fields for
//     them, awsCloudRuntimeDriftTypedPayload never sets them, and the evidence-atom
//     types iacManagementEvidenceEnrichment.recordEvidence matches
//     (service_candidate, environment_candidate, dependency_path,
//     terraform_config_resource with key file_path/relative_path/module_path,
//     any cloudformation/cdk/pulumi/crossplane/serverless/other_iac_resource
//     evidence type) are never emitted by cloudruntime.buildOneCandidate --
//     confirmed by exhaustive repo search, zero producers anywhere. They are
//     structurally unreachable on ANY real fact, on EITHER surface, not just
//     this one; a hand-built test fixture that sets them proves nothing about
//     production behavior. Projecting them here (even mirroring
//     IaCManagementFindingRow's own dead fields) would document a capability
//     that cannot fire, which is exactly the defect this fix removes rather
//     than relocates. The pre-existing dead fields on IaCManagementFindingRow/
//     list_aws_runtime_drift_findings are out of this issue's scope and are
//     flagged separately.
//   - Everything else (FindingKind, Confidence, MatchedTerraformStateAddress,
//     RecommendedAction, and the DriftedAttributes evidence projection) uses
//     the SAME field names and vocabulary in both payloads, so this mapping
//     copies them directly.
func awsCloudRuntimeDriftRowToNeutral(row postgres.AWSCloudRuntimeDriftFindingRow) MultiCloudRuntimeDriftFindingRow {
	resolution := cloudinventory.ResolveProviderIdentity(cloudinventory.ProviderAWS, row.ARN)
	status, missingEvidence, warningFlags := awsCloudRuntimeDriftDerivedStatus(row)
	return MultiCloudRuntimeDriftFindingRow{
		FactID:                       row.FactID,
		ScopeID:                      row.ScopeID,
		GenerationID:                 row.GenerationID,
		SourceSystem:                 row.SourceSystem,
		Provider:                     cloudinventory.ProviderAWS,
		CloudResourceUID:             resolution.CloudResourceUID,
		RawIdentity:                  row.ARN,
		FindingKind:                  row.FindingKind,
		ManagementStatus:             status,
		Confidence:                   row.Confidence,
		MatchedTerraformStateAddress: row.MatchedTerraformStateAddress,
		MissingEvidence:              missingEvidence,
		WarningFlags:                 warningFlags,
		RecommendedAction:            row.RecommendedAction,
		DriftedAttributes:            driftedAttributesFromAWSEvidence(row.Evidence),
	}
}

// cloudRuntimeDriftAggregateFilterToStore converts the query package's filter
// into the postgres aggregate filter shape. Split out so
// cloud_runtime_drift_store.go stays a thin adapter.
func cloudRuntimeDriftAggregateFilterToStore(
	filter MultiCloudRuntimeDriftFilter,
) postgres.CloudRuntimeDriftAggregateFilter {
	return postgres.CloudRuntimeDriftAggregateFilter{
		ScopeID:          filter.ScopeID,
		Provider:         strings.ToLower(strings.TrimSpace(filter.Provider)),
		CloudResourceUID: filter.CloudResourceUID,
		FindingKinds:     filter.FindingKinds,
		Limit:            filter.Limit,
		Offset:           filter.Offset,
	}
}
