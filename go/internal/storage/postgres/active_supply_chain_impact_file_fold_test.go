// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const foldTestFilePadding = 4096

// foldRoutingDB serves one core fact plus fileCount file facts, each file
// payload padded so payload byte accounting is observable. File IDs are
// numbered out of byte order on purpose: the branch returns them shuffled.
func foldRoutingDB(fileCount int) (*supplyChainImpactRoutingDB, []string, map[string]int) {
	routed := &supplyChainImpactRoutingDB{
		branchRows: map[string][][2]string{
			"vulnerability_cve_id": {{"vuln-affected-package:core", "vulnerability.affected_package"}},
		},
		facts:    map[string]string{"vuln-affected-package:core": "vulnerability.affected_package"},
		payloads: map[string][]byte{},
	}
	var fileRows [][2]string
	var ids []string
	sizes := map[string]int{}
	for n := fileCount - 1; n >= 0; n-- {
		id := fmt.Sprintf("file:%05d", (n*7919)%fileCount)
		payload := []byte(`{"id":"` + id + `","pad":"` + strings.Repeat("x", foldTestFilePadding) + `"}`)
		routed.facts[id] = factload.FactKindFile
		routed.payloads[id] = payload
		sizes[id] = len(payload)
		fileRows = append(fileRows, [2]string{id, factload.FactKindFile})
		ids = append(ids, id)
	}
	routed.branchRows["file_repo_id"] = fileRows
	return routed, ids, sizes
}

func foldFilter() supplychaincore.SupplyChainImpactFactFilter {
	return supplychaincore.SupplyChainImpactFactFilter{
		CVEIDs:            []string{"CVE-2026-9781"},
		FileRepositoryIDs: []string{"repo-a"},
		FileFold:          supplychaincore.NewFileReachabilityFold(nil),
	}
}

// TestListActiveSupplyChainImpactFactsFoldsFileFactsAtTheLoader pins #9781:
// with a FileFold on the filter, file facts are fetched from the byte-ordered
// payload statement, folded as they are scanned, and never returned; the
// non-file core fact still comes back through the primary-key page, and stats
// separate folded bytes from retained bytes.
func TestListActiveSupplyChainImpactFactsFoldsFileFactsAtTheLoader(t *testing.T) {
	t.Parallel()

	routed, ids, sizes := foldRoutingDB(7)
	filter := foldFilter()
	loaded, stats, err := NewFactStore(routed).ListActiveSupplyChainImpactFactsWithStats(context.Background(), filter)
	if err != nil {
		t.Fatalf("ListActiveSupplyChainImpactFactsWithStats() error = %v, want nil", err)
	}
	for _, envelope := range loaded {
		if envelope.FactKind == factload.FactKindFile {
			t.Fatalf("returned file fact %q, want files folded and not returned", envelope.FactID)
		}
	}
	if len(loaded) != 1 || loaded[0].FactID != "vuln-affected-package:core" {
		t.Fatalf("loaded = %v, want only the core fact", foldFactIDs(loaded))
	}
	if got := filter.FileFold.Files(); got != len(ids) {
		t.Fatalf("fold absorbed %d files, want %d", got, len(ids))
	}
	if stats.FoldedFiles != len(ids) {
		t.Fatalf("stats.FoldedFiles = %d, want %d", stats.FoldedFiles, len(ids))
	}
	var wantFolded int64
	for _, size := range sizes {
		wantFolded += int64(size)
	}
	if stats.FoldedPayloadBytes != wantFolded {
		t.Fatalf("stats.FoldedPayloadBytes = %d, want %d", stats.FoldedPayloadBytes, wantFolded)
	}
	wantRetained := int64(len(`{"id":"vuln-affected-package:core"}`))
	if stats.RetainedPayloadBytes != wantRetained {
		t.Fatalf("stats.RetainedPayloadBytes = %d, want %d (core fact only; folded bytes are not retained)", stats.RetainedPayloadBytes, wantRetained)
	}
	ordered := routed.callsFor("payload_page_ordered")
	if len(ordered) != 1 {
		t.Fatalf("ordered payload pages = %d, want 1", len(ordered))
	}
	bound := ordered[0].Args[0].([]string)
	if !slices.IsSortedFunc(bound, func(a, b string) int { return bytes.Compare([]byte(a), []byte(b)) }) {
		t.Fatalf("ordered page bound %v, want ids in byte order", bound)
	}
	for _, call := range routed.callsFor("payload_page") {
		for _, id := range call.Args[0].([]string) {
			if strings.HasPrefix(id, "file:") {
				t.Fatalf("primary-key page fetched file %q, want files on the ordered statement only", id)
			}
		}
	}
}

