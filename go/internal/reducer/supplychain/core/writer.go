// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu-sdk/factschema"
	reducerderivedv1 "github.com/eshu-hq/eshu-sdk/factschema/reducerderived/v1"
	"github.com/eshu-hq/eshu/go/internal/facts"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/factwrite"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
	"github.com/eshu-hq/eshu/go/internal/truth"
)

const supplyChainImpactFactKind = facts.ReducerSupplyChainImpactFindingFactKind

// PostgresSupplyChainImpactWriter stores reducer-owned vulnerability impact
// findings in the shared fact store.
//
// DB is transaction-capable (#6831), not a bare execer: each pass's finding
// set is the complete truth for its (scope, generation), so the upsert and the
// retraction of findings the pass no longer derives must commit together,
// under a lock on that (scope, generation) conflict domain.
type PostgresSupplyChainImpactWriter struct {
	DB  SupplyChainImpactBeginner
	Now func() time.Time
}

// WriteSupplyChainImpactFindings persists every impact status so callers can
// see affected, fixed, possible, and unknown evidence without a hidden priority
// policy.
func (w PostgresSupplyChainImpactWriter) WriteSupplyChainImpactFindings(
	ctx context.Context,
	write SupplyChainImpactWrite,
) (SupplyChainImpactWriteResult, error) {
	if w.DB == nil {
		return SupplyChainImpactWriteResult{}, fmt.Errorf("supply chain impact database is required")
	}
	if write.FencingToken == 0 {
		return SupplyChainImpactWriteResult{}, errSupplyChainImpactMissingFencingToken
	}
	now := factwrite.Now(w.Now)
	rows := make([]factwrite.VersionedRow, 0, len(write.Findings))
	meta := make([]changedRow, 0, len(write.Findings))
	for _, finding := range write.Findings {
		typed := supplyChainImpactTypedPayload(write, finding)
		digest := contentDigestOf(typed)
		if typed.Provenance == nil {
			typed.Provenance = map[string]any{}
		}
		typed.Provenance[contentDigestProvenanceKey] = digest
		payload, err := factschema.EncodeReducerSupplyChainImpactFinding(typed)
		if err != nil {
			return SupplyChainImpactWriteResult{}, fmt.Errorf("encode supply chain impact payload: %w", err)
		}
		payloadJSON, err := json.Marshal(payload)
		if err != nil {
			return SupplyChainImpactWriteResult{}, fmt.Errorf("marshal supply chain impact payload: %w", err)
		}
		factID := supplyChainImpactFactID(write, finding)
		meta = append(meta, changedRow{factID: factID, stableKey: supplyChainImpactStableFactKey(write, finding), digest: digest, cveID: finding.CVEID})
		rows = append(rows, factwrite.VersionedRow{
			FactID:           factID,
			ScopeID:          write.ScopeID,
			GenerationID:     write.GenerationID,
			FactKind:         supplyChainImpactFactKind,
			StableFactKey:    supplyChainImpactStableFactKey(write, finding),
			SchemaVersion:    facts.ReducerDerivedSchemaVersionV1,
			CollectorKind:    factwrite.CollectorKind(write.SourceSystem),
			SourceConfidence: facts.SourceConfidenceInferred,
			SourceSystem:     write.SourceSystem,
			SourceFactKey:    write.IntentID,
			FencingToken:     write.FencingToken,
			ObservedAt:       now,
			IngestedAt:       now,
			Payload:          string(payloadJSON),
		})
	}
	protected, hasProtected, err := buildProtectedScanImpactPublicationSet(write, rows)
	if err != nil {
		return SupplyChainImpactWriteResult{}, fmt.Errorf("prepare protected scan impact publication: %w", err)
	}
	outcome, err := runSupplyChainImpactTx(ctx, w.DB, write, rows, meta, protected, hasProtected, now)
	if err != nil {
		return SupplyChainImpactWriteResult{}, err
	}
	retracted := outcome.retracted
	canonicalWrites := supplyChainImpactCanonicalWrites(write.Findings)
	return SupplyChainImpactWriteResult{
		CanonicalWrites: canonicalWrites,
		FactsWritten:    len(write.Findings),
		FactsRetracted:  retracted,
		ChangedCVEIDs:   outcome.changedCVEs,
		RecoveryCVEIDs:  outcome.recoveryCVEs,
		EvidenceSummary: fmt.Sprintf(
			"wrote supply chain impact findings=%d canonical_writes=%d retracted=%d",
			len(write.Findings), canonicalWrites, retracted,
		),
	}, nil
}

