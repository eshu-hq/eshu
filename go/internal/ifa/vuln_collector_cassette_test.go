// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	sdkcollector "github.com/eshu-hq/eshu-sdk/collector"
	"github.com/eshu-hq/eshu/go/internal/factenvelope"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/replay"
	"github.com/eshu-hq/eshu/go/internal/replay/cassette"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// The recording's path and scope ids are declared beside the Odùs that select
// from it (vuln_collector_odu.go), so the catalog and these pins share one
// spelling of each.

// vulnCollectorInstanceID and vulnCollectorComponentID are the instance and
// component the recording ran under; the host stamps them as scope metadata.
const (
	vulnCollectorInstanceID  = "vulnerability-intelligence-recording"
	vulnCollectorComponentID = "dev.eshu.collector.vulnerability_intelligence"
)

func vulnCollectorCassettePath(t *testing.T) string {
	t.Helper()
	return filepath.Join(repoRootDir(t), filepath.FromSlash(vulnCollectorCassetteRelPath))
}

func readVulnCollectorCassette(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile(vulnCollectorCassettePath(t))
	if err != nil {
		t.Fatalf("read the vulnerability collector cassette: %v", err)
	}
	return raw
}

func TestVulnCollectorCassetteLoads(t *testing.T) {
	t.Parallel()

	file, err := cassette.LoadFile(vulnCollectorCassettePath(t))
	if err != nil {
		t.Fatalf("cassette.LoadFile() error = %v, want nil", err)
	}
	if file.Collector != "vulnerability_intelligence" {
		t.Errorf("collector = %q, want vulnerability_intelligence", file.Collector)
	}
	want := []string{vulnWindow1Scope, vulnWindow2Scope, vulnWindowXScope, vulnKEVScope, vulnEPSSScope}
	slices.Sort(want)
	got := make([]string, 0, len(file.Scopes))
	for _, sc := range file.Scopes {
		got = append(got, sc.ScopeID)
		if sc.SourceSystem != "vulnerability_intelligence" || sc.ScopeKind != "vulnerability_intelligence" ||
			sc.CollectorKind != "vulnerability_intelligence" {
			t.Errorf("%s: source_system/scope_kind/collector_kind = %q/%q/%q, want vulnerability_intelligence for all three: "+
				"the public-advisory commit boundary refuses any other triple",
				sc.ScopeID, sc.SourceSystem, sc.ScopeKind, sc.CollectorKind)
		}
		if len(sc.Facts) == 0 {
			t.Errorf("%s: no facts; an empty scope would make every assertion on it vacuous", sc.ScopeID)
		}
	}
	// Scopes are written sorted by scope_id, so the order is part of the bytes.
	if !slices.Equal(got, want) {
		t.Errorf("scope ids = %v, want %v", got, want)
	}
}

func TestVulnCollectorCassetteIsCanonical(t *testing.T) {
	t.Parallel()

	raw := readVulnCollectorCassette(t)
	canonical, err := replay.Canonicalize(raw, replay.DefaultCanonicalOptions())
	if err != nil {
		t.Fatalf("replay.Canonicalize() error = %v, want nil", err)
	}
	if !bytes.Equal(raw, canonical) {
		t.Errorf("the committed cassette is not a fixed point of replay.Canonicalize: the satellite's writer and " +
			"Eshu's disagree, or the file was edited by hand; re-record with `make ifa-cassette` in the collector repository")
	}
}

func TestVulnCollectorCassetteDigestSidecarMatches(t *testing.T) {
	t.Parallel()

	raw := readVulnCollectorCassette(t)
	sidecar, err := os.ReadFile(vulnCollectorCassettePath(t) + ".sha256")
	if err != nil {
		t.Fatalf("read the digest sidecar: %v", err)
	}
	sum := sha256.Sum256(raw)
	want := hex.EncodeToString(sum[:]) + "  " + filepath.Base(vulnCollectorCassetteRelPath) + "\n"
	if string(sidecar) != want {
		t.Errorf("sidecar = %q, want %q: a hand edit of the cassette must change the sidecar in the same diff, "+
			"and the sidecar is the sha256sum -c format", sidecar, want)
	}
}

