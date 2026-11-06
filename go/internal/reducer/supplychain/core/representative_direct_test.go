// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// repDirectOtherID is the fact id of the fact that cannot join the host's row in
// every scenario below, so a control can be built by dropping it by id.
const repDirectOtherID = "affected-other"

// repDirectScenario is one blank-ecosystem group whose own fact joins the host's
// installed row directly (#9990, arbiter ruling 9990-orderdep). The other fact is
// from another source and has no purl, so it cannot join and anchors nothing.
type repDirectScenario struct {
	own, other  facts.Envelope
	otherSource string
	host        installedOSRow
	// extra envelopes follow the permuted ones on the fleet path, for example a
	// consuming repository.
	extra []facts.Envelope
}

// repDirectRun is the findings one fact order produced. outcomes is the
// representative outcome count of the fleet path and is nil on the authorized path.
type repDirectRun struct {
	order    string
	findings []SupplyChainImpactFinding
	outcomes map[osRepresentativeOutcomeKey]int
}

// joinedDirectly reports whether the source-order representative joined the row.
func (r repDirectRun) joinedDirectly() bool {
	return r.outcomes[osRepresentativeOutcomeKey{vendor: "ubuntu", outcome: osRepresentativeJoined}] == 1
}

// withoutOther drops the fact that cannot join, which builds the control.
func repDirectWithoutOther(envelopes []facts.Envelope) []facts.Envelope {
	return slices.DeleteFunc(slices.Clone(envelopes), func(e facts.Envelope) bool { return e.FactID == repDirectOtherID })
}

func repDirectOrderLabel(envelopes []facts.Envelope) string {
	ids := make([]string, 0, len(envelopes))
	for _, e := range envelopes {
		ids = append(ids, e.FactID)
	}
	return strings.Join(ids, ",")
}

// fleet builds the findings for every order of cve, own, other and the host row,
// with or without the other fact.
func (sc repDirectScenario) fleet(t *testing.T, withOther bool) []repDirectRun {
	t.Helper()
	cve := vulnerabilityCVEFact("cve-9990", vendorLineCVE, 7.5)
	base := []facts.Envelope{cve, sc.own, sc.other, sc.host.envelope()}
	var runs []repDirectRun
	for _, order := range repJoinPermutations(base) {
		if !withOther {
			order = repDirectWithoutOther(order)
		}
		input := append(slices.Clone(order), sc.extra...)
		got, _, stats, err := buildSupplyChainImpactFindingsAndExclusions(input)
		if err != nil {
			t.Fatalf("build findings: %v", err)
		}
		runs = append(runs, repDirectRun{repDirectOrderLabel(order), got, stats.osRepresentativeOutcomes})
	}
	return runs
}

// authorized builds the findings for every order of the three advisory facts on
// the per-target path, with or without the other fact.
func (sc repDirectScenario) authorized(t *testing.T, withOther bool) []repDirectRun {
	t.Helper()
	cve := vulnerabilityCVEFact("cve-9990", vendorLineCVE, 7.5)
	cve.Payload["source"], cve.Payload["advisory_id"] = "ubuntu", "USN-9990-1"
	authority := []AdvisoryAuthorityDependency{repJoinAuthority("ubuntu"), repJoinAuthority(sc.otherSource)}
	scoped := []facts.Envelope{repJoinAdvisory(cve, "ubuntu"), repJoinAdvisory(sc.own, "ubuntu"), repJoinAdvisory(sc.other, sc.otherSource)}
	var runs []repDirectRun
	for _, order := range repJoinPermutations(scoped) {
		if !withOther {
			order = repDirectWithoutOther(order)
		}
		target := authorizedOSVTarget(0, order, authority, sc.host)
		got, err := BuildAuthorizedScanImpactFindings(nil, []AuthorizedScanImpactEvidence{target})
		if err != nil {
			t.Fatalf("BuildAuthorizedScanImpactFindings: %v", err)
		}
		runs = append(runs, repDirectRun{repDirectOrderLabel(order), got, nil})
	}
	return runs
}

// repDirectOtherSpellings and repDirectSources are the ruling's grid for tests
// 4.1 and 4.2.
var (
	repDirectOtherSpellings = []string{"os", "deb", "ubuntu", "dpkg"}
	repDirectSources        = []string{"nvd", "osv", "debian"}
)

// repDirectVendorScenario is a blank vendor fact whose fix is above the host, beside
// a fact that cannot join and whose fix is below it.
func repDirectVendorScenario(source, ecosystem string) repDirectScenario {
	return repDirectScenario{
		own:         repJoinVendorFixFact(""),
		other:       repJoinOtherFact(repDirectOtherID, source, ecosystem),
		otherSource: source,
		host:        ubuntuCurl("22.04", repJoinVersion),
	}
}