func supplyChainImpactPayload(write SupplyChainImpactWrite, finding SupplyChainImpactFinding) map[string]any {
	payload, err := factschema.EncodeReducerSupplyChainImpactFinding(
		supplyChainImpactTypedPayload(write, finding),
	)
	if err != nil {
		panic(fmt.Sprintf("encode supply chain impact payload: %v", err))
	}
	return payload
}

func supplyChainImpactTypedPayload(
	write SupplyChainImpactWrite,
	finding SupplyChainImpactFinding,
) reducerderivedv1.SupplyChainImpactFinding {
	payload := reducerderivedv1.SupplyChainImpactFinding{
		ReducerDomain:         string(reducercontract.DomainSupplyChainImpact),
		IntentID:              write.IntentID,
		ScopeID:               write.ScopeID,
		GenerationID:          write.GenerationID,
		SourceSystem:          write.SourceSystem,
		Cause:                 write.Cause,
		FindingID:             supplyChainImpactFindingID(finding),
		CVEID:                 finding.CVEID,
		AdvisoryID:            finding.AdvisoryID,
		PackageID:             finding.PackageID,
		Ecosystem:             finding.Ecosystem,
		PackageName:           finding.PackageName,
		PURL:                  finding.PURL,
		ProductCriteria:       finding.ProductCriteria,
		MatchCriteriaID:       finding.MatchCriteriaID,
		ObservedVersion:       finding.ObservedVersion,
		RequestedRange:        finding.RequestedRange,
		FixedVersion:          finding.FixedVersion,
		VulnerableRange:       finding.VulnerableRange,
		MatchReason:           finding.MatchReason,
		ImpactStatus:          string(finding.Status),
		Confidence:            finding.Confidence,
		CVSSScore:             finding.CVSSScore,
		AdvisoryPublishedAt:   finding.AdvisoryPublishedAt,
		AdvisoryUpdatedAt:     finding.AdvisoryUpdatedAt,
		EPSSProbability:       finding.EPSSProbability,
		EPSSPercentile:        finding.EPSSPercentile,
		KnownExploited:        finding.KnownExploited,
		PriorityReason:        finding.PriorityReason,
		PriorityScore:         finding.PriorityScore,
		PriorityBucket:        finding.PriorityBucket,
		PriorityReasonCodes:   nonNilStrings(payloadcore.UniqueSortedStrings(finding.PriorityReasonCodes)),
		PriorityContributions: nonNilMapSlice(serializePriorityContributions(finding.PriorityContributions)),
		RuntimeReachability:   finding.RuntimeReachability,
		RepositoryID:          finding.RepositoryID,
		SubjectDigest:         finding.SubjectDigest,
		ImageRef:              finding.ImageRef,
		DependencyScope:       finding.DependencyScope,
		WorkloadIDs:           nonNilStrings(payloadcore.UniqueSortedStrings(finding.WorkloadIDs)),
		DeploymentIDs:         nonNilStrings(payloadcore.UniqueSortedStrings(finding.DeploymentIDs)),
		ServiceIDs:            nonNilStrings(payloadcore.UniqueSortedStrings(finding.ServiceIDs)),
		Environments:          nonNilStrings(payloadcore.UniqueSortedStrings(finding.Environments)),
		EnvironmentEvidence:   finding.EnvironmentEvidence,
		CatalogEntityRefs:     nonNilStrings(payloadcore.UniqueSortedStrings(finding.CatalogEntityRefs)),
		CatalogOwnerRefs:      nonNilStrings(payloadcore.UniqueSortedStrings(finding.CatalogOwnerRefs)),
		DetectionProfile:      string(finding.DetectionProfile),
		MissingEvidence:       nonNilStrings(payloadcore.UniqueSortedStrings(finding.MissingEvidence)),
		EvidencePath:          nonNilStrings(orderedUniqueStrings(finding.EvidencePath)),
		EvidenceFactIDs:       nonNilStrings(payloadcore.UniqueSortedStrings(finding.EvidenceFactIDs)),
		CanonicalWrites:       finding.CanonicalWrites,
		SourceLayers: []string{
			string(truth.LayerSourceDeclaration),
			string(truth.LayerObservedResource),
		},
	}
	if len(finding.DependencyPath) > 0 {
		depth := finding.DependencyDepth
		payload.DependencyPath = orderedStrings(finding.DependencyPath)
		payload.DependencyDepth = &depth
	}
	if finding.DirectDependency != nil {
		payload.DirectDependency = finding.DirectDependency
	}
	if digest := finding.CIDeclaredArtifactDigest; digest != "" {
		payload.CIDeclaredArtifactDigest = &digest
	}
	if imageRef := finding.CIDeclaredImageRef; imageRef != "" {
		payload.CIDeclaredImageRef = &imageRef
	}
	if reachability := supplyChainReachabilityPayload(finding.Reachability); reachability != nil {
		payload.Reachability = reachability
	}
	provenance := supplyChainImpactProvenancePayload(finding)
	if len(provenance) > 0 {
		payload.Provenance = provenance
	}
	if suppression := supplyChainImpactSuppressionPayload(finding.Suppression); suppression != nil {
		suppressionState := string(supplyChainImpactSuppressionState(finding.Suppression))
		payload.Suppression = suppression
		// Persist the state as a top-level payload key as well so the
		// Postgres read model can filter on it without parsing nested JSON.
		payload.SuppressionState = &suppressionState
	}
	if remediation := supplyChainImpactRemediationPayload(finding.Remediation); remediation != nil {
		payload.Remediation = remediation
	}
	return payload
}

