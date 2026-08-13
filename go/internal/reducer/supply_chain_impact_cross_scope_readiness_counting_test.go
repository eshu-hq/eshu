// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

// The COUNTING half of the #5709 floor on supply_chain_impact: which
// cross-scope envelopes count as "the producer answered", which filter
// dimensions can reach a producer row at all, and which load stages the count
// has to be taken after. The defer/no-defer behaviour these rules drive lives in
// supply_chain_impact_cross_scope_readiness_test.go, which also carries the
// shared fixtures both files use.
//
// This is where this consumer diverges from CICDRunCorrelationHandler, whose
// cross-scope load asks a producer-only reader and can therefore count plain
// envelopes.

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestSupplyChainImpactCountsOnlyProducerOwnedCrossScopeFacts is the difference
// between this consumer and the CI/CD one, and it is easy to get wrong. The
// shared active-evidence reader returns twenty-odd fact kinds, so counting every
// envelope it returned would let a batch that resolved a pile of SBOM components
// and zero producer facts disarm the floor -- the exact case the floor exists
// for.
func TestSupplyChainImpactCountsOnlyProducerOwnedCrossScopeFacts(t *testing.T) {
	t.Parallel()

	loader := &stubSupplyChainImpactFactLoader{
		scopeFacts: supplyChainImpactArmedScopeFacts(),
		// Plenty of cross-scope evidence, none of it from a declared producer.
		active: []facts.Envelope{
			sbomComponentImpactFact("component-1", "doc-1", testImpactPURL),
			sbomAttachmentImpactFact("attachment-1", "doc-1", testImpactSubjectDigest),
		},
	}
	writer := &recordingSupplyChainImpactWriter{}
	handler := SupplyChainImpactHandler{
		FactLoader:        loader,
		Writer:            writer,
		ProducerReadiness: &fixedCrossScopeReadiness{ready: false},
	}

	_, err := handler.Handle(
		context.Background(),
		supplyChainImpactIntentUnderTheFloor("non-producer cross-scope evidence resolved"),
	)
	var notReady crossScopeProducerNotReadyError
	if !errors.As(err, &notReady) {
		t.Fatalf(
			"Handle() error = %#v, want a deferral: only producer-owned facts may disarm the floor",
			err,
		)
	}
	if writer.calls != 0 {
		t.Fatalf("WriteSupplyChainImpactFindings() calls = %d, want 0 on a deferral", writer.calls)
	}
}

// TestSupplyChainImpactCICDCorrelationAlsoDisarmsTheFloor covers the second
// declared producer. supply_chain_impact declares both
// container_image_identity and ci_cd_run_correlation, so a
// reducer_ci_cd_run_correlation envelope has to count as producer output too.
// Counting only identity facts would leave a pass that resolved its deployment
// context deferring for the full bound.
func TestSupplyChainImpactCICDCorrelationAlsoDisarmsTheFloor(t *testing.T) {
	t.Parallel()

	loader := &stubSupplyChainImpactFactLoader{
		scopeFacts: supplyChainImpactArmedScopeFacts(),
		active: []facts.Envelope{
			cicdRunCorrelationImpactFact(
				"cicd-correlation-1",
				testImpactSubjectDigest,
				"registry.example.com/team/api:v1",
				testImpactRepositoryID,
				"production",
				"exact",
			),
		},
	}
	writer := &recordingSupplyChainImpactWriter{}
	handler := SupplyChainImpactHandler{
		FactLoader:        loader,
		Writer:            writer,
		ProducerReadiness: readinessWithOnlyUnready(DomainCICDRunCorrelation),
	}

	if _, err := handler.Handle(
		context.Background(),
		supplyChainImpactIntentUnderTheFloor("ci/cd correlation producer has committed"),
	); err != nil {
		t.Fatalf("Handle() error = %v, want nil: a resolved ci_cd_run_correlation fact is producer output", err)
	}
	if writer.calls != 1 {
		t.Fatalf("WriteSupplyChainImpactFindings() calls = %d, want 1", writer.calls)
	}
}

