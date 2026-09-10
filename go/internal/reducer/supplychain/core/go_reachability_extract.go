// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/factdecode"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
	"github.com/eshu-hq/eshu/go/internal/reducer/schemadecode"
	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
	vulnerabilityv1 "github.com/eshu-hq/eshu/sdk/go/factschema/vulnerability/v1"
)

type goModuleEvidenceRow struct {
	factID             string
	repositoryID       string
	relativePath       string
	modulePath         string
	requiredVersion    string
	replacementPath    string
	replacementVersion string
	indirect           bool
	lineNumber         int
}

type goReachabilityRow struct {
	factID         string
	repositoryID   string
	osvID          string
	deepestModule  string
	deepestPackage string
	deepestSymbol  string
	level          string
	trace          []GoVulnerabilityCallFrame
}

type goAffectedPackageRow struct {
	osvID            string
	modulePath       string
	affectedRanges   []supplychainmodel.AffectedRange
	affectedVersions []string
	fixedVersions    []string
}

// extractGoModuleEvidenceRowsWithQuarantine decodes each
// vulnerability.go_module_evidence fact through the contracts seam. A fact
// missing a required identity field (repository_id, relative_path,
// module_path) is routed through factdecode.PartitionDecodeFailures into the returned
// quarantine list rather than silently excluded with no operator signal; any
// OTHER decode error is returned fatally.
func extractGoModuleEvidenceRowsWithQuarantine(envelopes []facts.Envelope) ([]goModuleEvidenceRow, []factdecode.QuarantinedFact, error) {
	out := make([]goModuleEvidenceRow, 0)
	var quarantined []factdecode.QuarantinedFact
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.VulnerabilityGoModuleEvidenceFactKind || envelope.IsTombstone {
			continue
		}
		evidence, err := schemadecode.DecodeVulnerabilityGoModuleEvidence(envelope)
		if err != nil {
			q, isQuarantine, fatal := factdecode.PartitionDecodeFailures(envelope, err)
			if fatal != nil {
				return nil, nil, fatal
			}
			if isQuarantine {
				quarantined = append(quarantined, q)
			}
			continue
		}
		out = append(out, goModuleEvidenceRow{
			factID:             envelope.FactID,
			repositoryID:       evidence.RepositoryID,
			relativePath:       evidence.RelativePath,
			modulePath:         evidence.ModulePath,
			requiredVersion:    payloadcore.DerefString(evidence.RequiredVersion),
			replacementPath:    payloadcore.DerefString(evidence.ReplacementPath),
			replacementVersion: payloadcore.DerefString(evidence.ReplacementVersion),
			indirect:           payloadcore.DerefBool(evidence.Indirect),
			lineNumber:         int(derefInt32(evidence.LineNumber)),
		})
	}
	return out, quarantined, nil
}

// extractGovulncheckReachabilityRowsWithQuarantine decodes each
// vulnerability.go_call_reachability fact through the contracts seam. A fact
// missing a required identity field (repository_id, osv_id) is routed
// through factdecode.PartitionDecodeFailures into the returned quarantine list rather
// than silently excluded with no operator signal; any OTHER decode error is
// returned fatally.
func extractGovulncheckReachabilityRowsWithQuarantine(envelopes []facts.Envelope) ([]goReachabilityRow, []factdecode.QuarantinedFact, error) {
	out := make([]goReachabilityRow, 0)
	var quarantined []factdecode.QuarantinedFact
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.VulnerabilityGoCallReachabilityFactKind || envelope.IsTombstone {
			continue
		}
		finding, err := schemadecode.DecodeVulnerabilityGoCallReachability(envelope)
		if err != nil {
			q, isQuarantine, fatal := factdecode.PartitionDecodeFailures(envelope, err)
			if fatal != nil {
				return nil, nil, fatal
			}
			if isQuarantine {
				quarantined = append(quarantined, q)
			}
			continue
		}
		out = append(out, goReachabilityRow{
			factID:         envelope.FactID,
			repositoryID:   finding.RepositoryID,
			osvID:          finding.OSVID,
			deepestModule:  payloadcore.DerefString(finding.DeepestModule),
			deepestPackage: payloadcore.DerefString(finding.DeepestPackage),
			deepestSymbol:  payloadcore.DerefString(finding.DeepestSymbol),
			level:          payloadcore.DerefString(finding.ReachabilityLevel),
			trace:          goVulnerabilityCallFramesFromTyped(finding.Trace),
		})
	}
	return out, quarantined, nil
}

