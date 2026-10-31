// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"

	"github.com/eshu-hq/eshu/go/internal/facts"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

const (
	authorizedImpactAdvisoryProfile   = "supply_chain_impact_v1"
	authorizedImpactAuthorityLimit    = 256
	authorizedImpactAdvisoryFactLimit = 50000
)

type authorizedImpactPublicAdvisorySet struct {
	dependencies   []supplychaincore.AdvisoryAuthorityDependency
	facts          []facts.Envelope
	evidence       []supplychaincore.ProtectedScanImpactAdvisoryEvidence
	staticEvidence []supplychaincore.ProtectedStaticImpactPublicAdvisoryEvidence
}

type authorizedImpactPublicAuthority struct {
	collectorInstanceID string
	id                  string
	epoch               int64
	scopeID             string
	generationID        string
	configSHA           string
	providerID          string
	sourceClass         string
	profile             string
	allowedKinds        string
	scopeEpoch          int64
	receiptID           string
}

func loadCurrentAuthorizedPublicAdvisories(
	ctx context.Context, queryer db.Queryer,
) (authorizedImpactPublicAdvisorySet, string, error) {
	expected, err := countExpectedAuthorizedPublicAdvisories(ctx, queryer)
	if err != nil {
		return authorizedImpactPublicAdvisorySet{}, "", err
	}
	if expected > authorizedImpactAuthorityLimit {
		return authorizedImpactPublicAdvisorySet{}, "cardinality_limit", nil
	}
	rows, err := queryer.QueryContext(ctx, authorizedImpactPublicAdvisoriesSQL, authorizedImpactAuthorityLimit+1)
	if err != nil {
		return authorizedImpactPublicAdvisorySet{}, "", fmt.Errorf("list registered public advisory generations: %w", err)
	}
	authorities := make([]authorizedImpactPublicAuthority, 0)
	for rows.Next() {
		var authority authorizedImpactPublicAuthority
		if err := rows.Scan(&authority.id, &authority.collectorInstanceID, &authority.epoch, &authority.scopeID,
			&authority.generationID, &authority.configSHA, &authority.providerID,
			&authority.sourceClass, &authority.profile, &authority.allowedKinds,
			&authority.scopeEpoch, &authority.receiptID); err != nil {
			_ = rows.Close()
			return authorizedImpactPublicAdvisorySet{}, "", fmt.Errorf("read public advisory authority: %w", err)
		}
		authorities = append(authorities, authority)
		if len(authorities) > authorizedImpactAuthorityLimit {
			_ = rows.Close()
			return authorizedImpactPublicAdvisorySet{}, "cardinality_limit", nil
		}
	}
	readErr := rows.Err()
	closeErr := rows.Close()
	if readErr != nil || closeErr != nil {
		return authorizedImpactPublicAdvisorySet{}, "", errors.Join(readErr, closeErr)
	}
	if len(authorities) != expected {
		return authorizedImpactPublicAdvisorySet{}, "policy_unavailable", nil
	}
	if len(authorities) == 0 {
		// No public advisory authority is registered anywhere: advisory-gated
		// publication is not in use, so advisory evidence is vacuously absent
		// and the static evidence stands on its own. A registered set with a
		// broken witness still fail-closes above.
		return authorizedImpactPublicAdvisorySet{}, "", nil
	}
	result := authorizedImpactPublicAdvisorySet{
		dependencies: make([]supplychaincore.AdvisoryAuthorityDependency, 0, len(authorities)),
	}
	seenFacts := make(map[string]struct{})
	for _, authority := range authorities {
		if authority.id == "" || authority.collectorInstanceID == "" || authority.receiptID == "" || authority.epoch <= 0 || authority.scopeEpoch <= 0 ||
			authority.configSHA == "" || authority.providerID == "" || authority.sourceClass == "" ||
			authority.profile != authorizedImpactAdvisoryProfile || !authorizedImpactAllowedKinds(authority.sourceClass, authority.allowedKinds) {
			return authorizedImpactPublicAdvisorySet{}, "policy_unavailable", nil
		}
		dependency := supplychaincore.AdvisoryAuthorityDependency{
			Kind: "public_registration", ID: authority.id,
			RevisionID:     strconv.FormatInt(authority.epoch, 10),
			AuthorityEpoch: authority.epoch, AdvisoryScopeEpoch: authority.scopeEpoch,
			AdvisoryScopeID: authority.scopeID, AdvisoryGenerationID: authority.generationID,
			ConfigFingerprint: authority.configSHA, ProviderID: authority.providerID,
			TrustedSourceClass: authority.sourceClass, ContentProfile: authority.profile,
		}
		factsForAuthority, reason, err := loadCurrentAuthorizedAdvisoryFacts(ctx, queryer, authority)
		if err != nil || reason != "" {
			return authorizedImpactPublicAdvisorySet{}, reason, err
		}
		for _, advisoryFact := range factsForAuthority {
			envelope := advisoryFact.envelope
			if _, duplicate := seenFacts[envelope.FactID]; duplicate {
				return authorizedImpactPublicAdvisorySet{}, "policy_unavailable", nil
			}
			seenFacts[envelope.FactID] = struct{}{}
			result.facts = append(result.facts, envelope)
			result.evidence = append(result.evidence, supplychaincore.ProtectedScanImpactAdvisoryEvidence{
				EvidenceFactID: envelope.FactID, AuthorityKind: dependency.Kind,
				AuthorityID: dependency.ID, AuthorityEpoch: dependency.AuthorityEpoch,
			})
			result.staticEvidence = append(result.staticEvidence, supplychaincore.ProtectedStaticImpactPublicAdvisoryEvidence{
				EvidenceFactID: envelope.FactID, RegistrationID: authority.id,
				GenerationReceiptID: authority.receiptID,
				RegistrationEpoch:   authority.epoch, ActivationEpoch: authority.scopeEpoch,
				EvidencePayloadSHA: advisoryFact.payloadSHA,
			})
			if len(result.facts) > authorizedImpactAdvisoryFactLimit {
				return authorizedImpactPublicAdvisorySet{}, "cardinality_limit", nil
			}
		}
		result.dependencies = append(result.dependencies, dependency)
	}
	return result, "", nil
}

