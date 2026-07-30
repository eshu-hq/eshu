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
//   - AWS-only enrichment: the AWS-specific reducer domain computes six
//     fields (MatchedTerraformConfigFile/ModulePath, MatchedOtherIaCSource,
//     ServiceCandidates, EnvironmentCandidates, DependencyPaths) that the
//     provider-neutral domain does not compute for GCP/Azure. Rather than
//     drop them (losing real AWS evidence) or synthesize an empty-but-present
//     value for GCP/Azure (inventing agreement that does not exist),
//     MultiCloudRuntimeDriftFindingRow/CloudRuntimeDriftFindingView carry
//     them as provider-conditional fields: populated only here, for
//     AWS-origin rows, and always absent (omitempty on the wire) for
//     GCP/Azure ones. The response is honest about which provider actually
//     produced which field instead of defaulting one shape's absence onto
//     the other's presence.
//   - Everything else (FindingKind, ManagementStatus, Confidence,
//     MatchedTerraformStateAddress, MissingEvidence, WarningFlags,
//     RecommendedAction, and the DriftedAttributes evidence projection) uses
//     the SAME field names and vocabulary in both payloads, so this mapping
//     copies them directly rather than re-deriving them. It deliberately does
//     NOT replicate awsRuntimeDriftRowToIaCManagement's ARN-based evidence
//     re-derivation (iac_management_transform.go) -- that logic is specific
//     to the AWS-only list_aws_runtime_drift_findings/ListUnmanagedCloudResources
//     contract and has no GCP/Azure equivalent; reusing it here would give
//     AWS-origin rows richer derived truth than GCP/Azure rows on this same
//     surface can ever carry. Treating every provider identically (trust the
//     reducer's own stored fields) is what multiCloudRuntimeDriftRowFromStore
//     already does for GCP/Azure, so this keeps the neutral surface's
//     semantics uniform across all three providers.
func awsCloudRuntimeDriftRowToNeutral(row postgres.AWSCloudRuntimeDriftFindingRow) MultiCloudRuntimeDriftFindingRow {
	resolution := cloudinventory.ResolveProviderIdentity(cloudinventory.ProviderAWS, row.ARN)
	return MultiCloudRuntimeDriftFindingRow{
		FactID:                       row.FactID,
		ScopeID:                      row.ScopeID,
		GenerationID:                 row.GenerationID,
		SourceSystem:                 row.SourceSystem,
		Provider:                     cloudinventory.ProviderAWS,
		CloudResourceUID:             resolution.CloudResourceUID,
		RawIdentity:                  row.ARN,
		FindingKind:                  row.FindingKind,
		ManagementStatus:             row.ManagementStatus,
		Confidence:                   row.Confidence,
		MatchedTerraformStateAddress: row.MatchedTerraformStateAddress,
		MissingEvidence:              row.MissingEvidence,
		WarningFlags:                 row.WarningFlags,
		RecommendedAction:            row.RecommendedAction,
		DriftedAttributes:            driftedAttributesFromAWSEvidence(row.Evidence),
		MatchedTerraformConfigFile:   row.MatchedTerraformConfigFile,
		MatchedTerraformModulePath:   row.MatchedTerraformModulePath,
		MatchedOtherIaCSource:        row.MatchedOtherIaCSource,
		ServiceCandidates:            row.ServiceCandidates,
		EnvironmentCandidates:        row.EnvironmentCandidates,
		DependencyPaths:              row.DependencyPaths,
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