// vulnStableIdentity returns the identity the collector hashes into the stable
// key of one recorded fact, rebuilt from the payload. These are the six kinds
// the recording carries.
func vulnStableIdentity(kind string, payload map[string]any) (map[string]any, bool) {
	str := func(key string) string { s, _ := payload[key].(string); return s }
	switch kind {
	case "vulnerability.cve", "vulnerability.known_exploited":
		return map[string]any{"cve_id": str("cve_id"), "source": str("source")}, true
	case "vulnerability.affected_product":
		return map[string]any{
			"cve_id": str("cve_id"), "source": str("source"),
			"match_criteria_id": str("match_criteria_id"), "criteria": str("criteria"),
		}, true
	case "vulnerability.reference":
		return map[string]any{"cve_id": str("cve_id"), "source": str("source"), "url": str("url")}, true
	case "vulnerability.epss_score":
		return map[string]any{"cve_id": str("cve_id"), "score_date": str("score_date"), "source": str("source")}, true
	case "vulnerability.source_snapshot":
		identity := map[string]any{"source": str("source")}
		if ecosystem := str("ecosystem"); ecosystem != "" {
			identity["ecosystem"] = ecosystem
		}
		return identity, true
	}
	return nil, false
}

// TestVulnCollectorCassetteStableKeysMatchFactsStableID recomputes every
// recorded stable key with Eshu's own facts.StableID. The satellite carries its
// own copy of the algorithm, so this is the proof the two agree on real data.
func TestVulnCollectorCassetteStableKeysMatchFactsStableID(t *testing.T) {
	t.Parallel()

	file, err := cassette.LoadFile(vulnCollectorCassettePath(t))
	if err != nil {
		t.Fatalf("cassette.LoadFile() error = %v, want nil", err)
	}
	checked := 0
	for _, sc := range file.Scopes {
		for _, fact := range sc.Facts {
			identity, ok := vulnStableIdentity(fact.FactKind, fact.Payload)
			if !ok {
				t.Errorf("%s: no stable identity rule for fact kind %q; add one so the key stays checked", sc.ScopeID, fact.FactKind)
				continue
			}
			if want := facts.StableID(fact.FactKind, identity); fact.StableFactKey != want {
				t.Errorf("%s: %s stable_fact_key = %s, want facts.StableID(%v) = %s",
					sc.ScopeID, fact.FactKind, fact.StableFactKey, identity, want)
			}
			checked++
		}
	}
	if checked == 0 {
		t.Fatal("no fact was checked")
	}
}

