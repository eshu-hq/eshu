// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backendconformance

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/queryplan"
)

// Identity-keyed read exemptions (#7233): an exemption names the builder
// identity (go-relative path:symbol of the direct Run/RunSingle caller)
// plus a stable anchor fragment, never the full statement text. A
// projection edit that keeps the anchor leaves the exemption working;
// a renamed or deleted callsite fails as stale instead of rotting.

const (
	identityCallsite = "internal/query/entity/context_handler.go:(*Handler).GetEntityContext"
	identityAnchor   = "MATCH (e) WHERE e.id = $entity_id"
	identityText     = "MATCH (e) WHERE e.id = $entity_id OPTIONAL MATCH (e)-[rel]->(target) RETURN e.id as id, collect(rel) as relationships"
	identityOther    = "internal/query/other.go:Other"
)

func identityManifest() queryplan.BuilderManifest {
	return queryplan.BuilderManifest{
		Version: 1,
		ReadExemptions: []queryplan.ReadExemption{{
			Callsite: identityCallsite,
			Anchor:   identityAnchor,
			Reason:   "miss-path read over an absent entity",
		}},
	}
}

func identityRead(callsite, statement string, rows int) DifferentialRecord {
	digest := "empty-digest"
	if rows > 0 {
		digest = "rows-digest"
	}
	return DifferentialRecord{
		Backend:     "neo4j",
		Callsite:    callsite,
		Fingerprint: DifferentialFingerprint{Statement: statement, Parameters: `{}`},
		RowCount:    rows,
		Digest:      digest,
	}
}

// (a) identity plus anchor excuses: the same callsite issuing the anchored
// read with zero rows everywhere is the exempted miss path.
func TestIdentityExemptionExcusesAnchoredMissPath(t *testing.T) {
	records := map[string][]DifferentialRecord{"neo4j": {
		identityRead(identityCallsite, identityText, 0),
	}}
	if failures := ComputeStatementCoverage(identityManifest(), records).Failures(); len(failures) != 0 {
		t.Fatalf("Failures() = %v, want none: identity plus anchor excuses the miss path", failures)
	}
}

// (b) wrong identity fails: the same text from another callsite is a
// different read and stays an always-empty failure naming its identity.
func TestIdentityExemptionRejectsWrongCallsite(t *testing.T) {
	records := map[string][]DifferentialRecord{"neo4j": {
		identityRead(identityOther, identityText, 0),
	}}
	failures := ComputeStatementCoverage(identityManifest(), records).Failures()
	assertFailure(t, failures, "neo4j", CoverageAlwaysEmptyRead, identityOther+" :: "+identityText)
	assertFailure(t, failures, "", CoverageStaleExemption, identityCallsite+" :: "+identityAnchor)
	if len(failures) != 2 {
		t.Fatalf("Failures() = %v, want the always-empty read plus the stale exemption", failures)
	}
}

// (c) same identity with a disjoint anchor fails twice: the rewritten
// statement is an unexcused always-empty read, and the exemption anchor
// now matches nothing the callsite produces, so it is stale too. Both
// point at updating the exemption, which is the anti-rot mechanism.
func TestIdentityExemptionRejectsNovelAnchor(t *testing.T) {
	novel := "MATCH (z:Zeta) WHERE z.id = $id RETURN z"
	records := map[string][]DifferentialRecord{"neo4j": {
		identityRead(identityCallsite, novel, 0),
	}}
	failures := ComputeStatementCoverage(identityManifest(), records).Failures()
	assertFailure(t, failures, "neo4j", CoverageAlwaysEmptyRead, identityCallsite+" :: "+novel)
	assertFailure(t, failures, "", CoverageStaleExemption, identityCallsite+" :: "+identityAnchor)
	if len(failures) != 2 {
		t.Fatalf("Failures() = %v, want the always-empty read plus the stale exemption", failures)
	}
}

// (d) stale identity fails: an exemption whose callsite has no recordings
// on any backend names deleted or renamed code and must not rot silently.
func TestIdentityExemptionStaleWithoutRecordings(t *testing.T) {
	records := map[string][]DifferentialRecord{"neo4j": {
		identityRead(identityOther, identityText, 1),
	}}
	failures := ComputeStatementCoverage(identityManifest(), records).Failures()
	assertFailure(t, failures, "", CoverageStaleExemption, identityCallsite+" :: "+identityAnchor)
	if len(failures) != 1 {
		t.Fatalf("Failures() = %v, want only the stale exemption", failures)
	}
}

