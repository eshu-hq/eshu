// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package evidencebundle

import (
	"regexp"
	"strings"
	"testing"
)

// relaxedULAv6PatternForProof is the alternative this package REJECTED: the
// unique-local hextet rule carrying the same relaxed left boundary the
// hostname and IPv4 rules now use. It exists only so
// TestRelaxingTheULABoundaryWouldRejectPublicIPv6 can demonstrate the
// false positive rather than assert it on authority.
var relaxedULAv6PatternForProof = regexp.MustCompile(`(?i)(^|[^0-9A-Za-z.-])f[cd][0-9a-f]{2}:[0-9a-f:]*[0-9a-f]`)

// The private-host rules require the character before the host to be outside
// a set of "this is still part of the previous token" characters. That set
// used to include the colon, and a colon is precisely what a labelled
// diagnostic ("upstream:db.internal:5432 refused") or a scope handle
// ("repo:db.internal") puts there. Every pre-existing case in
// live_redaction_test.go writes "dial tcp <host>", so the character before the
// host was always a space and the gap never showed up. The fixture shape was
// the blind spot, not the assertion.
//
// Reproduced against a binary built from origin/main at fc462effc before this
// branch was touched:
//
//	eshu evidence bundle export --scope 'repo:db.internal:5432' --out b.json
//	  -> rc=0; "scope_id": "repo:db.internal:5432" appears 4x in a bundle
//	     stamped validation.status "passed" with redaction rule
//	     "screened_private_endpoints"
//	eshu evidence bundle export --scope 'repo:10.0.5.3' --out b2.json
//	  -> rc=0; same, 4x
//
// The pattern fix is adopted from commit 6341dd234 (branch 6059-cli-evbundle).

// TestValidateRejectsPrivateEndpointsWrittenAfterAColon pins the delimiter the
// existing rows never exercised.
func TestValidateRejectsPrivateEndpointsWrittenAfterAColon(t *testing.T) {
	for _, reason := range []string{
		"upstream:db.internal:5432 refused",
		"target:127.0.0.1:5432 refused",
		"peer:localhost:8080 refused",
		"backend:10.0.5.3:5432 refused",
		"proxy:192.168.1.9:8080 refused",
		// Portless, so this exercises privateAddressPattern rather than the
		// host:port rule.
		"peer:10.0.5.3 unreachable",
		"svc:eshu-api.eshu.svc.cluster.local unreachable",
		"host:ip-10-0-1-5.ec2.internal:5432 refused",
		// IPv4-mapped IPv6: the colon before the quad is the same boundary.
		"peer ::ffff:10.0.5.3 unreachable",
	} {
		t.Run(reason, func(t *testing.T) {
			snapshot := realisticLiveSnapshot()
			snapshot.HealthReasons = []string{reason}
			bundle := BuildLiveBundle(snapshot, LiveBundleOptions{
				ScopeID:   "live:local",
				CreatedAt: fixedLiveCreatedAt,
			})
			if err := Validate(bundle); err == nil {
				t.Fatalf("Validate accepted a bundle carrying %q", reason)
			}
		})
	}
}

// TestValidateRejectsAPrivateHostUsedAsAScopeHandle covers the same gap on the
// one caller-supplied string the demo path stamps into the artifact. A scope
// handle is prefixed "repo:", which put the colon right before the host.
func TestValidateRejectsAPrivateHostUsedAsAScopeHandle(t *testing.T) {
	for _, scope := range []string{
		"repo:db.internal:5432",
		"repo:10.0.5.3",
		"repo:eshu-api.eshu.svc.cluster.local",
		"repo:localhost:8080",
	} {
		t.Run(scope, func(t *testing.T) {
			bundle := BuildDemoBundle(DemoBundleOptions{ScopeID: scope})
			if err := Validate(bundle); err == nil {
				t.Fatalf("Validate accepted a bundle scoped %q", scope)
			}
		})
	}
}

// TestValidateKeepsColonDelimitedHonestTextUsable is the other half of the
// widening. Colon-delimited labels are ordinary in this artifact -- handles,
// reproduce targets, domain counts -- and rejecting them would make the
// bundle unusable for honest content.
func TestValidateKeepsColonDelimitedHonestTextUsable(t *testing.T) {
	snapshot := realisticLiveSnapshot()
	snapshot.HealthReasons = []string{
		"domain:secrets_iam_trust_chain blocked:12",
		"stage:parse pending:1024",
		"capability:ask.eshu unavailable",
		"route:GET /api/v0/status/index degraded",
		"backend:nornicdb version:1.1.11 retry:10.4 percent",
		"observed at:12:30:45 UTC",
		"scope:repo:demo/service unchanged",
	}
	bundle := BuildLiveBundle(snapshot, LiveBundleOptions{
		ScopeID:   "live:local",
		CreatedAt: fixedLiveCreatedAt,
	})
	if err := Validate(bundle); err != nil {
		t.Fatalf("Validate rejected honest colon-delimited text: %v", err)
	}
}