// supplyChainImpactRemediationPayload serializes the advisory-only safe
// upgrade explanation onto the canonical finding payload. Reason and
// confidence are always present so the Postgres read model can filter on
// them without parsing nested JSON.
func supplyChainImpactRemediationPayload(r SupplyChainImpactRemediation) map[string]any {
	if r.Reason == "" && r.Confidence == "" && r.FirstPatchedVersion == "" &&
		r.ManifestRange == "" && r.CurrentVersion == "" && r.VulnerableRange == "" &&
		len(r.PatchedVersionBranches) == 0 && len(r.MissingEvidence) == 0 &&
		r.ParentPackage == "" && r.Ecosystem == "" && r.Direct == nil {
		return nil
	}
	out := map[string]any{
		"reason":              r.Reason,
		"confidence":          r.Confidence,
		"manifest_allows_fix": r.ManifestAllowsFix,
	}
	if r.Ecosystem != "" {
		out["ecosystem"] = r.Ecosystem
	}
	if r.CurrentVersion != "" {
		out["current_version"] = r.CurrentVersion
	}
	if r.VulnerableRange != "" {
		out["vulnerable_range"] = r.VulnerableRange
	}
	if r.FixedVersionSource != "" {
		out["fixed_version_source"] = r.FixedVersionSource
	}
	if r.MatchReason != "" {
		out["match_reason"] = r.MatchReason
	}
	if r.FirstPatchedVersion != "" {
		out["first_patched_version"] = r.FirstPatchedVersion
	}
	if r.ManifestRange != "" {
		out["manifest_range"] = r.ManifestRange
	}
	if r.Direct != nil {
		out["direct"] = *r.Direct
	}
	if r.ParentPackage != "" {
		out["parent_package"] = r.ParentPackage
	}
	if branches := serializeFixedVersionBranches(r.PatchedVersionBranches); len(branches) > 0 {
		out["patched_version_branches"] = branches
	}
	if len(r.MissingEvidence) > 0 {
		out["missing_evidence"] = payloadcore.UniqueSortedStrings(r.MissingEvidence)
	}
	return out
}