// TestSupplyChainImpactDoesNotDeferABatchWhereAnotherFindingResolved pins the
// same batch-wide gap the CI/CD consumer discloses, so it stays a documented
// property here rather than something the next maintainer rediscovers from a
// wrong finding.
//
// One Handle pass classifies every vulnerability finding in a scope generation
// and issues its cross-scope loads filtered by the union of every finding's
// package IDs, digests, and repository IDs. The producer count the floor reads
// is that one batch-wide number, so one finding whose identity is already
// published disarms the floor for every other finding in the same pass —
// including one whose own producer is still inside its activation window.
//
// Closing this needs per-digest readiness, a different contract than #5709
// specifies and not built. Deferring the whole batch instead would be worse: it
// holds back findings that already have their evidence, on a class that freezes
// attempt_count. See docs/internal/evidence/5709-supply-chain-consumer.md.
//
// If the batch-wide count were ever narrowed to a per-finding predicate, this
// test fails: the pass would defer instead of writing.
func TestSupplyChainImpactDoesNotDeferABatchWhereAnotherFindingResolved(t *testing.T) {
	t.Parallel()

	loader := &stubSupplyChainImpactFactLoader{
		scopeFacts: append(
			supplyChainImpactArmedScopeFacts(),
			vulnerabilityCVEFact("cve-2", "CVE-2026-0002", 7.5),
			vulnerabilityAffectedPackageFact(
				"affected-2", "CVE-2026-0002", "pkg:npm/waiting", "npm", "waiting", "2.0.0", "2.1.0",
			),
			packageConsumptionFactWithRange("consume-2", "pkg:npm/waiting", "repo://example/worker", "2.0.0"),
		),
		// Identity published for the first finding's repository only. The
		// second finding's producer is still inside its activation window.
		active: []facts.Envelope{
			containerImageIdentityImpactFact("image-identity-resolved", testImpactSubjectDigest, testImpactRepositoryID),
		},
	}
	writer := &recordingSupplyChainImpactWriter{}
	handler := SupplyChainImpactHandler{
		FactLoader:        loader,
		Writer:            writer,
		ProducerReadiness: readinessWithOnlyUnready(DomainContainerImageIdentity),
	}

	if _, err := handler.Handle(
		context.Background(),
		supplyChainImpactIntentUnderTheFloor("one finding resolved, one still waiting on its producer"),
	); err != nil {
		t.Fatalf(
			"Handle() error = %v, want nil: the floor is armed per batch, and this batch resolved a producer fact",
			err,
		)
	}
	if writer.calls != 1 {
		t.Fatalf("WriteSupplyChainImpactFindings() calls = %d, want 1", writer.calls)
	}
	// The waiting finding committed an answer computed without its producer's
	// evidence. That is the #5709 defect, disclosed rather than closed.
	waiting := supplyChainImpactFindingsByCVE(writer.write.Findings)["CVE-2026-0002"]
	if waiting.SubjectDigest != "" {
		t.Fatalf(
			"waiting finding SubjectDigest = %q, want empty: this test exists because that evidence was missing",
			waiting.SubjectDigest,
		)
	}
}

// osPackageStagedSupplyChainImpactFactLoader partitions facts by scope the way
// the production store does, and answers the shared active-evidence reader
// differently per stage: the first round returns an os_package, whose own scan
// scope the scanner-analysis stage then visits, and the resolved-digest re-run
// — seeded with the digest that stage just discovered — returns the producer's
// container image identity.
//
// The scope-blind stubSupplyChainImpactFactLoader cannot express that: it
// returns the same slice for every scope, so no stage after the until-stable
// loop ever has different evidence to find. That is the same blind spot that let
// issue #5463's missing load stage go undetected.
type osPackageStagedSupplyChainImpactFactLoader struct {
	factsByScope   map[string][]facts.Envelope
	firstRound     []facts.Envelope
	digestSeeded   []facts.Envelope
	seedDigest     string
	activeCalls    int
	digestRoundHit bool
}

func (l *osPackageStagedSupplyChainImpactFactLoader) ListFacts(
	_ context.Context,
	scopeID string,
	generationID string,
) ([]facts.Envelope, error) {
	return append([]facts.Envelope(nil), l.factsByScope[scanScopedFactLoaderKey(scopeID, generationID)]...), nil
}

