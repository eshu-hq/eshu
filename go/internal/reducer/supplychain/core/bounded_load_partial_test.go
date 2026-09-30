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

// limitHonoringOSPackageLoader wraps the scope-partitioned fixture loader with
// an OS-package advisory reader that behaves like the production postgres
// reader: it drains every available target, but stops once the envelopes it
// holds exceed the limit it was given, keeps the crossing batch, and reports
// truncated. The stock fixture returns every configured envelope regardless of
// limit, which is exactly how an unflagged cap hid behind a green test.
type limitHonoringOSPackageLoader struct {
	*scanScopedSupplyChainImpactFactLoader
	available []facts.Envelope
	// skippedAvailable is how many matching targets the reader would skip for
	// missing fields.
	skippedAvailable int
	// crossingBatch is how many envelopes past the limit the reader returns
	// when it stops on the limit (production: the rest of its page).
	crossingBatch int
	limits        []int
	packageIDs    [][]string
}

func (l *limitHonoringOSPackageLoader) ListOSPackageAdvisoryFactEnvelopes(
	_ context.Context,
	_ []string,
	packageIDs []string,
	limit int,
) ([]facts.Envelope, int, bool, error) {
	l.limits = append(l.limits, limit)
	l.packageIDs = append(l.packageIDs, append([]string(nil), packageIDs...))
	if len(l.available) <= limit {
		return append([]facts.Envelope(nil), l.available...), l.skippedAvailable, false, nil
	}
	stop := min(len(l.available), limit+max(l.crossingBatch, 1))
	return append([]facts.Envelope(nil), l.available[:stop]...), l.skippedAvailable, true, nil
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

// TestSupplyChainImpactOSPackageTargetsOverCapConverge pins #7154: an
// OS-package target set larger than one reader page is paged to completion, so
// the pass is a complete view and retracts. Before #7154 the same load hit the
// 500-target cap on every pass and the scope kept its stale findings forever.
func TestSupplyChainImpactOSPackageTargetsOverCapConverge(t *testing.T) {
	t.Parallel()

	// 500 was the reader's old target cap; the counts straddle it and its
	// multiples.
	const capTargets = 500
	cases := []struct {
		name      string
		available int
		skipped   int
	}{
		{"one past the old cap", capTargets + 1, 0},
		{"three times the old cap", capTargets * 3, 0},
		{"skipped rows push past the old cap", capTargets - 5, 6},
		{"exactly the old cap", capTargets, 0},
		{"under the old cap", capTargets - 1, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			write, calls, _, result := runOSPackageCapPass(t, tc.available, tc.skipped)
			if write.PartialEvidence {
				t.Fatalf("PartialEvidence = true (available=%d skipped=%d); an OS-package set that pages to completion is a complete view", tc.available, tc.skipped)
			}
			if !retractionIssued(calls) {
				t.Fatalf("retraction not issued (available=%d skipped=%d); a capped scope must converge", tc.available, tc.skipped)
			}
			if got := result.SubSignals["active_evidence_truncated"]; got != 0 {
				t.Fatalf("SubSignals[active_evidence_truncated] = %v, want 0", got)
			}
			if got, want := result.SubSignals["os_package_advisory_facts"], float64(tc.available); got != want {
				t.Fatalf("os_package_advisory_facts = %v, want every available target %v", got, want)
			}
		})
	}
}

// TestSupplyChainImpactPeerIdentityRepositoriesOverCapConverge pins #7154 on
// the peer-identity stage: seeding more repository ids than one filter chunk
// used to drop the excess. Every id must now reach the reader across chunks,
// and the stage must report no truncation (its bool is the suppression tail
// only).
func TestSupplyChainImpactPeerIdentityRepositoriesOverCapConverge(t *testing.T) {
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
	for _, count := range []int{10, supplyChainImpactFilterChunkSize / 2, supplyChainImpactFilterChunkSize/2 + 1, supplyChainImpactFilterChunkSize * 3} {
		loader := &stubSupplyChainImpactFactLoader{}
		handler := SupplyChainImpactHandler{FactLoader: loader}
		_, truncated, err := handler.loadSupplyChainImpactPeerIdentityFacts(context.Background(), build(count), nil)
		if err != nil {
			t.Fatalf("count=%d: loadSupplyChainImpactPeerIdentityFacts() error = %v", count, err)
		}
		if truncated {
			t.Fatalf("count=%d: truncated = true; the excess must be paged, not dropped", count)
		}
		requested := map[string]bool{}
		for _, filter := range loader.filters {
			for _, id := range filter.RepositoryIDs {
				requested[id] = true
			}
		}
		for i := range count {
			if id := fmt.Sprintf("repo-%04d", i); !requested[id] {
				t.Fatalf("count=%d: repository id %q never reached the reader; the cap dropped it", count, id)
			}
		}
	}
}