// TestIdentityExemptionStaleNamesAnchorPerExemption (#7233): one callsite
// may hold several exemptions, so a callsite-scoped stale ID would
// collapse them (and duplicate when every one is unused). Stale IDs carry
// the anchor: the used exemption stays silent while the unused one names
// itself.
func TestIdentityExemptionStaleNamesAnchorPerExemption(t *testing.T) {
	const otherAnchor = "MATCH (o:Other) WHERE o.id = $id RETURN o"
	manifest := queryplan.BuilderManifest{Version: 1, ReadExemptions: []queryplan.ReadExemption{
		{Callsite: identityCallsite, Anchor: identityAnchor, Reason: "miss path"},
		{Callsite: identityCallsite, Anchor: otherAnchor, Reason: "other miss path"},
	}}
	records := map[string][]DifferentialRecord{"neo4j": {
		identityRead(identityCallsite, identityText, 0),
	}}
	failures := ComputeStatementCoverage(manifest, records).Failures()
	assertFailure(t, failures, "", CoverageStaleExemption, identityCallsite+" :: "+otherAnchor)
	if len(failures) != 1 {
		t.Fatalf("Failures() = %v, want only the unused anchor's stale exemption", failures)
	}
}

// (e) family key-or-all-members preserved: sibling label members from one
// callsite are one read keyed by the unlabeled family text; the family
// anchor excuses every member.
func TestIdentityExemptionCoversDispatchFamily(t *testing.T) {
	manifest := queryplan.BuilderManifest{
		Version: 1,
		ReadExemptions: []queryplan.ReadExemption{{
			Callsite: "internal/query/entity/handler.go:(*Handler).Probe",
			Anchor:   dispatchUnlabeled,
			Reason:   "probe-by-id over an absent entity",
		}},
	}
	probed := func(statement string, rows int) DifferentialRecord {
		rec := dispatchRead(statement, dispatchParams, rows)
		rec.Callsite = "internal/query/entity/handler.go:(*Handler).Probe"
		return rec
	}
	records := map[string][]DifferentialRecord{"neo4j": {
		probed(dispatchLabelA, 0),
		probed(dispatchLabelB, 0),
		probed(dispatchUnlabeled, 0),
	}}
	if failures := ComputeStatementCoverage(manifest, records).Failures(); len(failures) != 0 {
		t.Fatalf("Failures() = %v, want none: the family anchor excuses every member", failures)
	}
}

// TestExemptionAnchorFloorMatchesFragmentAnchorFloor pins the shared
// rule: exemption anchors and builder fragment sets share one minimum
// anchor length, so a fragment too short to attribute a builder is too
// short to excuse a read.
func TestExemptionAnchorFloorMatchesFragmentAnchorFloor(t *testing.T) {
	if queryplan.MinExemptionAnchorLength != minCoverageAnchorLength {
		t.Fatalf("MinExemptionAnchorLength = %d, want minCoverageAnchorLength = %d",
			queryplan.MinExemptionAnchorLength, minCoverageAnchorLength)
	}
}

// identitySmokeCaller is the attributed caller for the capture smoke test:
// the recorded identity must name exactly this symbol in this file.
func identitySmokeCaller(q GraphQuery) ([]map[string]any, error) {
	return q.Run(context.Background(), "MATCH (n) RETURN n", map[string]any{})
}

// TestCallsiteIdentityFormatsMainPackageFrames pins the main-package
// attribution rule: main-package frames report a bare main. prefix with
// no import path, so the file locates the symbol. Without it every
// cmd/* callsite records "" (observed on reducer/bootstrap/projector
// captures) and every exemption naming one goes stale.
func TestCallsiteIdentityFormatsMainPackageFrames(t *testing.T) {
	goDir, err := queryplan.GoDir()
	if err != nil {
		t.Fatal(err)
	}
	const modulePath = "github.com/eshu-hq/eshu/go"
	frame := runtime.Frame{
		Function: "main.neo4jWorkloadDependencyLookup.ListWorkloadDependencyEdges",
		File:     filepath.Join(goDir, "cmd", "reducer", "workload_dependency_lookup.go"),
	}
	want := "cmd/reducer/workload_dependency_lookup.go:(neo4jWorkloadDependencyLookup).ListWorkloadDependencyEdges"
	if got := callsiteIdentity(goDir, modulePath, frame); got != want {
		t.Fatalf("callsiteIdentity() = %q, want %q", got, want)
	}
	// A main-package frame outside the tree still fails closed.
	outside := runtime.Frame{Function: "main.escapee", File: "/tmp/escapee.go"}
	if got := callsiteIdentity(goDir, modulePath, outside); got != "" {
		t.Fatalf("callsiteIdentity() = %q, want empty", got)
	}
}

