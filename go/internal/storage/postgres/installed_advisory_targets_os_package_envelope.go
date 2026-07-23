// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// ListOSPackageAdvisoryFactEnvelopes loads active installed OS package
// advisory targets through ListOSPackageAdvisoryTargets — the SAME
// advisory-matched reader and SQL matching logic the vulnerability-
// intelligence coordinator planner uses (service_installed_advisory_targets.go)
// — and reconstructs each target into a vulnerability.os_package
// facts.Envelope, so a cross-scope reducer consumer can feed it through the
// normal fact-decode seam exactly as if the fact had been loaded natively for
// its own scope.
//
// This bridging method exists (rather than exposing the workflow-typed
// ListOSPackageAdvisoryTargets result straight to the reducer) because
// go/internal/reducer cannot import go/internal/workflow: internal/workflow
// itself imports internal/reducer (for GraphProjectionKeyspace/Phase
// constants — see collector_contract.go, progress.go, store.go), so the
// reverse import would be a compile-time cycle. internal/storage/postgres
// already imports both internal/workflow and internal/facts (and
// internal/reducer, in other files, e.g. accepted_generation.go), so the
// target->envelope bridge lives here instead of in the reducer's own load
// stage (go/internal/reducer/supply_chain_impact_os_package_advisory_load.go),
// which declares its FactLoader-satisfying interface using only leaf types
// (context.Context, []string, int, []facts.Envelope) that this method's
// signature matches structurally.
func (s FactStore) ListOSPackageAdvisoryFactEnvelopes(
	ctx context.Context,
	ecosystems []string,
	limit int,
) ([]facts.Envelope, error) {
	targets, err := s.ListOSPackageAdvisoryTargets(ctx, workflow.OSPackageAdvisoryTargetFilter{
		Ecosystems: ecosystems,
		Limit:      limit,
	})
	if err != nil {
		return nil, err
	}
	envelopes := make([]facts.Envelope, 0, len(targets))
	for _, target := range targets {
		envelope, ok := osPackageAdvisoryFactEnvelopeFromTarget(target)
		if !ok {
			continue
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

// osPackageAdvisoryFactEnvelopeFromTarget reconstructs a vulnerability.os_package
// fact envelope from one OSPackageAdvisoryTarget row, carrying every field
// that fact kind's required-field decode contract needs — distro,
// distro_version, package_manager, name, arch, installed_version_raw (see
// sdk/go/factschema/vulnerability/v1/os_package.go) — plus the optional
// purl/repository_class/vendor_advisory_source fields the reducer's
// supply-chain-impact matcher reads
// (go/internal/reducer/supply_chain_impact_match.go,
// osPackageMatchesAffectedPackage). A target missing any required field, its
// FactID, its ScopeID, or its GenerationID is skipped (ok=false) rather than
// producing an envelope that would either dead-letter as input_invalid
// downstream or fail to key the sibling scanner-analysis-scope
// ScopeID+GenerationID join.
func osPackageAdvisoryFactEnvelopeFromTarget(target workflow.OSPackageAdvisoryTarget) (facts.Envelope, bool) {
	distro := strings.TrimSpace(target.Distro)
	distroVersion := strings.TrimSpace(target.DistroVersion)
	packageManager := strings.TrimSpace(target.PackageManager)
	name := strings.TrimSpace(target.PackageName)
	arch := strings.TrimSpace(target.Arch)
	installedVersion := strings.TrimSpace(target.InstalledVersion)
	factID := strings.TrimSpace(target.FactID)
	scopeID := strings.TrimSpace(target.ScopeID)
	generationID := strings.TrimSpace(target.GenerationID)
	if distro == "" || distroVersion == "" || packageManager == "" || name == "" ||
		arch == "" || installedVersion == "" || factID == "" || scopeID == "" || generationID == "" {
		return facts.Envelope{}, false
	}

	payload := map[string]any{
		"distro":                distro,
		"distro_version":        distroVersion,
		"package_manager":       packageManager,
		"name":                  name,
		"arch":                  arch,
		"installed_version_raw": installedVersion,
	}
	if purl := strings.TrimSpace(target.PURL); purl != "" {
		payload["purl"] = purl
	}
	if repositoryClass := strings.TrimSpace(target.RepositoryClass); repositoryClass != "" {
		payload["repository_class"] = repositoryClass
	}
	if vendorSource := strings.TrimSpace(target.VendorAdvisorySource); vendorSource != "" {
		payload["vendor_advisory_source"] = vendorSource
	}

	return facts.Envelope{
		FactID:       factID,
		FactKind:     facts.VulnerabilityOSPackageFactKind,
		ScopeID:      scopeID,
		GenerationID: generationID,
		Payload:      payload,
	}, true
}