// TestListActiveSupplyChainImpactFactsFoldPagesPastFiveHundredFiles pins that
// the fold pages the ordered statement like any other payload page.
func TestListActiveSupplyChainImpactFactsFoldPagesPastFiveHundredFiles(t *testing.T) {
	t.Parallel()

	routed, _, _ := foldRoutingDB(supplyChainImpactPayloadPageSize + 1)
	filter := foldFilter()
	_, stats, err := NewFactStore(routed).ListActiveSupplyChainImpactFactsWithStats(context.Background(), filter)
	if err != nil {
		t.Fatalf("error = %v, want nil", err)
	}
	if got := len(routed.callsFor("payload_page_ordered")); got != 2 {
		t.Fatalf("ordered pages = %d, want 2", got)
	}
	if stats.FoldedFiles != supplyChainImpactPayloadPageSize+1 {
		t.Fatalf("stats.FoldedFiles = %d, want %d", stats.FoldedFiles, supplyChainImpactPayloadPageSize+1)
	}
}

// TestListActiveSupplyChainImpactFactsFoldRejectsBadOrderedPages pins the load
// contract: the fold's result depends on file order, so a page that comes back
// out of byte order, or drops a resolved file, fails the call.
func TestListActiveSupplyChainImpactFactsFoldRejectsBadOrderedPages(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name    string
		mutate  func(*supplyChainImpactRoutingDB, []string)
		wantErr string
	}{
		{name: "page out of byte order", mutate: func(r *supplyChainImpactRoutingDB, _ []string) { r.misorderOrdered = true }, wantErr: "order"},
		{name: "page drops a resolved file", mutate: func(r *supplyChainImpactRoutingDB, ids []string) { r.missing = map[string]bool{ids[0]: true} }, wantErr: "returned"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			routed, ids, _ := foldRoutingDB(5)
			test.mutate(routed, ids)
			_, _, err := NewFactStore(routed).ListActiveSupplyChainImpactFactsWithStats(context.Background(), foldFilter())
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("error = %v, want one containing %q", err, test.wantErr)
			}
		})
	}
}

// TestListActiveSupplyChainImpactFactsWithoutFoldReturnsFiles pins that a
// caller with no fold gets file envelopes through the primary-key page, as
// before, and never touches the ordered statement.
func TestListActiveSupplyChainImpactFactsWithoutFoldReturnsFiles(t *testing.T) {
	t.Parallel()

	routed, ids, _ := foldRoutingDB(4)
	filter := foldFilter()
	filter.FileFold = nil
	loaded, stats, err := NewFactStore(routed).ListActiveSupplyChainImpactFactsWithStats(context.Background(), filter)
	if err != nil {
		t.Fatalf("error = %v, want nil", err)
	}
	if len(loaded) != len(ids)+1 {
		t.Fatalf("loaded %d facts, want %d (files returned)", len(loaded), len(ids)+1)
	}
	if got := len(routed.callsFor("payload_page_ordered")); got != 0 {
		t.Fatalf("ordered pages = %d, want 0 without a fold", got)
	}
	if stats.FoldedFiles != 0 || stats.FoldedPayloadBytes != 0 {
		t.Fatalf("stats folded = %d files, %d bytes, want zero", stats.FoldedFiles, stats.FoldedPayloadBytes)
	}
	if stats.RetainedPayloadBytes <= foldTestFilePadding {
		t.Fatalf("stats.RetainedPayloadBytes = %d, want file payload bytes counted as retained", stats.RetainedPayloadBytes)
	}
}

func foldFactIDs(envelopes []facts.Envelope) []string {
	out := make([]string, 0, len(envelopes))
	for _, envelope := range envelopes {
		out = append(out, envelope.FactID)
	}
	return out
}