// TestVulnCollectorCassetteHostMappingPin pins how far a cassette replay
// differs from the real extension host. It rebuilds each recorded fact as the
// SDK fact the satellite returned, maps it with the host's
// factenvelope.InternalFromSDKFact, and compares with the envelope the cassette
// source builds: the fact id namespace is the one known difference (a replay
// stamps CassetteReplay ids, the host CollectorExtensionFact ids), and every
// other field must agree.
func TestVulnCollectorCassetteHostMappingPin(t *testing.T) {
	t.Parallel()

	source, err := cassette.NewSource(vulnCollectorCassettePath(t))
	if err != nil {
		t.Fatalf("cassette.NewSource() error = %v, want nil", err)
	}
	compared := 0
	for _, sc := range source.File.Scopes {
		generation, ok, err := source.Next(context.Background())
		if err != nil || !ok {
			t.Fatalf("source.Next() = (ok %v, error %v), want the next scope", ok, err)
		}
		assertHostScopeFields(t, sc, generation.Scope, generation.Generation)
		replayed := make(map[string]facts.Envelope)
		for envelope := range generation.Facts {
			replayed[envelope.FactKind+"\x00"+envelope.StableFactKey] = envelope
		}
		for _, fact := range sc.Facts {
			sdkFact := sdkcollector.Fact{
				Kind: fact.FactKind, SchemaVersion: fact.SchemaVersion, StableKey: fact.StableFactKey,
				SourceConfidence: sdkcollector.SourceConfidence(fact.SourceConfidence),
				ObservedAt:       sc.ObservedAt, Payload: fact.Payload,
				SourceRef: sdkcollector.SourceRef{
					SourceSystem: sc.SourceSystem, ScopeID: sc.ScopeID, GenerationID: sc.GenerationID,
					FactKey: fact.StableFactKey, URI: fact.SourceURI, RecordID: recordIDOrKey(fact),
				},
			}
			hosted := factenvelope.InternalFromSDKFact(sdkFact, factenvelope.InternalEnvelopeOptions{
				ComponentID: vulnCollectorComponentID, ScopeID: sc.ScopeID, GenerationID: sc.GenerationID,
				CollectorKind: sc.CollectorKind, FencingToken: 1,
			})
			got := replayed[fact.FactKind+"\x00"+fact.StableFactKey]
			if got.FactID == hosted.FactID {
				t.Errorf("%s: replay and host stamp the same fact id %q; the namespaces are expected to differ", sc.ScopeID, got.FactID)
			}
			// The fact id is the one allowed delta: align it and compare the rest.
			hosted.FactID = got.FactID
			if !equalEnvelopes(got, hosted) {
				t.Errorf("%s: %s replay envelope differs from the host mapping:\n replay: %+v\n  host: %+v",
					sc.ScopeID, fact.FactKind, got, hosted)
			}
			compared++
		}
	}
	if compared == 0 {
		t.Fatal("no fact was compared")
	}
}

// assertHostScopeFields checks the three scope fields the host owns against
// the cassette replay's scope and generation: the partition key is the
// scheduler's fairness key, the metadata names the component and instance, and
// a collected generation is a snapshot.
func assertHostScopeFields(t *testing.T, sc cassette.Scope, ingestion scope.IngestionScope, generation scope.ScopeGeneration) {
	t.Helper()
	source := "nvd"
	switch sc.ScopeID {
	case vulnKEVScope:
		source = "cisa_kev"
	case vulnEPSSScope:
		source = "first_epss"
	}
	wantPartition := "vulnerability_intelligence:" + vulnCollectorInstanceID + ":" + source
	if ingestion.PartitionKey != wantPartition {
		t.Errorf("%s: partition key = %q, want the fairness key %q", sc.ScopeID, ingestion.PartitionKey, wantPartition)
	}
	if ingestion.Metadata["component_id"] != vulnCollectorComponentID || ingestion.Metadata["instance_id"] != vulnCollectorInstanceID ||
		len(ingestion.Metadata) != 2 {
		t.Errorf("%s: metadata = %v, want component_id and instance_id only", sc.ScopeID, ingestion.Metadata)
	}
	if generation.TriggerKind != scope.TriggerKindSnapshot {
		t.Errorf("%s: trigger kind = %q, want %q", sc.ScopeID, generation.TriggerKind, scope.TriggerKindSnapshot)
	}
}

func recordIDOrKey(fact cassette.Fact) string {
	if strings.TrimSpace(fact.SourceRecordID) != "" {
		return fact.SourceRecordID
	}
	return fact.StableFactKey
}

func equalEnvelopes(a, b facts.Envelope) bool {
	return a.ScopeID == b.ScopeID && a.GenerationID == b.GenerationID && a.FactKind == b.FactKind &&
		a.StableFactKey == b.StableFactKey && a.SchemaVersion == b.SchemaVersion &&
		a.CollectorKind == b.CollectorKind && a.FencingToken == b.FencingToken &&
		a.SourceConfidence == b.SourceConfidence && a.ObservedAt.Equal(b.ObservedAt) &&
		a.IsTombstone == b.IsTombstone && a.SourceRef == b.SourceRef && payloadsEqual(a.Payload, b.Payload)
}

func payloadsEqual(a, b map[string]any) bool {
	left, _ := replay.CanonicalizeValue(a, replay.DefaultCanonicalOptions())
	right, _ := replay.CanonicalizeValue(b, replay.DefaultCanonicalOptions())
	return left != nil && bytes.Equal(left, right)
}