func (l *osPackageStagedSupplyChainImpactFactLoader) ListFactsByKind(
	_ context.Context,
	scopeID string,
	generationID string,
	kinds []string,
) ([]facts.Envelope, error) {
	var matched []facts.Envelope
	for _, envelope := range l.factsByScope[scanScopedFactLoaderKey(scopeID, generationID)] {
		if slices.Contains(kinds, envelope.FactKind) {
			matched = append(matched, envelope)
		}
	}
	return matched, nil
}

func (l *osPackageStagedSupplyChainImpactFactLoader) ListActiveSupplyChainImpactFacts(
	_ context.Context,
	filter SupplyChainImpactFactFilter,
) ([]facts.Envelope, bool, error) {
	l.activeCalls++
	// Only the resolved-digest stage carries the digest the scanner-analysis
	// stage discovered, so this branch cannot be reached by the until-stable
	// loop or by the peer-identity pass, which clears SubjectDigests.
	if slices.Contains(filter.SubjectDigests, l.seedDigest) {
		l.digestRoundHit = true
		return append([]facts.Envelope(nil), l.digestSeeded...), false, nil
	}
	if l.activeCalls == 1 {
		return append([]facts.Envelope(nil), l.firstRound...), false, nil
	}
	return nil, false, nil
}

// TestSupplyChainImpactCountsProducerFactsFromTheResolvedDigestStage is the
// guard for the load stage the producer count most has to reach.
//
// An OS-package finding's scanned digest is not known when the first
// active-evidence load runs. It only appears after the scanner-analysis stage,
// which is why the resolved-digest stage (issue #5464) re-runs the
// active-evidence reader seeded with it — and that re-run is where a pure
// OS-package finding's reducer_container_image_identity arrives. A producer
// count taken when the until-stable loop settles is zero here, so the pass would
// defer for the full 30-minute bound with its producer's answer already in hand.
func TestSupplyChainImpactCountsProducerFactsFromTheResolvedDigestStage(t *testing.T) {
	t.Parallel()

	const (
		intentScopeID      = "vuln-intel:debian:openssl"
		intentGenerationID = "generation-intel-5709"
		scanScopeID        = "scan-target-debian-app-5709"
		scanGenerationID   = "generation-scan-5709"
		imageRef           = "registry.example/debian-app@" + testScannerAnalysisImageDigest
	)

	osPackage := facts.Envelope{
		FactID:       "dpkg-os-openssl-5709",
		FactKind:     facts.VulnerabilityOSPackageFactKind,
		ScopeID:      scanScopeID,
		GenerationID: scanGenerationID,
		Payload: map[string]any{
			"distro":                 "debian",
			"distro_version":         "12",
			"package_manager":        "dpkg",
			"name":                   "openssl",
			"arch":                   "amd64",
			"repository_class":       "vendor",
			"vendor_advisory_source": "debian",
			"installed_version_raw":  "3.0.11-1~deb12u2",
			"purl":                   "pkg:deb/debian/openssl@3.0.11-1~deb12u2?arch=debian-12",
		},
	}
	loader := &osPackageStagedSupplyChainImpactFactLoader{
		seedDigest: testScannerAnalysisImageDigest,
		factsByScope: map[string][]facts.Envelope{
			scanScopedFactLoaderKey(intentScopeID, intentGenerationID): {
				vulnerabilityCVEFact("cve-1", "CVE-2026-0001", 9.8),
				vulnerabilityAffectedPackageFact(
					"affected-1", "CVE-2026-0001", testImpactPackageID, "npm", "example", "1.2.3", "1.3.0",
				),
				// Arms the floor: a repository ID is one of the three filter
				// dimensions the active-evidence query matches a producer on.
				packageConsumptionFactWithRange("consume-1", testImpactPackageID, testImpactRepositoryID, "1.2.3"),
			},
			scanScopedFactLoaderKey(scanScopeID, scanGenerationID): {
				scannerWorkerAnalysisFact(scanScopeID, scanGenerationID, testScannerAnalysisImageDigest, imageRef),
			},
		},
		firstRound: []facts.Envelope{osPackage},
		digestSeeded: []facts.Envelope{
			containerImageIdentityImpactFact(
				"image-identity-resolved-digest", testScannerAnalysisImageDigest, testImpactRepositoryID,
			),
		},
	}
	writer := &recordingSupplyChainImpactWriter{}
	handler := SupplyChainImpactHandler{
		FactLoader:        loader,
		Writer:            writer,
		ProducerReadiness: readinessWithOnlyUnready(DomainContainerImageIdentity),
	}

	if _, err := handler.Handle(context.Background(), Intent{
		IntentID:       "intent-supply-chain-resolved-digest",
		ScopeID:        intentScopeID,
		GenerationID:   intentGenerationID,
		SourceSystem:   "vulnerability_intelligence",
		Domain:         DomainSupplyChainImpact,
		Cause:          "producer identity arrives on the resolved-digest re-run",
		CycleStartedAt: time.Now().Add(-time.Minute),
	}); err != nil {
		t.Fatalf(
			"Handle() error = %v, want nil: the producer count must be taken after the resolved-digest stage, not when the until-stable loop settles",
			err,
		)
	}
	if !loader.digestRoundHit {
		t.Fatal("the resolved-digest stage never ran: the fixture did not exercise the stage this test exists for")
	}
	if writer.calls != 1 {
		t.Fatalf("WriteSupplyChainImpactFindings() calls = %d, want 1", writer.calls)
	}
}

