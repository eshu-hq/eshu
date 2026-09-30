// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// osPackageAdvisoryDrainPageSize is how many candidate rows one page of
// ListOSPackageAdvisoryFactEnvelopes reads. It bounds one statement, not the
// drain: the drain runs to its last page or to the caller's envelope limit.
const osPackageAdvisoryDrainPageSize = 500

// ListOSPackageAdvisoryFactEnvelopes loads the active installed OS package
// advisory targets whose package id is one of packageIDs and whose advisory
// ecosystem is one of ecosystems, and reconstructs each into a
// vulnerability.os_package facts.Envelope, so a cross-scope reducer consumer
// can feed it through the normal fact-decode seam exactly as if the fact had
// been loaded natively for its own scope.
//
// The read is narrowed to packageIDs because the finding set of one impact
// intent depends only on installed rows whose package id equals one of the
// intent's affected-package lookup keys (#7154); an empty packageIDs or
// ecosystems returns nothing. The whole drain runs inside ONE read-only
// repeatable-read snapshot. An os_package fact id hashes its generation id, so
// a scan scope whose generation flipped between two page statements would move
// every still-installed package to a new position in the key space and the
// cursor would skip some of them; the pass would then count as complete and
// retract their findings. One snapshot reads one consistent generation set.
//
// The drain pages by fact id until a short page, or until the envelopes loaded
// exceed limit. The page that crosses limit is kept, so the caller never loses
// evidence it already paid for, and truncated reports that the drain stopped on
// the limit. limit is the caller's remaining evidence budget: a non-positive
// limit still reads one page and reports truncated when it returns anything.
//
// The returned int is the count of targets skipped because they are missing
// one or more required fields (distro, distro_version, package_manager,
// name, arch, installed_version, fact_id, scope_id, or generation_id).
// A persistently non-zero skip count signals a systematic backfill gap.
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
// stage (go/internal/reducer/supplychain/core/os_package_advisory_load.go),
// which declares its FactLoader-satisfying interface using only leaf types
// that this method's signature matches structurally.
func (s FactStore) ListOSPackageAdvisoryFactEnvelopes(
	ctx context.Context,
	ecosystems []string,
	packageIDs []string,
	limit int,
) ([]facts.Envelope, int, bool, error) {
	if s.database == nil {
		return nil, 0, false, fmt.Errorf("fact store database is required")
	}
	ecosystems = cleanStringFilterValues(ecosystems)
	packageIDs = cleanStringFilterValues(packageIDs)
	if len(ecosystems) == 0 || len(packageIDs) == 0 {
		return nil, 0, false, nil
	}

	var (
		envelopes []facts.Envelope
		skipped   int
		truncated bool
	)
	err := withReadOnlyRepeatableRead(ctx, s.database, func(queryer db.Queryer) error {
		cursor := ""
		for {
			page, err := readOSPackageAdvisoryCandidatePage(ctx, queryer, packageIDs, ecosystems, cursor)
			if err != nil {
				return err
			}
			for _, candidate := range page.candidates {
				if !candidate.active {
					continue
				}
				envelope, ok := osPackageAdvisoryFactEnvelopeFromTarget(candidate.target)
				if !ok {
					skipped++
					continue
				}
				envelopes = append(envelopes, envelope)
			}
			if len(page.candidates) < osPackageAdvisoryDrainPageSize {
				return nil
			}
			if len(envelopes) > limit {
				truncated = true
				return nil
			}
			cursor = page.lastFactID
		}
	})
	if err != nil {
		return nil, 0, false, err
	}
	return envelopes, skipped, truncated, nil
}

type osPackageAdvisoryCandidate struct {
	target workflow.OSPackageAdvisoryTarget
	active bool
}

type osPackageAdvisoryCandidatePage struct {
	candidates []osPackageAdvisoryCandidate
	lastFactID string
}

// readOSPackageAdvisoryCandidatePage reads one page of candidate rows after
// cursor. Every candidate row is returned, active or not, so the caller counts
// a full page and advances the cursor past rows it then drops.
func readOSPackageAdvisoryCandidatePage(
	ctx context.Context,
	queryer db.Queryer,
	packageIDs []string,
	ecosystems []string,
	cursor string,
) (osPackageAdvisoryCandidatePage, error) {
	rows, err := queryer.QueryContext(
		ctx,
		listOSPackageAdvisoryTargetsForPackagesQuery(),
		packageIDs, cursor, osPackageAdvisoryDrainPageSize, ecosystems,
	)
	if err != nil {
		return osPackageAdvisoryCandidatePage{}, fmt.Errorf("list OS package advisory targets after %q: %w", cursor, err)
	}
	defer func() { _ = rows.Close() }()

	page := osPackageAdvisoryCandidatePage{candidates: make([]osPackageAdvisoryCandidate, 0, osPackageAdvisoryDrainPageSize)}
	for rows.Next() {
		var candidate osPackageAdvisoryCandidate
		target := &candidate.target
		if err := rows.Scan(
			&target.Ecosystem,
			&target.Distro,
			&target.DistroVersion,
			&target.PackageName,
			&target.InstalledVersion,
			&target.PackageManager,
			&target.Arch,
			&target.VendorAdvisorySource,
			&target.RepositoryClass,
			&target.PURL,
			&target.FactID,
			&target.ScopeID,
			&target.GenerationID,
			&candidate.active,
		); err != nil {
			return osPackageAdvisoryCandidatePage{}, fmt.Errorf("list OS package advisory targets after %q: %w", cursor, err)
		}
		page.lastFactID = target.FactID
		normalizeOSPackageAdvisoryTarget(target)
		page.candidates = append(page.candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return osPackageAdvisoryCandidatePage{}, fmt.Errorf("list OS package advisory targets after %q: %w", cursor, err)
	}
	if len(page.candidates) == osPackageAdvisoryDrainPageSize && page.lastFactID == cursor {
		// A full page that does not advance would repeat forever.
		return osPackageAdvisoryCandidatePage{}, fmt.Errorf("os package advisory drain did not advance past %q", cursor)
	}
	return page, nil
}

// osPackageAdvisoryFactEnvelopeFromTarget reconstructs a vulnerability.os_package
// fact envelope from one OSPackageAdvisoryTarget row, carrying every field
// that fact kind's required-field decode contract needs — distro,
// distro_version, package_manager, name, arch, installed_version_raw (see
// sdk/go/factschema/vulnerability/v1/os_package.go) — plus the optional
// purl/repository_class/vendor_advisory_source fields the reducer's
// supply-chain-impact matcher reads
// (go/internal/reducer/supplychain/core/match.go,
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
