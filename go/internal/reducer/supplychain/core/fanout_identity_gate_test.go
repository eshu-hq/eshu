// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	reducerderivedv1 "github.com/eshu-hq/eshu-sdk/factschema/reducerderived/v1"

	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// runIdentityGatePass runs one genuine pass of the advisory-only fan-out
// handler whose writer reports result, and returns the port and the counter.
func runIdentityGatePass(t *testing.T, result SupplyChainImpactWriteResult) (*stubFanOut, map[string]int64) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatal(err)
	}
	port := &stubFanOut{holders: []string{"vuln-intel://epss/daily", "vuln-intel://osv/npm"}}
	if _, err := fanOutHandler(port, &changeWriter{result: result}, inst).Handle(context.Background(), fanOutIntent(1, nil)); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	return port, fanOutCount(t, reader)
}

// TestIdentityGateDropsAnEnrichmentOnlyChangeOfANonEnrichmentPass pins D7: an
// advisory scope's pass whose rows changed only in EPSS or KEV fields reads no
// holders, reopens nobody, and counts the dropped id as identity_gated.
func TestIdentityGateDropsAnEnrichmentOnlyChangeOfANonEnrichmentPass(t *testing.T) {
	t.Parallel()

	port, counts := runIdentityGatePass(t, SupplyChainImpactWriteResult{
		ChangedCVEIDs: []string{"CVE-2026-9001"}, TouchedGroupIDs: []string{"CVE-2026-9001"}, IdentityReported: true,
	})
	if len(port.findCalls) != 0 || len(port.reopened) != 0 {
		t.Fatalf("holder reads %v, reopened %v; want none: only enrichment fields changed", port.findCalls, port.reopened)
	}
	if got := counts[fanOutOutcomeIdentityGated]; got != 1 {
		t.Fatalf("identity_gated = %d, want 1 (counts %v)", got, counts)
	}
}

// TestIdentityGateKeepsAnIdentityChange: a row whose identity changed still
// fans out for its id.
func TestIdentityGateKeepsAnIdentityChange(t *testing.T) {
	t.Parallel()

	port, counts := runIdentityGatePass(t, SupplyChainImpactWriteResult{
		ChangedCVEIDs: []string{"CVE-2026-9001"}, IdentityChangedCVEIDs: []string{"CVE-2026-9001"}, IdentityReported: true,
	})
	if !reflect.DeepEqual(port.findCalls, [][]string{{"CVE-2026-9001"}}) {
		t.Fatalf("holder reads = %v, want [[CVE-2026-9001]]", port.findCalls)
	}
	if got := counts[fanOutOutcomeIdentityGated]; got != 0 {
		t.Fatalf("identity_gated = %d, want 0", got)
	}
}

// TestIdentityGateFallsBackToTheFullSetsWithoutIdentitySets: a writer that
// does not report the identity sets fans out on the full sets (fail closed).
func TestIdentityGateFallsBackToTheFullSetsWithoutIdentitySets(t *testing.T) {
	t.Parallel()

	port, _ := runIdentityGatePass(t, SupplyChainImpactWriteResult{ChangedCVEIDs: []string{"CVE-2026-9001"}})
	if !reflect.DeepEqual(port.findCalls, [][]string{{"CVE-2026-9001"}}) {
		t.Fatalf("holder reads = %v, want the full changed set", port.findCalls)
	}
}

// TestFanOutOriginIDsUsesTheFullDigestForAnEnrichmentOrigin pins the origin
// rule: a pass whose own scope facts hold an EPSS or KEV kind fans out on the
// full sets; any other pass fans out on the identity sets.
func TestFanOutOriginIDsUsesTheFullDigestForAnEnrichmentOrigin(t *testing.T) {
	t.Parallel()

	write := SupplyChainImpactWriteResult{
		ChangedCVEIDs: []string{"CVE-1", "CVE-2"}, TouchedGroupIDs: []string{"CVE-2", "GHSA-3"},
		IdentityChangedCVEIDs: []string{"CVE-2"}, IdentityTouchedGroupIDs: []string{"CVE-2"}, IdentityReported: true,
	}
	for _, tc := range []struct {
		name    string
		kinds   []string
		want    []string
		dropped int
	}{
		{"advisory scope", []string{chain.VulnerabilityCVEFactKind, chain.VulnerabilityAffectedPackageFactKind}, []string{"CVE-2"}, 2},
		{"epss scope", []string{chain.VulnerabilityEPSSScoreFactKind}, []string{"CVE-1", "CVE-2", "GHSA-3"}, 0},
		{"kev scope", []string{chain.VulnerabilityKnownExploitedFactKind}, []string{"CVE-1", "CVE-2", "GHSA-3"}, 0},
		{"advisory plus epss", []string{chain.VulnerabilityCVEFactKind, chain.VulnerabilityEPSSScoreFactKind}, []string{"CVE-1", "CVE-2", "GHSA-3"}, 0},
	} {
		got, dropped := fanOutOriginIDs(withFanOutOrigin(write, tc.kinds))
		if !reflect.DeepEqual(got, tc.want) || dropped != tc.dropped {
			t.Errorf("%s: ids %v dropped %d, want %v dropped %d", tc.name, got, dropped, tc.want, tc.dropped)
		}
	}
}