// TestCallsiteIdentityCanonicalizesValueReceiver (#7233) is the capture
// side of the receiver-form contract: the runtime renders a value-receiver
// method bare (Type.Method) while manifest authors write the
// method-expression form ((Type).Method), so capture must canonicalize —
// otherwise the recorded callsite never matches its exemption (observed:
// EnumerateProjectedSourceEdges recorded bare, manifest parenthesized).
func TestCallsiteIdentityCanonicalizesValueReceiver(t *testing.T) {
	goDir, err := queryplan.GoDir()
	if err != nil {
		t.Fatal(err)
	}
	const modulePath = "github.com/eshu-hq/eshu/go"
	frame := runtime.Frame{
		Function: modulePath + "/internal/reducer.ProjectedSourceEdgeBackfillReader.EnumerateProjectedSourceEdges",
		File:     filepath.Join(goDir, "internal", "reducer", "projected_source_edge_backfill.go"),
	}
	want := "internal/reducer/projected_source_edge_backfill.go:(ProjectedSourceEdgeBackfillReader).EnumerateProjectedSourceEdges"
	if got := callsiteIdentity(goDir, modulePath, frame); got != want {
		t.Fatalf("callsiteIdentity() = %q, want %q", got, want)
	}
	// Pointer receivers keep their star inside the parens.
	star := runtime.Frame{
		Function: modulePath + "/internal/query/entity.(*Handler).GetEntityContext",
		File:     filepath.Join(goDir, "internal", "query", "entity", "context_handler.go"),
	}
	starWant := "internal/query/entity/context_handler.go:(*Handler).GetEntityContext"
	if got := callsiteIdentity(goDir, modulePath, star); got != starWant {
		t.Fatalf("callsiteIdentity() = %q, want %q", got, starWant)
	}
}

// BenchmarkDisabledPassthroughWrapped and BenchmarkDisabledPassthroughBare
// are the production no-regression proof for capture attribution (#7233):
// with capture disabled WrapGraphQuery returns the inner query unwrapped,
// so a served read must pay no attribution work. The wrapped-vs-bare delta
// must stay at noise level; recordCallsite (below) never runs here.
func BenchmarkDisabledPassthroughWrapped(b *testing.B) {
	inner := stubDifferentialGraphQuery{rows: []map[string]any{{"n": 1}}}
	wrapped := WrapGraphQuery(inner, NewDifferentialRecorder(), "neo4j")
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = wrapped.Run(ctx, "MATCH (n) RETURN n", nil)
	}
}

func BenchmarkDisabledPassthroughBare(b *testing.B) {
	inner := stubDifferentialGraphQuery{rows: []map[string]any{{"n": 1}}}
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = inner.Run(ctx, "MATCH (n) RETURN n", nil)
	}
}

// BenchmarkRecordCallsite bounds the per-read attribution cost added under
// capture (#7233): one runtime.Callers walk plus receiver canonicalization.
// Capture runs only on CI differential legs, so this is a leg-budget check
// (microseconds per read against thousands of reads per leg), not a
// production latency claim — production never calls this function.
func BenchmarkRecordCallsite(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = recordCallsite()
	}
}

// (f) capture smoke: WrapGraphQuery records the direct caller's builder
// identity (first non-infra frame wins): through a helper it names the
// helper, called directly it names the test function itself.
func TestCaptureSmokeRecordsCallerIdentity(t *testing.T) {
	t.Setenv("ESHU_DIFFERENTIAL_CAPTURE", "1")
	recorder := NewDifferentialRecorder()
	query := WrapGraphQuery(stubDifferentialGraphQuery{}, recorder, "nornicdb")
	if _, err := identitySmokeCaller(query); err != nil {
		t.Fatal(err)
	}
	if _, err := query.Run(context.Background(), "MATCH (n) RETURN n", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	records := recorder.Records()
	if len(records) != 2 {
		t.Fatalf("records = %d, want 2", len(records))
	}
	const file = "internal/backendconformance/statement_coverage_identity_test.go"
	if want := file + ":identitySmokeCaller"; records[0].Callsite != want {
		t.Fatalf("helper record.Callsite = %q, want %q", records[0].Callsite, want)
	}
	if want := file + ":TestCaptureSmokeRecordsCallerIdentity"; records[1].Callsite != want {
		t.Fatalf("direct record.Callsite = %q, want %q", records[1].Callsite, want)
	}
}

// identitySmokeOuter calls through identitySmokeMiddle so the smoke test
// pins immediate-caller-wins across distinct helpers: the recorded
// identity names the helper holding the Run call, never the helpers
// above it.
func identitySmokeOuter(q GraphQuery) ([]map[string]any, error) {
	return identitySmokeMiddle(q)
}

func identitySmokeMiddle(q GraphQuery) ([]map[string]any, error) {
	return identitySmokeCaller(q)
}

// TestCaptureSmokeRecordsImmediateCallerIdentity pins attribution across
// distinct helpers: through two helpers the recorded identity is still
// the immediate Run holder.
func TestCaptureSmokeRecordsImmediateCallerIdentity(t *testing.T) {
	t.Setenv("ESHU_DIFFERENTIAL_CAPTURE", "1")
	recorder := NewDifferentialRecorder()
	query := WrapGraphQuery(stubDifferentialGraphQuery{}, recorder, "nornicdb")
	if _, err := identitySmokeOuter(query); err != nil {
		t.Fatal(err)
	}
	records := recorder.Records()
	if len(records) != 1 {
		t.Fatalf("records = %d, want 1", len(records))
	}
	want := "internal/backendconformance/statement_coverage_identity_test.go:identitySmokeCaller"
	if records[0].Callsite != want {
		t.Fatalf("nested record.Callsite = %q, want %q", records[0].Callsite, want)
	}
}