// TestDirectlyJoinedBlankGroupIsDecidedByTheFactsThatJoin is test 4.1 of ruling
// 9990-orderdep. A blank-ecosystem fact that joins the installed row directly is
// decided by the facts that join, as a reselected group is: the vendor's fix is
// above the host and another source's is below, and that source must not make the
// host read known-fixed, in any fact order, on either path.
func TestDirectlyJoinedBlankGroupIsDecidedByTheFactsThatJoin(t *testing.T) {
	t.Parallel()
	for _, ecosystem := range repDirectOtherSpellings {
		for _, source := range repDirectSources {
			t.Run(fmt.Sprintf("other=%q/%q", ecosystem, source), func(t *testing.T) {
				t.Parallel()
				sc := repDirectVendorScenario(source, ecosystem)
				t.Run("fleet", func(t *testing.T) {
					for _, run := range sc.fleet(t, true) {
						t.Logf("order %s", run.order)
						assertRepJoinVendorVerdict(t, run.findings)
					}
				})
				t.Run("authorized", func(t *testing.T) {
					for _, run := range sc.authorized(t, true) {
						t.Logf("order %s", run.order)
						assertRepJoinVendorVerdict(t, run.findings)
					}
				})
			})
		}
	}
}

// repDirectSummary names what a failure message needs from a set of findings.
func repDirectSummary(findings []SupplyChainImpactFinding) string {
	var parts []string
	for _, f := range findings {
		parts = append(parts, fmt.Sprintf("%s/%s adv=%s fix=%q evidence=%v", f.Status, f.MatchReason, f.AdvisoryID, f.FixedVersion, f.EvidenceFactIDs))
	}
	return "[" + strings.Join(parts, "; ") + "]"
}

// assertRepDirectEqualsControl holds every order to the finding the same envelopes
// give without the fact that cannot join (test 4.2): a fact with no anchor must not
// change a finding, down to its advisory id, evidence and fix.
func assertRepDirectEqualsControl(t *testing.T, runs, controls []repDirectRun) {
	t.Helper()
	want := controls[0].findings
	for _, control := range controls {
		if !reflect.DeepEqual(control.findings, want) {
			t.Fatalf("the control depends on fact order:\n%s\n%s", repDirectSummary(control.findings), repDirectSummary(want))
		}
	}
	if len(want) != 1 {
		t.Fatalf("control findings = %d, want 1: %#v", len(want), want)
	}
	for _, run := range runs {
		if !reflect.DeepEqual(run.findings, want) {
			t.Fatalf("order %s differs from the control without the other fact:\n got %s\nwant %s", run.order, repDirectSummary(run.findings), repDirectSummary(want))
		}
	}
}

// TestFactThatCannotJoinChangesNothingOnADirectBlankRow is test 4.2: deep equality
// with the control built without the other fact, on both paths.
func TestFactThatCannotJoinChangesNothingOnADirectBlankRow(t *testing.T) {
	t.Parallel()
	scenarios := map[string]repDirectScenario{}
	for _, ecosystem := range repDirectOtherSpellings {
		for _, source := range repDirectSources {
			scenarios[fmt.Sprintf("fix above the host/other=%q/%q", ecosystem, source)] = repDirectVendorScenario(source, ecosystem)
		}
	}
	scenarios["affected list/other=os/nvd"] = repDirectScenario{
		own:         repJoinOwnFact(""),
		other:       repJoinOtherFact(repDirectOtherID, "nvd", "os"),
		otherSource: "nvd",
		host:        ubuntuCurl("22.04", repJoinVersion),
	}
	for _, name := range slices.Sorted(maps.Keys(scenarios)) {
		sc := scenarios[name]
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			t.Run("fleet", func(t *testing.T) {
				assertRepDirectEqualsControl(t, sc.fleet(t, true), sc.fleet(t, false))
			})
			t.Run("authorized", func(t *testing.T) {
				assertRepDirectEqualsControl(t, sc.authorized(t, true), sc.authorized(t, false))
			})
		})
	}
}

// repDirectKnownFixedScenario is a blank vendor fact whose fix is below the host and
// that lists no affected versions, beside an nvd fact under os that lists the host.
func repDirectKnownFixedScenario() repDirectScenario {
	own := repJoinVendorFixFact("")
	own.Payload["fixed_versions"] = []any{"1.0-2"}
	other := repJoinOtherFact(repDirectOtherID, "nvd", "os")
	other.Payload["affected_versions"] = []any{repJoinVersion}
	return repDirectScenario{own: own, other: other, otherSource: "nvd", host: ubuntuCurl("22.04", repJoinVersion)}
}