func serializePriorityContributions(values []SupplyChainImpactPriorityContribution) []map[string]any {
	if len(values) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(values))
	for _, value := range values {
		if value.ReasonCode == "" {
			continue
		}
		row := map[string]any{
			"reason_code":  value.ReasonCode,
			"input":        value.Input,
			"contribution": value.Contribution,
		}
		if value.Value != "" {
			row["value"] = value.Value
		}
		out = append(out, row)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func supplyChainImpactSuppressionState(decision SupplyChainSuppressionDecision) SupplyChainSuppressionState {
	if decision.State == "" {
		return SupplyChainSuppressionStateActive
	}
	return decision.State
}

func supplyChainImpactSuppressionPayload(decision SupplyChainSuppressionDecision) map[string]any {
	state := supplyChainImpactSuppressionState(decision)
	out := map[string]any{
		"state": string(state),
	}
	if decision.SuppressionID != "" {
		out["suppression_id"] = decision.SuppressionID
	}
	if decision.Source != "" {
		out["source"] = decision.Source
	}
	if decision.Justification != "" {
		out["justification"] = decision.Justification
	}
	if decision.Author != "" {
		out["author"] = decision.Author
	}
	if !decision.AuthoredAt.IsZero() {
		out["authored_at"] = decision.AuthoredAt.UTC().Format(time.RFC3339)
	}
	if !decision.ExpiresAt.IsZero() {
		out["expires_at"] = decision.ExpiresAt.UTC().Format(time.RFC3339)
	}
	if decision.Reason != "" {
		out["reason"] = decision.Reason
	}
	if decision.EvidenceRef != "" {
		out["evidence_ref"] = decision.EvidenceRef
	}
	if decision.VEXDocumentID != "" {
		out["vex_document_id"] = decision.VEXDocumentID
	}
	if decision.VEXStatementID != "" {
		out["vex_statement_id"] = decision.VEXStatementID
	}
	// Always return the payload so the reducer publishes a deterministic
	// "active" decision when no suppression matched; this lets the API
	// filter by suppression_state without nullable-payload guards.
	return out
}

func serializeFixedVersionBranches(values []FixedVersionBranch) []map[string]any {
	if len(values) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(values))
	for _, value := range values {
		out = append(out, map[string]any{
			"version": value.Version,
			"source":  value.Source,
		})
	}
	return out
}

func serializeAdvisorySources(values []AdvisorySourceObservation) []map[string]any {
	if len(values) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(values))
	for _, value := range values {
		row := map[string]any{"source": value.Source}
		if value.AdvisoryID != "" {
			row["advisory_id"] = value.AdvisoryID
		}
		if value.SourceUpdatedAt != "" {
			row["source_updated_at"] = value.SourceUpdatedAt
		}
		if value.WithdrawnAt != "" {
			row["withdrawn_at"] = value.WithdrawnAt
		}
		if value.Status != "" {
			row["status"] = value.Status
		}
		if len(value.ReplacedBy) > 0 {
			row["replaced_by"] = value.ReplacedBy
		}
		out = append(out, row)
	}
	return out
}

// serializeAdvisoryRetraction renders the retraction block a retracted
// finding carries under provenance.advisory_retraction (#9695). It returns nil
// for a finding whose advisory still stands.
func serializeAdvisoryRetraction(retraction *AdvisoryRetraction) map[string]any {
	if retraction == nil {
		return nil
	}
	sources := make([]map[string]any, 0, len(retraction.Sources))
	for _, source := range retraction.Sources {
		row := map[string]any{
			"source":      source.Source,
			"advisory_id": source.AdvisoryID,
			"reason_code": source.Code,
		}
		if source.Status != "" {
			row["status"] = source.Status
		}
		if source.WithdrawnAt != "" {
			row["withdrawn_at"] = source.WithdrawnAt
		}
		if len(source.ReplacedBy) > 0 {
			row["replaced_by"] = source.ReplacedBy
		}
		sources = append(sources, row)
	}
	out := map[string]any{
		"reason_code": retraction.ReasonCode,
		"reason":      retraction.Reason,
		"sources":     sources,
	}
	if len(retraction.ReplacedBy) > 0 {
		out["replaced_by"] = retraction.ReplacedBy
	}
	return out
}

func orderedUniqueStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

// orderedStrings forwards to [payloadcore.OrderedStrings].
func orderedStrings(values []string) []string {
	return payloadcore.OrderedStrings(values)
}