// mutatedPayload round-trips the payload through JSON, mutates one top-level
// key, and decodes it back.
func mutatedPayload(t *testing.T, base reducerderivedv1.SupplyChainImpactFinding, key string) reducerderivedv1.SupplyChainImpactFinding {
	t.Helper()
	encoded, err := json.Marshal(base)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatal(err)
	}
	switch value := fields[key].(type) {
	case string:
		fields[key] = value + "-changed"
	case bool:
		fields[key] = !value
	case float64:
		fields[key] = value + 1
	case []any:
		if len(value) > 0 {
			if _, isMap := value[0].(map[string]any); isMap {
				fields[key] = append(value, map[string]any{"changed": "changed"})
				break
			}
		}
		fields[key] = append(value, "changed")
	case map[string]any:
		value["changed"] = "changed"
	default:
		t.Fatalf("field %q has unmutable value %#v", key, fields[key])
	}
	mutated, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	var out reducerderivedv1.SupplyChainImpactFinding
	if err := json.Unmarshal(mutated, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// TestIdentityDigestClassifiesEveryPayloadField is the D7 coverage guard: every
// top-level payload field is in exactly one class. An enrichment field changes
// the full digest and not the identity digest; every other field the full
// digest covers changes both. A field not named in enrichmentPayloadKeys is
// identity, so a new field fans out until it is classified (fail closed).
func TestIdentityDigestClassifiesEveryPayloadField(t *testing.T) {
	t.Parallel()

	base := filledImpactPayload(t)
	baseFull, baseIdentity := contentDigestOf(base), identityDigestOf(base)
	enrichment := map[string]bool{}
	for _, key := range enrichmentPayloadKeys {
		enrichment[key] = true
	}
	excluded := map[string]bool{}
	for _, key := range DigestExcludedPayloadKeys() {
		excluded[key] = true
	}
	seen := map[string]bool{}
	typ := reflect.TypeOf(base)
	for i := 0; i < typ.NumField(); i++ {
		key := strings.Split(typ.Field(i).Tag.Get("json"), ",")[0]
		seen[key] = true
		mutated := mutatedPayload(t, base, key)
		full, identity := contentDigestOf(mutated), identityDigestOf(mutated)
		switch {
		case excluded[key]:
			if full != baseFull || identity != baseIdentity {
				t.Errorf("digest-excluded field %q changed a digest", key)
			}
		case enrichment[key]:
			if full == baseFull {
				t.Errorf("enrichment field %q left the full digest unchanged: the row write would miss it", key)
			}
			if identity != baseIdentity {
				t.Errorf("enrichment field %q changed the identity digest: an EPSS or KEV refresh would still echo", key)
			}
		default:
			if identity == baseIdentity {
				t.Errorf("identity field %q left the identity digest unchanged: a real change would not fan out", key)
			}
		}
	}
	for key := range enrichment {
		if !seen[key] {
			t.Errorf("enrichmentPayloadKeys names %q, which is not a payload field", key)
		}
	}
}

// TestIdentityDigestClassifiesProvenanceEntries covers the nested entries: the
// EPSS and KEV provenance keys change only the full digest, any other entry
// changes both, and the two stored digests change neither. The selected EPSS
// score date is the one exception (#11598 Fix 2): it differs on every rescan
// without the finding changing, so it changes neither digest.
func TestIdentityDigestClassifiesProvenanceEntries(t *testing.T) {
	t.Parallel()

	base := filledImpactPayload(t)
	baseFull, baseIdentity := contentDigestOf(base), identityDigestOf(base)
	with := func(key string) reducerderivedv1.SupplyChainImpactFinding {
		out := base
		out.Provenance = map[string]any{}
		for k, v := range base.Provenance {
			out.Provenance[k] = v
		}
		out.Provenance[key] = "changed"
		return out
	}
	for key := range enrichmentProvenanceKeys {
		if key == selectedEPSSScoreDateProvenanceKey {
			if contentDigestOf(with(key)) != baseFull || identityDigestOf(with(key)) != baseIdentity {
				t.Errorf("enrichment provenance %q: want both digests unchanged: a score-date-only rescan is not a change", key)
			}
			continue
		}
		if contentDigestOf(with(key)) == baseFull || identityDigestOf(with(key)) != baseIdentity {
			t.Errorf("enrichment provenance %q: want the full digest changed and the identity digest unchanged", key)
		}
	}
	if identityDigestOf(with("selected_severity_source")) == baseIdentity {
		t.Error("identity provenance selected_severity_source left the identity digest unchanged")
	}
	for _, key := range []string{contentDigestProvenanceKey, identityDigestProvenanceKey} {
		if contentDigestOf(with(key)) != baseFull || identityDigestOf(with(key)) != baseIdentity {
			t.Errorf("stored digest key %q changed a digest", key)
		}
	}
}

// TestContentDigestValueIsPinnedForAFixedFinding pins the full content digest
// of one fixed finding to the literal value the code before D7 computed for it
// (#10972). The byte-exact batch payload test recomputes its expectation with
// the production digest, so only a literal can show that adding the identity
// digest left the full digest's value unchanged.
func TestContentDigestValueIsPinnedForAFixedFinding(t *testing.T) {
	t.Parallel()

	write := SupplyChainImpactWrite{
		FencingToken: 1, IntentID: "intent-impact-batch", ScopeID: "vuln-intel://osv/npm/example",
		GenerationID: "generation-impact-batch", SourceSystem: "vulnerability_intelligence", Cause: "vulnerability evidence observed",
	}
	finding := SupplyChainImpactFinding{
		CVEID: "CVE-2026-1001", PackageID: testImpactPackageID, PURL: testImpactPURL, ObservedVersion: "1.2.3",
		Status: SupplyChainImpactAffectedExact, RepositoryID: testImpactRepositoryID, CanonicalWrites: 1,
	}
	const preD7 = "a80d29db2de24987749c5171ea49f5292b9dc4de7d831bbc4344e481baf96575"
	if got := supplyChainImpactContentDigest(write, finding); got != preD7 {
		t.Fatalf("content digest = %s, want the pre-D7 value %s: the full digest's meaning changed", got, preD7)
	}
}
