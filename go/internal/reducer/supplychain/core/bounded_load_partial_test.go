// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/factwrite/testutil"
)

// limitHonoringOSPackageLoader wraps the scope-partitioned fixture loader
// with an OS-package advisory reader that behaves like the production
// postgres reader: it returns at most limit targets (loaded plus skipped),
// ordered, with no rotation. The stock fixture returns every configured
// envelope regardless of limit, which is exactly how an unflagged cap hid
// behind a green test.
type limitHonoringOSPackageLoader struct {
	*scanScopedSupplyChainImpactFactLoader
	available []facts.Envelope
	// skippedAvailable is how many additional matching targets the reader
	// would skip for missing fields; they consume the limit like real rows.
	skippedAvailable int
	limits           []int
}

func (l *limitHonoringOSPackageLoader) ListOSPackageAdvisoryFactEnvelopes(
	_ context.Context,
	_ []string,
	limit int,
) ([]facts.Envelope, int, error) {
	l.limits = append(l.limits, limit)
	remaining := limit
	skipped := min(l.skippedAvailable, remaining)
	remaining -= skipped
	loaded := l.available
	if len(loaded) > remaining {
		loaded = loaded[:remaining]
	}
	return append([]facts.Envelope(nil), loaded...), skipped, nil
}

func osPackageEnvelopes(count int, scopeID, generationID string) []facts.Envelope {
	out := make([]facts.Envelope, 0, count)
	for i := range count {
		out = append(out, facts.Envelope{
			FactID:       fmt.Sprintf("dpkg-os-%04d", i),
			FactKind:     facts.VulnerabilityOSPackageFactKind,
			ScopeID:      scopeID,
			GenerationID: generationID,
			Payload: map[string]any{
				"distro":                 "debian",
				"distro_version":         "12",
				"package_manager":        "dpkg",
				"name":                   fmt.Sprintf("pkg-%04d", i),
				"arch":                   "amd64",
				"repository_class":       "vendor",
				"vendor_advisory_source": "debian",
				"installed_version_raw":  "1.0-1",
			},
		})
	}
	return out
}

// captureWriteWriter records the write the handler hands the real writer.
type captureWriteWriter struct {
	inner PostgresSupplyChainImpactWriter
	write SupplyChainImpactWrite
}

func (c *captureWriteWriter) WriteSupplyChainImpactFindings(
	ctx context.Context,
	write SupplyChainImpactWrite,
) (SupplyChainImpactWriteResult, error) {
	c.write = write
	return c.inner.WriteSupplyChainImpactFindings(ctx, write)
}

// runOSPackageCapPass drives the real Handle with the real Postgres writer on
// a fake transaction, so the assertion is on the statements a pass actually
// issues rather than on a hand-set flag.
func runOSPackageCapPass(
	t *testing.T,
	available, skippedAvailable int,
) (SupplyChainImpactWrite, []testutil.ExecCall, *limitHonoringOSPackageLoader, reducercontract.Result) {
	t.Helper()

	const (
		intentScopeID      = "vuln-intel:debian:cap-6831"
		intentGenerationID = "generation-intel-cap-6831"
		scanScopeID        = "scan-target-cap-6831"
		scanGenerationID   = "generation-scan-cap-6831"
	)
	base := &scanScopedSupplyChainImpactFactLoader{
		factsByScope: map[string][]facts.Envelope{
			scanScopedFactLoaderKey(intentScopeID, intentGenerationID): {
				vulnerabilityCVEFactWithProvenance(
					"debian-cve-cap", "CVE-2026-6831", "debian", "DSA-2026-6831",
					7.5, "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:N/A:N", "HIGH", "2026-06-05T12:00:00Z",
				),
				vulnerabilityAffectedPackageFactWithSource(
					"debian-affected-cap", "CVE-2026-6831", "debian", "DSA-2026-6831",
					"pkg:deb/debian/pkg-0000", "deb", "pkg-0000", "1.0-1", "1.0-2",
				),
			},
		},
	}
	loader := &limitHonoringOSPackageLoader{
		scanScopedSupplyChainImpactFactLoader: base,
		available:                             osPackageEnvelopes(available, scanScopeID, scanGenerationID),
		skippedAvailable:                      skippedAvailable,
	}
	beginner := newFakeImpactBeginner(&testutil.FakeExecer{})
	capture := &captureWriteWriter{inner: PostgresSupplyChainImpactWriter{DB: beginner}}
	handler := SupplyChainImpactHandler{FactLoader: loader, Writer: capture}

	result, err := handler.Handle(context.Background(), reducercontract.Intent{
		IntentID:     "intent-cap-6831",
		ScopeID:      intentScopeID,
		GenerationID: intentGenerationID,
		SourceSystem: "vulnerability_intelligence",
		Domain:       reducercontract.DomainSupplyChainImpact,
		Cause:        "debian advisory observed",
	})
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	return capture.write, beginner.state.all, loader, result
}