// TestKnownFixedVerdictDoesNotDependOnFactOrder is test 4.3. The vendor says the host
// is past its fix, and a fact that cannot join lists the host as affected. Every
// order reads the vendor's known-fixed verdict, not os_release_unverified.
func TestKnownFixedVerdictDoesNotDependOnFactOrder(t *testing.T) {
	t.Parallel()
	sc := repDirectKnownFixedScenario()
	check := func(t *testing.T, runs []repDirectRun) {
		t.Helper()
		for _, run := range runs {
			if len(run.findings) != 1 {
				t.Fatalf("order %s: findings = %d, want 1: %#v", run.order, len(run.findings), run.findings)
			}
			got := run.findings[0]
			if got.Status != SupplyChainImpactNotAffectedKnownFixed || got.MatchReason != supplyChainVersionReasonDPKGKnownFixed {
				t.Fatalf("order %s: verdict = %s/%s, want %s/%s", run.order, got.Status, got.MatchReason,
					SupplyChainImpactNotAffectedKnownFixed, supplyChainVersionReasonDPKGKnownFixed)
			}
		}
	}
	t.Run("fleet", func(t *testing.T) { check(t, sc.fleet(t, true)) })
	t.Run("authorized", func(t *testing.T) { check(t, sc.authorized(t, true)) })
}

// repDirectBorrowedScenario is the vendor's fact spelled debian that carries the
// Ubuntu purl, beside a blank Ubuntu fact with no purl that does join the host.
func repDirectBorrowedScenario() repDirectScenario {
	debianSpelled := repJoinOwnFact("debian")
	debianSpelled.FactID = repDirectOtherID
	blank := repJoinOtherFact("affected-blank", "ubuntu", "")
	blank.Payload["affected_versions"] = []any{repJoinVersion}
	delete(blank.Payload, "fixed_versions")
	return repDirectScenario{own: blank, other: debianSpelled, otherSource: "ubuntu", host: ubuntuCurl("22.04", repJoinVersion)}
}

// TestBlankFactDoesNotBorrowItsMatcherFromAFactThatCannotJoin is test 4.4. The
// blank joining fact has no purl of its own, and the only OS purl in the group
// belongs to a fact that cannot join. The group is decided by the joining fact
// alone, so the version family is unknown and the verdict is
// unsupported_ecosystem, equal to the control without the other fact. Reading the
// family from that purl is #10034 item 2.
func TestBlankFactDoesNotBorrowItsMatcherFromAFactThatCannotJoin(t *testing.T) {
	t.Parallel()
	sc := repDirectBorrowedScenario()
	check := func(t *testing.T, runs, controls []repDirectRun) {
		t.Helper()
		assertRepDirectEqualsControl(t, runs, controls)
		for _, run := range runs {
			got := run.findings[0]
			if got.Status != SupplyChainImpactPossiblyAffected || got.MatchReason != supplyChainVersionReasonUnsupportedEcosystem {
				t.Fatalf("order %s: verdict = %s/%s, want %s/%s", run.order, got.Status, got.MatchReason,
					SupplyChainImpactPossiblyAffected, supplyChainVersionReasonUnsupportedEcosystem)
			}
			if slices.Contains(got.EvidenceFactIDs, repDirectOtherID) {
				t.Fatalf("order %s: evidence %v holds the fact that cannot join", run.order, got.EvidenceFactIDs)
			}
		}
	}
	t.Run("fleet", func(t *testing.T) { check(t, sc.fleet(t, true), sc.fleet(t, false)) })
	t.Run("authorized", func(t *testing.T) { check(t, sc.authorized(t, true), sc.authorized(t, false)) })
}

// assertRepDirectKeepsOtherFact wants the other fact on the finding's evidence in
// every order where the source-order representative joined the row, and the given
// verdict when one is named. directOnly limits the check to those orders, because
// the other order reselects and so drops a fact that cannot join, on the base too.
func assertRepDirectKeepsOtherFact(t *testing.T, runs []repDirectRun, directOnly bool, status SupplyChainImpactStatus, reason string) {
	t.Helper()
	checked := 0
	defer func() {
		if checked == 0 {
			t.Fatalf("no order joined directly, so the guard checked nothing")
		}
	}()
	for _, run := range runs {
		if directOnly && !run.joinedDirectly() {
			continue
		}
		checked++
		if len(run.findings) != 1 {
			t.Fatalf("order %s: findings = %d, want 1: %s", run.order, len(run.findings), repDirectSummary(run.findings))
		}
		got := run.findings[0]
		if !slices.Contains(got.EvidenceFactIDs, repDirectOtherID) {
			t.Fatalf("order %s: evidence %v lost the other fact", run.order, got.EvidenceFactIDs)
		}
		if status != "" && (got.Status != status || got.MatchReason != reason) {
			t.Fatalf("order %s: verdict = %s/%s, want %s/%s", run.order, got.Status, got.MatchReason, status, reason)
		}
	}
}

