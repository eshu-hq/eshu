// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/cicdrun"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// TestProbe6702HistogramCountsTiers pins the probe's per-digest tier
// histogram over the live corpus shape: 19 deploying-repository rows plus
// one CI-scope build row for the pinned digest, plus one unrelated digest
// that must not leak into the counts.
func TestProbe6702HistogramCountsTiers(t *testing.T) {
	t.Parallel()

	const (
		deployRepo = "repository:r_217415d9"
		buildRepo  = "repository:r_69256c06"
		decoy      = "oci-registry://registry.example/6702-probe-app"
		other      = "sha256:67020000000000000000000000000000000000000000000000000000000000ff"
	)

	envelopes := []facts.Envelope{
		containerImageIdentityImpactFactWithBuildProvenance(
			"probe-ci", anchorProbe6702Digest, decoy, buildRepo, deployRepo, buildRepo,
		),
	}
	for i := 0; i < 19; i++ {
		envelopes = append(envelopes,
			containerImageIdentityImpactFactWithSourceRepositoryIDs(
				"probe-deploy-"+string(rune('a'+i%26))+string(rune('a'+i/26)),
				anchorProbe6702Digest, decoy, deployRepo,
			),
		)
	}
	envelopes = append(envelopes,
		containerImageIdentityImpactFactWithSourceRepositoryIDs(
			"probe-other", other, decoy, deployRepo,
		),
	)

	got := probeSupplyChainAnchor6702(envelopes, nil)
	if !got.digestPresent {
		t.Fatal("digestPresent = false, want true for the pinned digest")
	}
	if got.identityRows != 20 {
		t.Fatalf("identityRows = %d, want 20", got.identityRows)
	}
	if got.tierA != 19 {
		t.Fatalf("tierA = %d, want 19", got.tierA)
	}
	if got.tierB != 1 {
		t.Fatalf("tierB = %d, want 1", got.tierB)
	}
	if got.tierC != 0 {
		t.Fatalf("tierC = %d, want 0", got.tierC)
	}
	if !got.winnerFound {
		t.Fatal("winnerFound = false, want true")
	}
	if got.winnerTier != 0 {
		t.Fatalf("winnerTier = %d, want 0 (tier A)", got.winnerTier)
	}
	if got.winnerRepository != deployRepo {
		t.Fatalf("winnerRepository = %q, want %q", got.winnerRepository, deployRepo)
	}
}