func retractionIssued(calls []testutil.ExecCall) bool {
	for _, call := range calls {
		if call.Query == retractSupersededSupplyChainImpactFindingsQuery {
			return true
		}
	}
	return false
}

// TestSupplyChainImpactOSPackageAdvisoryCapMarksPartialEvidence pins the #6831
// review F1: the OS-package advisory load is bounded, and a load that hit the
// bound is not the complete evidence set, so the pass must not retract.
func TestSupplyChainImpactOSPackageAdvisoryCapMarksPartialEvidence(t *testing.T) {
	t.Parallel()

	const capTargets = maxSupplyChainImpactOSPackageAdvisoryTargets
	cases := []struct {
		name          string
		available     int
		skipped       int
		wantPartial   bool
		wantRetracted bool
	}{
		{"more than the cap available", capTargets + 1, 0, true, false},
		{"far more than the cap available", capTargets * 3, 0, true, false},
		{"skipped rows push past the cap", capTargets - 5, 6, true, false},
		{"exactly the cap available is complete", capTargets, 0, false, true},
		{"under the cap is complete", capTargets - 1, 0, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			write, calls, loader, result := runOSPackageCapPass(t, tc.available, tc.skipped)
			if write.PartialEvidence != tc.wantPartial {
				t.Fatalf("PartialEvidence = %v, want %v (available=%d skipped=%d, limits asked=%v)",
					write.PartialEvidence, tc.wantPartial, tc.available, tc.skipped, loader.limits)
			}
			if got := retractionIssued(calls); got != tc.wantRetracted {
				t.Fatalf("retraction issued = %v, want %v; a capped OS-package load must retract nothing", got, tc.wantRetracted)
			}
			if got := result.SubSignals["active_evidence_truncated"]; (got == 1) != tc.wantPartial {
				t.Fatalf("SubSignals[active_evidence_truncated] = %v, want truncated=%v", got, tc.wantPartial)
			}
			if got := result.SubSignals["os_package_advisory_facts"]; got > float64(capTargets) {
				t.Fatalf("os_package_advisory_facts = %v, want at most the cap %d", got, capTargets)
			}
		})
	}
}

// TestSupplyChainImpactPeerIdentityRepositoryCapReportsTruncation proves the
// same class on the sibling bounded stage: seeding more repository ids than
// the cap silently dropped the excess while reporting the load complete.
func TestSupplyChainImpactPeerIdentityRepositoryCapReportsTruncation(t *testing.T) {
	t.Parallel()

	build := func(count int) []facts.Envelope {
		out := make([]facts.Envelope, 0, count)
		for i := range count {
			out = append(out, facts.Envelope{
				FactID:   fmt.Sprintf("identity-%04d", i),
				FactKind: reducercontract.ContainerImageIdentityFactKind,
				Payload: map[string]any{
					"repository_id": fmt.Sprintf("repo-%04d", i),
				},
			})
		}
		return out
	}
	handler := SupplyChainImpactHandler{FactLoader: &stubSupplyChainImpactFactLoader{}}

	// supplyChainImpactRepositoryFilterIDs emits each repository id plus its
	// git-repository-scope: alias, so the cap of ids is reached at half as many
	// repositories.
	atCap := maxSupplyChainImpactResolvedDigestLoads / 2
	_, truncated, err := handler.loadSupplyChainImpactPeerIdentityFacts(context.Background(), build(atCap+1))
	if err != nil {
		t.Fatalf("loadSupplyChainImpactPeerIdentityFacts() error = %v", err)
	}
	if !truncated {
		t.Fatal("truncated = false with more repository ids than the cap; the excess was dropped silently")
	}
	_, truncated, err = handler.loadSupplyChainImpactPeerIdentityFacts(context.Background(), build(atCap))
	if err != nil {
		t.Fatalf("loadSupplyChainImpactPeerIdentityFacts() error = %v", err)
	}
	if truncated {
		t.Fatal("truncated = true at exactly the cap; nothing was dropped")
	}
}