// TestRowWithNoVersionKeepsTheWholeGroup is guard 4.5a. A blank representative that
// joins a row with no installed version already has a row on the base, so criterion 2
// keeps its finding on the whole group (condition c of the ruling).
func TestRowWithNoVersionKeepsTheWholeGroup(t *testing.T) {
	t.Parallel()
	sc := repDirectScenario{
		own:         repJoinOwnFact(""),
		other:       repJoinOtherFact(repDirectOtherID, "nvd", "os"),
		otherSource: "nvd",
		host:        ubuntuCurl("22.04", ""),
	}
	assertRepDirectKeepsOtherFact(t, sc.fleet(t, true), true, SupplyChainImpactPossiblyAffected, supplyChainVersionReasonMissingInstalled)
}

// TestSpelledEcosystemThatJoinsKeepsTheWholeGroup is guard 4.5b. A spelled ecosystem
// that joins has a row on the base, so a blank fact that cannot join stays on its
// evidence (condition b of the ruling).
func TestSpelledEcosystemThatJoinsKeepsTheWholeGroup(t *testing.T) {
	t.Parallel()
	sc := repDirectScenario{
		own:         repJoinOwnFact("os"),
		other:       repJoinOtherFact(repDirectOtherID, "nvd", ""),
		otherSource: "nvd",
		host:        ubuntuCurl("22.04", repJoinVersion),
	}
	assertRepDirectKeepsOtherFact(t, sc.fleet(t, true), true, "", "")
}

// TestSpelledDpkgThatJoinsKeepsTheWholeGroup is the review's F7 test. dpkg is a spelled
// ecosystem with a row on the base, though it normalizes to no ecosystem name. A
// condition b that reads the normalized name instead of the trimmed spelling narrows it
// and moves that row, so this fails under that change.
func TestSpelledDpkgThatJoinsKeepsTheWholeGroup(t *testing.T) {
	t.Parallel()
	sc := repDirectScenario{
		own:         repJoinOwnFact("dpkg"),
		other:       repJoinOtherFact(repDirectOtherID, "nvd", "os"),
		otherSource: "nvd",
		host:        ubuntuCurl("22.04", repJoinVersion),
	}
	assertRepDirectKeepsOtherFact(t, sc.fleet(t, true), true, "", "")
}

// TestBlankGroupWithAConsumerKeepsTheWholeGroup is guard 4.5c. A consuming
// repository anchors the group, so the finding is unchanged from the base and a
// blank-ecosystem group with a consumer stays #10034 item 5.
func TestBlankGroupWithAConsumerKeepsTheWholeGroup(t *testing.T) {
	t.Parallel()
	sc := repDirectScenario{
		own:         repJoinOwnFact(""),
		other:       repJoinOtherFact(repDirectOtherID, "nvd", "os"),
		otherSource: "nvd",
		host:        ubuntuCurl("22.04", repJoinVersion),
		extra:       []facts.Envelope{packageConsumptionFactWithRange("consume-9990", repJoinPackageID, "repository:r_9990", repJoinVersion)},
	}
	assertRepDirectKeepsOtherFact(t, sc.fleet(t, true), false, "", "")
}

// TestDirectlyJoinedBlankGroupCountsAsJoined is the outcome row of test 4.6.
// The representative either joined the row or did not, and the count says
// which, even when the direct group is narrowed to the facts that join. Each
// order counts one group. Before #10034 item 1 the source-order representative
// differed per order, so one order counted joined and the other reselected;
// now the vendor fact names the group in every order, the narrow-direct group
// counts joined everywhere, and the reselected label for this shape is gone.
func TestDirectlyJoinedBlankGroupCountsAsJoined(t *testing.T) {
	t.Parallel()
	sc := repDirectVendorScenario("nvd", "os")
	joined := map[osRepresentativeOutcomeKey]int{{vendor: "ubuntu", outcome: osRepresentativeJoined}: 1}
	for _, run := range sc.fleet(t, true) {
		if !maps.Equal(run.outcomes, joined) {
			t.Fatalf("order %s: outcomes = %v, want %v", run.order, run.outcomes, joined)
		}
	}
}