// TestSupplyChainImpactIgnoresProducerFactsAlreadyInItsOwnScope pins why the
// producer count is a delta over the cross-scope stages rather than a count of
// the whole envelope set.
//
// supplyChainImpactFactKinds asks the intent's OWN scope for
// reducer_container_image_identity and reducer_ci_cd_run_correlation as well.
// An absolute count would let a producer fact that happened to live in the
// consumer's own vulnerability scope stand in for one the cross-scope read
// actually resolved, disarming the floor on evidence that says nothing about
// whether the producer's OTHER scopes have activated.
func TestSupplyChainImpactIgnoresProducerFactsAlreadyInItsOwnScope(t *testing.T) {
	t.Parallel()

	loader := &stubSupplyChainImpactFactLoader{
		scopeFacts: append(
			supplyChainImpactArmedScopeFacts(),
			containerImageIdentityImpactFact("image-identity-own-scope", testImpactSubjectDigest, testImpactRepositoryID),
		),
		// The cross-scope read still resolves no producer output.
		active: nil,
	}
	writer := &recordingSupplyChainImpactWriter{}
	handler := SupplyChainImpactHandler{
		FactLoader:        loader,
		Writer:            writer,
		ProducerReadiness: &fixedCrossScopeReadiness{ready: false},
	}

	_, err := handler.Handle(
		context.Background(),
		supplyChainImpactIntentUnderTheFloor("a producer fact already sits in the consumer's own scope"),
	)
	var notReady crossScopeProducerNotReadyError
	if !errors.As(err, &notReady) {
		t.Fatalf(
			"Handle() error = %#v, want a deferral: only producer facts the CROSS-SCOPE read resolved may disarm the floor",
			err,
		)
	}
	if writer.calls != 0 {
		t.Fatalf("WriteSupplyChainImpactFindings() calls = %d, want 0 on a deferral", writer.calls)
	}
}

// TestSupplyChainImpactProducerFactKindsCoverEveryDeclaredProducer is the
// catalog-drift guard.
//
// The producer fact kinds this consumer counts are hand-mapped from the producer
// DOMAINS the cross-scope catalog declares. Adding a third producer to
// crossScopeDependencyCatalog without adding its fact kind here would leave that
// producer's output uncounted, so a pass that resolved only that producer would
// defer to the full 30-minute bound with its answer already in hand.
func TestSupplyChainImpactProducerFactKindsCoverEveryDeclaredProducer(t *testing.T) {
	t.Parallel()

	dependencies := crossScopeDependenciesForRegistration(DomainSupplyChainImpact)
	if len(dependencies) != 1 {
		t.Fatalf("crossScopeDependenciesForRegistration(%s) returned %d entries, want 1", DomainSupplyChainImpact, len(dependencies))
	}
	for _, producer := range dependencies[0].ProducerDomains {
		kind, ok := supplyChainImpactProducerFactKindByDomain[producer]
		if !ok {
			t.Fatalf(
				"producer domain %s has no fact kind in supplyChainImpactProducerFactKindByDomain: its output would never disarm the floor",
				producer,
			)
		}
		if strings.TrimSpace(kind) == "" {
			t.Fatalf("producer domain %s maps to a blank fact kind", producer)
		}
	}
}