// TestListActiveSupplyChainImpactFactsRecordsPayloadBytesPerBranch pins the
// #9781 operator signal: the payload-bytes histogram records the raw bytes of
// each payload-reading statement under the closed branch label, so an operator
// can tell folded file bytes from retained payload bytes.
func TestListActiveSupplyChainImpactFactsRecordsPayloadBytesPerBranch(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("fold-test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	routed, _, sizes := foldRoutingDB(3)
	_, stats, err := NewFactStore(routed).WithInstruments(instruments).ListActiveSupplyChainImpactFactsWithStats(context.Background(), foldFilter())
	if err != nil {
		t.Fatalf("error = %v, want nil", err)
	}
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	got := map[string]int64{}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != telemetry.MetricSupplyChainImpactActiveEvidencePayloadBytes {
				continue
			}
			for _, point := range m.Data.(metricdata.Histogram[int64]).DataPoints {
				branch, _ := point.Attributes.Value(telemetry.MetricDimensionBranch)
				got[branch.AsString()] += point.Sum
			}
		}
	}
	var wantFolded int64
	for _, size := range sizes {
		wantFolded += int64(size)
	}
	if got[supplychaincore.SupplyChainImpactActiveEvidenceFileFold] != wantFolded || stats.FoldedPayloadBytes != wantFolded {
		t.Fatalf("file_fold bytes metric = %d, stats = %d, want %d", got[supplychaincore.SupplyChainImpactActiveEvidenceFileFold], stats.FoldedPayloadBytes, wantFolded)
	}
	if got[supplychaincore.SupplyChainImpactActiveEvidencePayloadPage] != stats.RetainedPayloadBytes || stats.RetainedPayloadBytes == 0 {
		t.Fatalf("payload_page bytes metric = %d, retained stats = %d, want equal and nonzero", got[supplychaincore.SupplyChainImpactActiveEvidencePayloadPage], stats.RetainedPayloadBytes)
	}
}

// TestListActiveSupplyChainImpactFactsStopsPayloadPagesAtTheByteBudget pins the
// per-page charge (#9781): once the kept raw payload bytes pass the filter's
// limit, the loader keeps the page that crossed it and reads no later core
// page, identity page, or suppression page. A zero limit loads everything, so
// the bounded run is not just a short fixture.
func TestListActiveSupplyChainImpactFactsStopsPayloadPagesAtTheByteBudget(t *testing.T) {
	t.Parallel()

	build := func() *supplyChainImpactRoutingDB {
		routed := identityRoutingDB(3)
		routed.payloads = map[string][]byte{}
		var rows [][2]string
		for n := 0; n < 3*supplyChainImpactPayloadPageSize/2; n++ {
			id := fmt.Sprintf("vuln-affected-package:%05d", n)
			routed.facts[id] = "vulnerability.affected_package"
			routed.payloads[id] = []byte(`{"id":"` + id + `","pad":"` + strings.Repeat("x", 200) + `"}`)
			rows = append(rows, [2]string{id, "vulnerability.affected_package"})
		}
		routed.branchRows["vulnerability_cve_id"] = rows
		return routed
	}
	filter := supplychaincore.SupplyChainImpactFactFilter{CVEIDs: []string{"CVE-2026-9781"}, SubjectDigests: []string{"sha256:a"}}

	unlimited := build()
	all, _, err := NewFactStore(unlimited).ListActiveSupplyChainImpactFactsWithStats(context.Background(), filter)
	if err != nil {
		t.Fatalf("unlimited load error = %v, want nil", err)
	}
	if got := len(unlimited.callsFor("payload_page")); got < 3 {
		t.Fatalf("unlimited core and suppression payload pages = %d, want at least 3", got)
	}

	bounded := build()
	filter.PayloadBytesLimit = 1000 // one 500-fact page is far above it
	loaded, stats, err := NewFactStore(bounded).ListActiveSupplyChainImpactFactsWithStats(context.Background(), filter)
	if err != nil {
		t.Fatalf("bounded load error = %v, want nil", err)
	}
	if got := len(bounded.callsFor("payload_page")); got != 1 {
		t.Fatalf("bounded payload pages = %d, want 1: the page that crossed the limit is kept and no later page is read", got)
	}
	if got := len(bounded.callsFor("identity")); got != 0 {
		t.Fatalf("bounded identity pages = %d, want 0 after the budget was spent", got)
	}
	if len(loaded) != supplyChainImpactPayloadPageSize || len(loaded) >= len(all) {
		t.Fatalf("bounded load kept %d facts of %d, want exactly one page", len(loaded), len(all))
	}
	if stats.RetainedPayloadBytes <= filter.PayloadBytesLimit {
		t.Fatalf("stats.RetainedPayloadBytes = %d, want the crossing page counted above the limit %d", stats.RetainedPayloadBytes, filter.PayloadBytesLimit)
	}
}