// goVulnerabilityCallFramesFromTyped converts the typed
// vulnerabilityv1.CallFrame slice into the reducer's internal
// GoVulnerabilityCallFrame rows.
func goVulnerabilityCallFramesFromTyped(frames []vulnerabilityv1.CallFrame) []GoVulnerabilityCallFrame {
	if len(frames) == 0 {
		return nil
	}
	out := make([]GoVulnerabilityCallFrame, 0, len(frames))
	for _, frame := range frames {
		out = append(out, GoVulnerabilityCallFrame{
			Module:   payloadcore.DerefString(frame.Module),
			Version:  payloadcore.DerefString(frame.Version),
			Package:  payloadcore.DerefString(frame.Package),
			Function: payloadcore.DerefString(frame.Function),
			Receiver: payloadcore.DerefString(frame.Receiver),
			Position: payloadcore.DerefString(frame.Position),
		})
	}
	return out
}

// extractGoAffectedPackages projects the Go-ecosystem subset of
// vulnerability.affected_package facts into goAffectedPackageRow values. It
// decodes through the same typed contracts seam the primary
// buildSupplyChainImpactIndexWithQuarantine loop uses for this kind, but does
// NOT quarantine on decode error here: that loop is the single authoritative
// place a vulnerability.affected_package fact is quarantined and counted, so
// this second, Go-specific projection silently skips a fact that fails decode
// (mirroring its own pre-existing skip-on-blank-package-name/osvID tolerance)
// rather than double-reporting the same malformed fact as two separate
// quarantines.
//
// The typed AffectedPackage struct carries no dedicated ModulePath field: the
// wire payload's Go module identity always rides in package_name (the OSV/
// GHSA/go.mod collectors never populate a separate module_path key on this
// kind — that key exists only on vulnerability.go_module_evidence), so
// PackageName is the sole source, matching the pre-typing
// payloadcore.FirstNonBlank(module_path, package_name) fallback's real-world behavior.
func extractGoAffectedPackages(envelopes []facts.Envelope) []goAffectedPackageRow {
	out := make([]goAffectedPackageRow, 0)
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.VulnerabilityAffectedPackageFactKind || envelope.IsTombstone {
			continue
		}
		pkg, err := schemadecode.DecodeVulnerabilityAffectedPackage(envelope)
		if err != nil {
			continue
		}
		ecosystem := strings.ToLower(payloadcore.DerefString(pkg.Ecosystem))
		if ecosystem != "go" && ecosystem != "go-module" && ecosystem != "golang" && ecosystem != "gomod" {
			continue
		}
		modulePath := payloadcore.DerefString(pkg.PackageName)
		osvID := payloadcore.FirstNonBlank(pkg.AdvisoryID, payloadcore.DerefString(pkg.CVEID))
		if modulePath == "" || osvID == "" {
			continue
		}
		out = append(out, goAffectedPackageRow{
			osvID:            osvID,
			modulePath:       modulePath,
			affectedRanges:   supplyChainAffectedRangesFromTyped(pkg.AffectedRanges),
			affectedVersions: pkg.AffectedVersions,
			fixedVersions:    pkg.FixedVersions,
		})
	}
	return out
}

// derefInt32 returns the pointed-to int32, or 0 for a nil pointer. The
// vulnerability.go_module_evidence typed decode carries LineNumber as
// *int32 so an absent line anchor stays distinct from an observed 0, mirroring
// the pre-typing supplyChainInt's own default-to-zero behavior for callers
// that only need the value, not the presence.
func derefInt32(value *int32) int32 {
	if value == nil {
		return 0
	}
	return *value
}