// TestValidateKeepsPublicIPv6InFreeTextUsable guards the IPv6 boundary that
// deliberately did NOT change. A unique-local address is private and must be
// rejected wherever it appears, but a public address whose middle hextet
// happens to start "fc"/"fd" is not, and relaxing that rule's left boundary
// the way the hostname rules were relaxed would have started rejecting it:
// the character before "fd12" in 2001:db8:fd12::1 is a colon.
//
// Verified directly rather than taken on trust -- see
// TestRelaxingTheULABoundaryWouldRejectPublicIPv6 below, which compiles the
// relaxed shape and shows it matching.
func TestValidateKeepsPublicIPv6InFreeTextUsable(t *testing.T) {
	for _, reason := range []string{
		"peer 2001:db8:fd12::1 unreachable",
		"peer 2001:db8:fc00::9 unreachable",
		"peer 2606:4700:fd00::1 unreachable",
	} {
		t.Run(reason, func(t *testing.T) {
			snapshot := realisticLiveSnapshot()
			snapshot.HealthReasons = []string{reason}
			bundle := BuildLiveBundle(snapshot, LiveBundleOptions{
				ScopeID:   "live:local",
				CreatedAt: fixedLiveCreatedAt,
			})
			if err := Validate(bundle); err != nil {
				t.Fatalf("Validate rejected public IPv6 text %q: %v", reason, err)
			}
		})
	}
}

// TestValidateStillRejectsUniqueLocalIPv6 is the paired positive control: the
// rule whose boundary was left alone must still fire. It passes before and
// after the widening, which is what says the split moved the alternative
// rather than dropping it.
func TestValidateStillRejectsUniqueLocalIPv6(t *testing.T) {
	for _, reason := range []string{
		"dial tcp fd00::1 refused",
		"dial tcp fc00::42 refused",
	} {
		t.Run(reason, func(t *testing.T) {
			snapshot := realisticLiveSnapshot()
			snapshot.HealthReasons = []string{reason}
			bundle := BuildLiveBundle(snapshot, LiveBundleOptions{
				ScopeID:   "live:local",
				CreatedAt: fixedLiveCreatedAt,
			})
			if err := Validate(bundle); err == nil {
				t.Fatalf("Validate accepted unique-local IPv6 %q", reason)
			}
		})
	}
}

// TestPrivateHostRuleErrorNamesTheEndpoint keeps the refusal actionable: an
// operator who hits it has to be able to tell which rule fired.
func TestPrivateHostRuleErrorNamesTheEndpoint(t *testing.T) {
	snapshot := realisticLiveSnapshot()
	snapshot.HealthReasons = []string{"upstream:db.internal:5432 refused"}
	bundle := BuildLiveBundle(snapshot, LiveBundleOptions{
		ScopeID:   "live:local",
		CreatedAt: fixedLiveCreatedAt,
	})
	err := Validate(bundle)
	if err == nil {
		t.Fatal("Validate accepted a colon-prefixed private endpoint")
	}
	if !strings.Contains(err.Error(), "private endpoint is not allowed") {
		t.Fatalf("error = %v, want the private-endpoint rule named", err)
	}
}

// TestRelaxingTheULABoundaryWouldRejectPublicIPv6 is the check that the split
// is not cargo-culted. It builds the rejected alternative -- the unique-local
// hextet rule carrying the SAME relaxed boundary the hostname and IPv4 rules
// now use -- and shows it matching a public address on a middle hextet. That
// match is the whole reason privateULAv6Pattern exists as its own pattern.
func TestRelaxingTheULABoundaryWouldRejectPublicIPv6(t *testing.T) {
	const publicAddress = "peer 2001:db8:fd12::1 unreachable"

	if privateULAv6Pattern.MatchString(publicAddress) {
		t.Fatalf("privateULAv6Pattern matched public %q; its boundary must keep excluding the colon", publicAddress)
	}
	if !relaxedULAv6PatternForProof.MatchString(publicAddress) {
		t.Fatalf("the relaxed shape did NOT match %q, so the split's stated reason is wrong", publicAddress)
	}
	// And the relaxed shape is not simply broken: it still matches a real ULA.
	if !relaxedULAv6PatternForProof.MatchString("dial tcp fd00::1 refused") {
		t.Fatal("the relaxed shape failed to match a real unique-local address; the proof above proves nothing")
	}
}

// TestPrivateHostAfterAColonIsNotScreenedForULAv6 pins the residual gap the
// split deliberately leaves. A unique-local address written straight after a
// label colon ("repo:fd00::1") is NOT screened, because the boundary that
// would catch it is the same boundary that protects public IPv6. This is a
// documented limitation, not an oversight: it is stated in
// docs/public/reference/evidence-bundle.md so nobody has to rediscover it,
// and this test fails if the behaviour silently changes in either direction.
func TestPrivateHostAfterAColonIsNotScreenedForULAv6(t *testing.T) {
	snapshot := realisticLiveSnapshot()
	snapshot.HealthReasons = []string{"peer:fd00::1 unreachable"}
	bundle := BuildLiveBundle(snapshot, LiveBundleOptions{
		ScopeID:   "live:local",
		CreatedAt: fixedLiveCreatedAt,
	})
	if err := Validate(bundle); err != nil {
		t.Fatalf("known ULA-after-colon gap now rejects %q (err = %v); "+
			"if that is intended, update the Not-screened list in "+
			"docs/public/reference/evidence-bundle.md and delete this test",
			"peer:fd00::1 unreachable", err)
	}
}