func countExpectedAuthorizedPublicAdvisories(ctx context.Context, queryer db.Queryer) (int, error) {
	rows, err := queryer.QueryContext(ctx, authorizedImpactExpectedPublicAuthoritiesSQL)
	if err != nil {
		return 0, fmt.Errorf("count expected public advisory authorities: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return 0, errors.Join(fmt.Errorf("public advisory authority count returned no row"), rows.Err())
	}
	var count int
	if err := rows.Scan(&count); err != nil {
		return 0, fmt.Errorf("read public advisory authority count: %w", err)
	}
	return count, rows.Err()
}

// authorizedImpactExpectedKinds is the single source of truth mapping a
// trusted advisory source class to the fact kinds its generations may carry.
// The seam's kind attribution (decodeVulnerabilityAdvisorySourceClass) must
// stay consistent with this map; the test pins that every kind here
// attributes to exactly its own class and no foreign one.
func authorizedImpactExpectedKinds(sourceClass string) []string {
	switch sourceClass {
	case "nvd":
		return []string{"vulnerability.cve", "vulnerability.affected_product"}
	case "osv":
		return []string{"vulnerability.cve", "vulnerability.affected_package"}
	case "cisa_kev":
		return []string{"vulnerability.known_exploited"}
	case "first_epss":
		return []string{"vulnerability.epss_score"}
	default:
		return nil
	}
}

func authorizedImpactAllowedKinds(sourceClass, raw string) bool {
	var kinds []string
	if err := json.Unmarshal([]byte(raw), &kinds); err != nil {
		return false
	}
	expected := authorizedImpactExpectedKinds(sourceClass)
	if expected == nil {
		return false
	}
	if len(kinds) != len(expected) {
		return false
	}
	seen := make(map[string]struct{}, len(kinds))
	for _, kind := range kinds {
		if !slices.Contains(expected, kind) {
			return false
		}
		if _, duplicate := seen[kind]; duplicate {
			return false
		}
		seen[kind] = struct{}{}
	}
	return true
}

func loadCurrentAuthorizedAdvisoryFacts(
	ctx context.Context, queryer db.Queryer, authority authorizedImpactPublicAuthority,
) ([]authorizedImpactAdvisoryFact, string, error) {
	rows, err := queryer.QueryContext(ctx, authorizedImpactAdvisoryFactsSQL,
		authority.receiptID, authority.scopeID, authority.generationID, authority.allowedKinds,
		authorizedImpactAdvisoryFactLimit+1)
	if err != nil {
		return nil, "", fmt.Errorf("load registered advisory facts: %w", err)
	}
	factsForAuthority := make([]authorizedImpactAdvisoryFact, 0)
	for rows.Next() {
		advisoryFact, err := scanAuthorizedImpactAdvisoryFact(rows)
		envelope := advisoryFact.envelope
		if err != nil {
			_ = rows.Close()
			return nil, "", fmt.Errorf("decode registered advisory fact: %w", err)
		}
		if envelope.CollectorKind != "vulnerability_intelligence" ||
			envelope.SourceRef.SourceSystem != "vulnerability_intelligence" ||
			advisoryFact.receiptCollectorInstanceID != authority.collectorInstanceID ||
			!advisorySourceClassMatches(envelope, authority.sourceClass) {
			_ = rows.Close()
			return nil, "policy_unavailable", nil
		}
		factsForAuthority = append(factsForAuthority, advisoryFact)
		if len(factsForAuthority) > authorizedImpactAdvisoryFactLimit {
			_ = rows.Close()
			return nil, "cardinality_limit", nil
		}
	}
	readErr := rows.Err()
	closeErr := rows.Close()
	if readErr != nil || closeErr != nil {
		return nil, "", errors.Join(readErr, closeErr)
	}
	return factsForAuthority, "", nil
}

func loadAuthorizedImpactTargetAdvisories(
	ctx context.Context, queryer db.Queryer, receipt authorizedImpactTargetReceipt,
	target supplychaincore.AuthorizedScanImpactEvidence,
	witness supplychaincore.ScanImpactPublicationWitness,
	public authorizedImpactPublicAdvisorySet,
) (supplychaincore.AuthorizedScanImpactEvidence, supplychaincore.ScanImpactPublicationWitness, string, error) {
	rows, err := queryer.QueryContext(ctx, authorizedImpactPrivateAdvisoriesExistSQL, receipt.scanTargetID)
	if err != nil {
		return target, witness, "", fmt.Errorf("check private advisory binding: %w", err)
	}
	if !rows.Next() {
		readErr := rows.Err()
		_ = rows.Close()
		return target, witness, "", errors.Join(fmt.Errorf("private advisory binding check returned no row"), readErr)
	}
	var privateExists bool
	if err := rows.Scan(&privateExists); err != nil {
		_ = rows.Close()
		return target, witness, "", fmt.Errorf("read private advisory binding check: %w", err)
	}
	readErr := rows.Err()
	closeErr := rows.Close()
	if readErr != nil || closeErr != nil {
		return target, witness, "", errors.Join(readErr, closeErr)
	}
	if privateExists {
		// A private binding needs its own pinned source-configuration
		// fingerprint and exact current authority join. Never substitute a
		// public registration or infer the profile from payload fields.
		return target, witness, "policy_unavailable", nil
	}
	target.Advisories = cloneAuthorizedImpactEnvelopes(public.facts)
	target.AuthorityDependencies = slices.Clone(public.dependencies)
	target.Complete = true
	witness.AdvisoryEvidence = slices.Clone(public.evidence)
	witness.AuthorityDependencies = slices.Clone(public.dependencies)
	witness.Complete = true
	return target, witness, "", nil
}