// TestListActiveSupplyChainImpactFactsReadsEveryPageAtTheExactByteLimit pins
// the loader's side of the boundary the handler shares: a call that has kept
// exactly the limit is not over it, so the next page is still read and the load
// is complete. A loader that stopped at equality would drop core pages while
// the handler, which only treats bytes strictly over the limit as spent, would
// report the pass complete and retract (#9855).
func TestListActiveSupplyChainImpactFactsReadsEveryPageAtTheExactByteLimit(t *testing.T) {
	t.Parallel()

	const pad = 120
	build := func() *supplyChainImpactRoutingDB {
		routed := identityRoutingDB(0)
		routed.payloads = map[string][]byte{}
		var rows [][2]string
		for n := 0; n < 3*supplyChainImpactPayloadPageSize/2; n++ {
			id := fmt.Sprintf("vuln-affected-package:%05d", n)
			routed.facts[id] = "vulnerability.affected_package"
			// Equal-length payloads, so every full page keeps the same bytes.
			routed.payloads[id] = []byte(`{"id":"` + id + `","pad":"` + strings.Repeat("x", pad) + `"}`)
			rows = append(rows, [2]string{id, "vulnerability.affected_package"})
		}
		routed.branchRows["vulnerability_cve_id"] = rows
		return routed
	}
	filter := supplychaincore.SupplyChainImpactFactFilter{CVEIDs: []string{"CVE-2026-9855"}}

	unlimited := build()
	all, _, err := NewFactStore(unlimited).ListActiveSupplyChainImpactFactsWithStats(context.Background(), filter)
	if err != nil {
		t.Fatalf("unlimited load error = %v, want nil", err)
	}
	pages := len(unlimited.callsFor("payload_page"))
	if pages < 2 {
		t.Fatalf("unlimited payload pages = %d, want at least 2 so a page follows the first", pages)
	}
	// A limit of 1 keeps exactly the first page; one byte more keeps the first
	// two (the second page crosses). Their bytes are the boundary: a limit equal
	// to them leaves the call exactly spent, with a third page still to read.
	probeFilter := filter
	probeFilter.PayloadBytesLimit = 1
	_, firstProbe, err := NewFactStore(build()).ListActiveSupplyChainImpactFactsWithStats(context.Background(), probeFilter)
	if err != nil {
		t.Fatalf("first probe load error = %v, want nil", err)
	}
	probeFilter.PayloadBytesLimit = firstProbe.RetainedPayloadBytes + 1
	_, secondProbe, err := NewFactStore(build()).ListActiveSupplyChainImpactFactsWithStats(context.Background(), probeFilter)
	if err != nil {
		t.Fatalf("second probe load error = %v, want nil", err)
	}
	firstPage := secondProbe.RetainedPayloadBytes

	exact := build()
	filter.PayloadBytesLimit = firstPage
	loaded, _, err := NewFactStore(exact).ListActiveSupplyChainImpactFactsWithStats(context.Background(), filter)
	if err != nil {
		t.Fatalf("exact-limit load error = %v, want nil", err)
	}
	if got := len(exact.callsFor("payload_page")); got != pages {
		t.Fatalf("payload pages at the exact limit = %d, want all %d: bytes equal to the limit are not over it", got, pages)
	}
	if len(loaded) != len(all) {
		t.Fatalf("exact-limit load kept %d facts, want all %d", len(loaded), len(all))
	}
}