// TestSupplyChainImpactProducerLookupPlannedTracksTheFilterDimensions pins the
// three filter dimensions listActiveSupplyChainImpactFactsQuery can match a
// producer row on, against a table read out of that query rather than guessed.
func TestSupplyChainImpactProducerLookupPlannedTracksTheFilterDimensions(t *testing.T) {
	t.Parallel()

	handler := SupplyChainImpactHandler{FactLoader: &stubSupplyChainImpactFactLoader{}}
	cases := []struct {
		name   string
		filter SupplyChainImpactFactFilter
		want   bool
	}{
		{
			name:   "an empty filter asks nothing",
			filter: SupplyChainImpactFactFilter{},
			want:   false,
		},
		{
			name:   "package identity alone cannot match a producer row",
			filter: SupplyChainImpactFactFilter{PackageIDs: []string{testImpactPackageID}, CVEIDs: []string{"CVE-2026-0001"}},
			want:   false,
		},
		{
			name:   "a subject digest matches digest and artifact_digest",
			filter: SupplyChainImpactFactFilter{SubjectDigests: []string{testImpactSubjectDigest}},
			want:   true,
		},
		{
			name:   "an image ref matches image_ref on both producer kinds",
			filter: SupplyChainImpactFactFilter{ImageRefs: []string{"registry.example.com/team/api:v1"}},
			want:   true,
		},
		{
			name:   "a repository ID matches the repository branch, which lists both producer kinds",
			filter: SupplyChainImpactFactFilter{RepositoryIDs: []string{testImpactRepositoryID}},
			want:   true,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			if got := handler.crossScopeProducerLookupPlanned(testCase.filter); got != testCase.want {
				t.Fatalf("crossScopeProducerLookupPlanned() = %t, want %t", got, testCase.want)
			}
		})
	}
}

// TestSupplyChainImpactCountsProducerFactsFromALaterEvidenceRound proves the
// producer count is taken after the whole active-evidence pipeline, not after
// its first round.
//
// This consumer's cross-scope read is not one call. The until-stable loop runs
// up to 8 rounds, each seeded by what the previous round returned, and two more
// stages follow it: the resolved-digest re-run (#5464), which is where a pure
// OS-package finding's reducer_container_image_identity arrives, and the
// peer-identity pass (#5468). A count taken at the first round would defer a
// pass whose producer had already answered on the second.
//
// The fixture drives the loop itself: round one returns an SBOM attachment
// carrying a digest the first filter did not have, which is what makes a second
// round run, and that round returns the identity.
func TestSupplyChainImpactCountsProducerFactsFromALaterEvidenceRound(t *testing.T) {
	t.Parallel()

	identity := containerImageIdentityImpactFact("image-identity-late", testImpactSubjectDigest, testImpactRepositoryID)
	call := 0
	loader := &stubSupplyChainImpactFactLoader{
		scopeFacts: supplyChainImpactArmedScopeFacts(),
		activeForFilter: func(SupplyChainImpactFactFilter) []facts.Envelope {
			call++
			// Round one resolves a non-producer fact whose subject digest
			// widens the filter, so a second round runs.
			if call == 1 {
				return []facts.Envelope{
					sbomAttachmentImpactFact("attachment-late", "doc-late", testImpactSubjectDigest),
				}
			}
			return []facts.Envelope{identity}
		},
	}
	writer := &recordingSupplyChainImpactWriter{}
	handler := SupplyChainImpactHandler{
		FactLoader:        loader,
		Writer:            writer,
		ProducerReadiness: readinessWithOnlyUnready(DomainContainerImageIdentity),
	}

	if _, err := handler.Handle(
		context.Background(),
		supplyChainImpactIntentUnderTheFloor("producer identity arrives on a later active-evidence load"),
	); err != nil {
		t.Fatalf("Handle() error = %v, want nil: a producer fact loaded by a later stage still counts", err)
	}
	if call < 2 {
		t.Fatalf("active-evidence loads = %d, want at least 2: the fixture did not exercise a later stage", call)
	}
	if writer.calls != 1 {
		t.Fatalf("WriteSupplyChainImpactFindings() calls = %d, want 1", writer.calls)
	}
}