// TestProbe6702ClassifySeparatesH1FromH2 pins the H1 vs H2 discrimination
// the instrumented gate probe exists to provide.
func TestProbe6702ClassifySeparatesH1FromH2(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name              string
		result            anchorProbe6702Result
		floorArmed        bool
		batchProducerHits int
		want              anchorProbe6702Class
	}{
		{
			name: "healthy pinned finding",
			result: anchorProbe6702Result{
				digestPresent: true, identityRows: 20, tierA: 19, tierB: 1,
				winnerFound: true, winnerTier: 0, winnerRepository: "repository:r_217415d9",
				findingPresent: true, findingRepository: "repository:r_217415d9",
			},
			floorArmed: true, batchProducerHits: 20,
			want: anchorProbe6702Healthy,
		},
		{
			name: "H1b later-stage no floor",
			result: anchorProbe6702Result{
				digestPresent: false, identityRows: 0,
				findingPresent: true, findingRepository: "",
			},
			floorArmed: false, batchProducerHits: 0,
			want: anchorProbe6702H1bNoFloor,
		},
		{
			name: "H1a batch-wide disarm",
			result: anchorProbe6702Result{
				digestPresent: false, identityRows: 0,
				findingPresent: true, findingRepository: "",
			},
			floorArmed: true, batchProducerHits: 3,
			want: anchorProbe6702H1aBatchDisarm,
		},
		{
			name: "H2 all rows tier C",
			result: anchorProbe6702Result{
				digestPresent: true, identityRows: 4, tierC: 4,
				winnerFound: true, winnerTier: 2,
				findingPresent: true, findingRepository: "",
			},
			floorArmed: true, batchProducerHits: 4,
			want: anchorProbe6702H2AllTierC,
		},
		{
			name:              "probe not applicable without digest or finding",
			result:            anchorProbe6702Result{},
			floorArmed:        false,
			batchProducerHits: 0,
			want:              anchorProbe6702NotApplicable,
		},
		{
			name: "evidence absent while armed",
			result: anchorProbe6702Result{
				digestPresent: false, identityRows: 0,
				findingPresent: true, findingRepository: "",
			},
			floorArmed: true, batchProducerHits: 0,
			want: anchorProbe6702EvidenceAbsent,
		},
		{
			name: "wrong non-empty winner",
			result: anchorProbe6702Result{
				digestPresent: true, identityRows: 20, tierA: 19, tierB: 1,
				winnerFound: true, winnerTier: 0, winnerRepository: "repository:r_69256c06",
				findingPresent: true, findingRepository: "repository:r_69256c06",
			},
			floorArmed: true, batchProducerHits: 20,
			want: anchorProbe6702WrongWinner,
		},
		{
			name: "rows present but winner lost outside tier C",
			result: anchorProbe6702Result{
				digestPresent: true, identityRows: 3, tierA: 2, tierB: 1,
				winnerFound: true, winnerTier: 0, winnerRepository: "",
				findingPresent: true, findingRepository: "",
			},
			floorArmed: true, batchProducerHits: 3,
			want: anchorProbe6702UnexpectedTier,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := classifyAnchorProbe6702(tc.result, tc.floorArmed, tc.batchProducerHits); got != tc.want {
				t.Fatalf("classifyAnchorProbe6702() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestProbe6702EndToEndH2AllTierC drives probe plus classify over
// genuinely ambiguous writer output for the pinned digest: every row names
// two repositories with no build provenance, so each row is tier C and the
// anchor stays empty. This is the H2 writer-variance signature the gate
// probe must recognize, exercised through the production functions rather
// than a hand-built result.
func TestProbe6702EndToEndH2AllTierC(t *testing.T) {
	t.Parallel()

	const decoy = "oci-registry://registry.example/6702-probe-h2"
	envelopes := []facts.Envelope{
		containerImageIdentityImpactFactWithSourceRepositoryIDs(
			"probe-h2-1", anchorProbe6702Digest, decoy,
			"repository:r_217415d9", "repository:r_69256c06",
		),
		containerImageIdentityImpactFactWithSourceRepositoryIDs(
			"probe-h2-2", anchorProbe6702Digest, decoy,
			"repository:r_69256c06", "repository:r_217415d9",
		),
	}
	findings := []SupplyChainImpactFinding{{
		CVEID:        anchorProbe6702CVE,
		RepositoryID: "",
	}}
	result := probeSupplyChainAnchor6702(envelopes, findings)
	if result.identityRows != 2 || result.tierC != 2 {
		t.Fatalf("histogram = %+v, want 2 rows both tier C", result)
	}
	if got := classifyAnchorProbe6702(result, true, 2); got != anchorProbe6702H2AllTierC {
		t.Fatalf("class = %q, want %q", got, anchorProbe6702H2AllTierC)
	}
}

// TestProbe6702EndToEndH1bNoFloor drives probe plus classify over a pass
// with no pinned-digest identity evidence at all and an unarmed floor: the
// later-stage no-floor residual window from
// docs/internal/evidence/5709-supply-chain-consumer.md.
func TestProbe6702EndToEndH1bNoFloor(t *testing.T) {
	t.Parallel()

	findings := []SupplyChainImpactFinding{{
		CVEID:         anchorProbe6702CVE,
		RepositoryID:  "",
		SubjectDigest: anchorProbe6702Digest,
	}}
	result := probeSupplyChainAnchor6702(nil, findings)
	if result.digestPresent || result.identityRows != 0 {
		t.Fatalf("result = %+v, want no digest evidence", result)
	}
	if got := classifyAnchorProbe6702(result, false, 0); got != anchorProbe6702H1bNoFloor {
		t.Fatalf("class = %q, want %q", got, anchorProbe6702H1bNoFloor)
	}
}

// TestCrossScopeProducerDelta6702SumsDeltas proves the H1a discriminator:
// the batch-wide delta subtracts each producer's pre-load baseline, so a
// producer fact already sitting in the consumer's own scope does not count
// as cross-scope evidence.
func TestCrossScopeProducerDelta6702SumsDeltas(t *testing.T) {
	t.Parallel()

	before := map[reducercontract.Domain]int{
		reducercontract.DomainContainerImageIdentity: 1,
		reducercontract.DomainCICDRunCorrelation:     0,
	}
	envelopes := []facts.Envelope{
		{FactKind: reducercontract.ContainerImageIdentityFactKind},
		{FactKind: reducercontract.ContainerImageIdentityFactKind},
		{FactKind: reducercontract.ContainerImageIdentityFactKind},
		{FactKind: cicdrun.CICDRunCorrelationFactKind},
	}
	if got := crossScopeProducerDelta6702(before, envelopes); got != 3 {
		t.Fatalf("crossScopeProducerDelta6702() = %d, want 3 (2 identity + 1 cicd)", got)
	}
	if got := crossScopeProducerDelta6702(nil, nil); got != 0 {
		t.Fatalf("crossScopeProducerDelta6702(nil, nil) = %d, want 0", got)
	}
}

// TestEmitAnchorProbe6702Bounded proves the emission bounds: a nil logger
// never panics, a pass untouched by the pinned digest stays silent, and a
// pass carrying the pinned identity row emits the probe line with its class.
func TestEmitAnchorProbe6702Bounded(t *testing.T) {
	t.Parallel()

	intent := reducercontract.Intent{
		Domain:       reducercontract.DomainSupplyChainImpact,
		ScopeID:      "probe-scope",
		GenerationID: "probe-generation",
	}

	// Nil logger is a no-op, never a panic.
	emitAnchorProbe6702(context.Background(), nil, intent, true, 1, false,
		[]facts.Envelope{{
			FactKind: reducercontract.ContainerImageIdentityFactKind,
			Payload:  map[string]any{"digest": anchorProbe6702Digest},
		}}, nil)

	silent := &bytes.Buffer{}
	emitAnchorProbe6702(context.Background(),
		slog.New(slog.NewTextHandler(silent, nil)),
		intent, false, 0, false, nil, nil)
	if silent.Len() != 0 {
		t.Fatalf("emit wrote %d bytes for an untouched pass, want silence", silent.Len())
	}

	emitted := &bytes.Buffer{}
	emitAnchorProbe6702(context.Background(),
		slog.New(slog.NewTextHandler(emitted, nil)),
		intent, true, 1, false,
		[]facts.Envelope{
			containerImageIdentityImpactFactWithSourceRepositoryIDs(
				"probe-emit", anchorProbe6702Digest,
				"oci-registry://registry.example/6702-probe-emit",
				"repository:r_217415d9",
			),
		},
		[]SupplyChainImpactFinding{{
			CVEID:         anchorProbe6702CVE,
			RepositoryID:  anchorProbe6702Repository,
			SubjectDigest: anchorProbe6702Digest,
		}})
	if got := emitted.String(); !bytes.Contains([]byte(got), []byte("supply_chain_anchor_probe_6702")) ||
		!bytes.Contains([]byte(got), []byte("probe_class=healthy")) {
		t.Fatalf("emit line = %q, want the probe message with probe_class=healthy", got)
	}
}
