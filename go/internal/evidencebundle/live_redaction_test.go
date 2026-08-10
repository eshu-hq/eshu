// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package evidencebundle

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/redact"
)

// realisticLiveSnapshot is the shape cmd/eshu's decoders actually populate:
// enum states, reason codes, and counts. Tests below mutate one field of it to
// inject a canary, so the only difference between a passing and a failing case
// is the injected value.
func realisticLiveSnapshot() LiveSnapshot {
	return LiveSnapshot{
		RepositoryCount: 12,
		HealthState:     "degraded",
		HealthReasons:   []string{"queue backlog", "domain aws_relationship blocked"},
		Queue: LiveQueueSnapshot{
			Pending:    4,
			InFlight:   2,
			Retrying:   1,
			Succeeded:  100,
			Failed:     0,
			DeadLetter: 1,
		},
		QueueBlockedCount: 1,
		ScopeActivity:     LiveScopeActivitySnapshot{Active: 12, Changed: 3, Unchanged: 9},
		GenerationHistory: LiveGenerationHistorySnapshot{Active: 1, Pending: 0, Completed: 40, Failed: 0},
		StageSummaries: []LiveStageSummarySnapshot{
			{Stage: "parse", Pending: 2, Retrying: 0, Failed: 0, DeadLetter: 0},
		},
		DomainBacklogs: []LiveDomainBacklogSnapshot{
			{Domain: "aws_relationship", Outstanding: 1, Retrying: 0, Failed: 0, DeadLetter: 1},
		},
		Collectors: []LiveCollectorSnapshot{
			{CollectorKind: "git", StatusCategory: "ready", Health: "healthy"},
			{CollectorKind: "aws", StatusCategory: "failed", Health: "unhealthy"},
		},
		SemanticExtraction: LiveSemanticExtractionSnapshot{
			State:              "unavailable",
			Reason:             "provider_not_configured",
			ProviderConfigured: false,
		},
	}
}

// TestBuildLiveBundleRendersCleanSnapshotWithoutCanaries is the control for the
// injection cases below: a snapshot carrying no sensitive value must validate
// and render, so a failure there means the injection tests are meaningful
// rather than failing for an unrelated reason.
func TestBuildLiveBundleRendersCleanSnapshotWithoutCanaries(t *testing.T) {
	bundle := BuildLiveBundle(realisticLiveSnapshot(), LiveBundleOptions{
		ScopeID:   "live:local",
		CreatedAt: fixedLiveCreatedAt,
	})
	if err := Validate(bundle); err != nil {
		t.Fatalf("Validate(clean snapshot) error = %v", err)
	}
	raw, err := RenderJSON(bundle)
	if err != nil {
		t.Fatalf("RenderJSON() error = %v", err)
	}
	registry := redact.HostedGovernanceRegistry()
	if err := registry.AssertNoForbiddenCanary(redact.SurfaceOnboardingArtifacts, raw); err != nil {
		t.Fatalf("clean snapshot tripped a canary: %v\nbundle: %s", err, raw)
	}
}

// TestBuildLiveBundleNeverLeaksHostedGovernanceCanaries injects every canary
// the shared hosted-governance registry forbids on SurfaceOnboardingArtifacts
// into the free-text status fields a live bundle carries, and requires that
// each one is caught rather than serialized.
//
// The earlier version of this test built a snapshot with no canary in it at
// all and then asserted the rendered bundle contained none. That assertion was
// satisfied by the absence of the input, not by any redaction behavior, so it
// would have passed with every check removed.
func TestBuildLiveBundleNeverLeaksHostedGovernanceCanaries(t *testing.T) {
	registry := redact.HostedGovernanceRegistry()
	forbidden := registry.ForbiddenCanaries(redact.SurfaceOnboardingArtifacts)
	if len(forbidden) == 0 {
		t.Fatal("registry reports no forbidden canaries for SurfaceOnboardingArtifacts; this test would be vacuous")
	}

	// Each carrier is a free-text field that a status response could populate
	// and that therefore reaches the rendered bundle.
	carriers := map[string]func(*LiveSnapshot, string){
		"health_reason":     func(s *LiveSnapshot, v string) { s.HealthReasons = []string{v} },
		"semantic_reason":   func(s *LiveSnapshot, v string) { s.SemanticExtraction.Reason = v },
		"semantic_state":    func(s *LiveSnapshot, v string) { s.SemanticExtraction.State = v },
		"collector_health":  func(s *LiveSnapshot, v string) { s.Collectors[0].Health = v },
		"domain_name":       func(s *LiveSnapshot, v string) { s.DomainBacklogs[0].Domain = v },
		"stage_name":        func(s *LiveSnapshot, v string) { s.StageSummaries[0].Stage = v },
		"collector_kind":    func(s *LiveSnapshot, v string) { s.Collectors[0].CollectorKind = v },
		"health_state_word": func(s *LiveSnapshot, v string) { s.HealthState = v },
	}

	for _, canary := range forbidden {
		for carrierName, inject := range carriers {
			t.Run(string(canary.Class)+"/"+carrierName, func(t *testing.T) {
				snapshot := realisticLiveSnapshot()
				inject(&snapshot, canary.Raw)
				bundle := BuildLiveBundle(snapshot, LiveBundleOptions{
					ScopeID:   "live:local",
					CreatedAt: fixedLiveCreatedAt,
				})

				// Validate is the only guard that runs in production, so it is
				// the only thing worth asserting. An earlier version of this
				// test accepted "Validate refused it OR the registry noticed it
				// in the rendered bytes". Nothing redacts before serialization,
				// so the registry arm was always true and the whole condition
				// could never fail — the test passed with the registry check
				// stripped out of Validate entirely.
				if err := Validate(bundle); err == nil {
					raw, _ := RenderJSON(bundle)
					t.Fatalf("Validate accepted a bundle carrying %s canary %q injected via %s\nbundle: %s",
						canary.Class, canary.Raw, carrierName, raw)
				}
			})
		}
	}
}

// TestValidateRejectsBareHostPortEndpoints covers the shape Go's own network
// errors produce. The scheme-anchored privateEndpointPattern only matches a
// full URL, so "127.0.0.1:5432" in an error string slipped through until
// privateHostPortPattern was added.
func TestValidateRejectsBareHostPortEndpoints(t *testing.T) {
	for _, endpoint := range []string{
		"127.0.0.1:5432",
		"localhost:5432",
		"10.0.5.3:5432",
		"192.168.1.9:8080",
		"172.20.0.4:7687",
		// Matched by two independent rules now: the "internal" substring AND the
		// .cluster.local suffix. Before the suffix rule existed this row matched
		// on the substring alone, so it read as coverage for cluster-local
		// service names while an ordinary one exported clean.
		"internal-llm.svc.cluster.local:8080",
		"eshu-api.eshu.svc.cluster.local:8080",
		// Dot-qualified "internal" hostnames. The first pattern anchored the
		// left boundary so tightly that a preceding dot excluded the match, so
		// the most common private-DNS shapes slipped through untested.
		"db.internal:5432",
		"ip-10-0-1-5.ec2.internal:5432",
		"database.internal.example.com:5432",
		// IPv6 loopback, which the IPv4-only alternatives never covered.
		"[::1]:5432",
	} {
		t.Run(endpoint, func(t *testing.T) {
			snapshot := realisticLiveSnapshot()
			snapshot.HealthReasons = []string{"dial tcp " + endpoint + ": connection refused"}
			bundle := BuildLiveBundle(snapshot, LiveBundleOptions{
				ScopeID:   "live:local",
				CreatedAt: fixedLiveCreatedAt,
			})
			if err := Validate(bundle); err == nil {
				t.Fatalf("Validate accepted a bundle carrying private endpoint %q", endpoint)
			}
		})
	}
}

// TestValidateKeepsOrdinaryDottedTextUsable guards the pattern above from
// firing on version-like or count-like text, which would make the live bundle
// unusable for honest content.
func TestValidateKeepsOrdinaryDottedTextUsable(t *testing.T) {
	snapshot := realisticLiveSnapshot()
	snapshot.HealthReasons = []string{
		"backend nornicdb v1.1.11 reported 10.4 percent retry rate",
		"stage parse pending 1024",
		// The portless address rule fires without needing a port, so it has the
		// widest chance of rejecting honest text. These are the real shapes
		// status_health.go emits.
		"reducer 1.10.0.2 build is current",
		"domain aws_relationship has 12 outstanding items",
		"workflow coordinator recent failed runs=2 in 5m0s (cumulative=3)",
	}
	bundle := BuildLiveBundle(snapshot, LiveBundleOptions{
		ScopeID:   "live:local",
		CreatedAt: fixedLiveCreatedAt,
	})
	if err := Validate(bundle); err != nil {
		t.Fatalf("Validate rejected ordinary dotted text: %v", err)
	}
}

// TestValidateRejectsRealCredentialBearingURLs covers what the registry canary
// check cannot. AssertNoForbiddenCanary searches for the registry's exact
// synthetic strings, so a real credential an operator's status text happens to
// carry passes it untouched. These are non-canary values.
func TestValidateRejectsRealCredentialBearingURLs(t *testing.T) {
	for _, secret := range []string{
		"https://alice:s3cr3t@example.com/x",
		"http://svc-account:hunter2@registry.example.org/v2/",
		"https://token:ghp_aaaaaaaaaaaaaaaaaaaa@github.example/repo.git",
	} {
		t.Run(secret, func(t *testing.T) {
			snapshot := realisticLiveSnapshot()
			// profile_id is operator-supplied free text that reaches the bundle.
			snapshot.SemanticExtraction.ProviderProfiles = []LiveSemanticProviderProfileSnapshot{
				{ProfileID: secret, ProviderKind: "openai", State: "ready"},
			}
			snapshot.SemanticExtraction.ProviderConfigured = true
			bundle := BuildLiveBundle(snapshot, LiveBundleOptions{
				ScopeID:   "live:local",
				CreatedAt: fixedLiveCreatedAt,
			})
			if err := Validate(bundle); err == nil {
				raw, _ := RenderJSON(bundle)
				t.Fatalf("Validate accepted a bundle carrying credential URL %q\nbundle: %s", secret, raw)
			}
		})
	}
}

// TestValidateKeepsCredentialFreeURLsUsable guards the check above from
// rejecting an ordinary URL, which would make honest status text unexportable.
func TestValidateKeepsCredentialFreeURLsUsable(t *testing.T) {
	snapshot := realisticLiveSnapshot()
	snapshot.HealthReasons = []string{
		"see https://docs.example.com/runbook/queue-backlog for remediation",
	}
	bundle := BuildLiveBundle(snapshot, LiveBundleOptions{ScopeID: "live:local", CreatedAt: fixedLiveCreatedAt})
	if err := Validate(bundle); err != nil {
		t.Fatalf("Validate rejected a credential-free URL: %v", err)
	}
}

// TestValidateAcceptsPublicIPv6Endpoints keeps the IPv6 branch consistent with
// the IPv4 ones, which only reject loopback and private ranges. Treating every
// bracketed IPv6 literal as private would reject a bundle whose status text
// merely mentions a public resolver.
func TestValidateAcceptsPublicIPv6Endpoints(t *testing.T) {
	snapshot := realisticLiveSnapshot()
	snapshot.HealthReasons = []string{"upstream resolver [2606:4700:4700::1111]:443 responded slowly"}
	bundle := BuildLiveBundle(snapshot, LiveBundleOptions{ScopeID: "live:local", CreatedAt: fixedLiveCreatedAt})
	if err := Validate(bundle); err != nil {
		t.Fatalf("Validate rejected a public IPv6 endpoint: %v", err)
	}
}

// TestValidateRejectsPrivateIPv6Endpoints covers the ranges that are private:
// loopback, unique-local, and link-local.
func TestValidateRejectsPrivateIPv6Endpoints(t *testing.T) {
	for _, endpoint := range []string{"[::1]:5432", "[fd00::5]:5432", "[fe80::1]:7687"} {
		t.Run(endpoint, func(t *testing.T) {
			snapshot := realisticLiveSnapshot()
			snapshot.HealthReasons = []string{"dial tcp " + endpoint + ": connection refused"}
			bundle := BuildLiveBundle(snapshot, LiveBundleOptions{ScopeID: "live:local", CreatedAt: fixedLiveCreatedAt})
			if err := Validate(bundle); err == nil {
				t.Fatalf("Validate accepted private IPv6 endpoint %q", endpoint)
			}
		})
	}
}

// TestValidateKeepsQueryParamNoiseUsable covers the false positive the
// credential-URL pattern could produce on a pathless URL: the user class
// excluded "/" but not "?" or "#", so a query string carrying a colon and an @
// looked like userinfo and was reported as a credential.
func TestValidateKeepsQueryParamNoiseUsable(t *testing.T) {
	// These deliberately avoid the words "secret", "password", "token", and
	// "api_key": the pre-existing credentialPattern matches those literals on
	// their own, independent of URL shape, so including one would test that
	// guard rather than this one.
	for _, text := range []string{
		"https://example.com?user=admin:value@handle",
		"https://example.com?session=user:pass@domain",
		"https://example.com#frag=a:b@c",
	} {
		t.Run(text, func(t *testing.T) {
			snapshot := realisticLiveSnapshot()
			snapshot.HealthReasons = []string{"upstream " + text + " returned 503"}
			bundle := BuildLiveBundle(snapshot, LiveBundleOptions{ScopeID: "live:local", CreatedAt: fixedLiveCreatedAt})
			if err := Validate(bundle); err != nil {
				t.Fatalf("Validate rejected query/fragment noise as a credential: %v", err)
			}
		})
	}
}

// TestValidateRejectsMixedCasePrivateHosts covers DNS being case-insensitive.
// The host alternatives were case-sensitive, so LOCALHOST:5432 exported clean.
func TestValidateRejectsMixedCasePrivateHosts(t *testing.T) {
	for _, endpoint := range []string{
		"LOCALHOST:5432", "LocalHost:5432", "DB.INTERNAL:5432", "Db.Internal:5432",
	} {
		t.Run(endpoint, func(t *testing.T) {
			snapshot := realisticLiveSnapshot()
			snapshot.HealthReasons = []string{"dial tcp " + endpoint + ": refused"}
			bundle := BuildLiveBundle(snapshot, LiveBundleOptions{ScopeID: "live:x", CreatedAt: fixedLiveCreatedAt})
			if err := Validate(bundle); err == nil {
				t.Fatalf("Validate accepted mixed-case private endpoint %q", endpoint)
			}
		})
	}
}

// TestValidateRejectsFilesystemRootsBeyondTheOriginalList covers roots the
// original prefix list missed entirely.
func TestValidateRejectsFilesystemRootsBeyondTheOriginalList(t *testing.T) {
	for _, path := range []string{
		"/root/.config/eshu/config", "/etc/eshu/config", "/usr/local/share/eshu/x", "/data/eshu/store",
	} {
		t.Run(path, func(t *testing.T) {
			snapshot := realisticLiveSnapshot()
			snapshot.HealthReasons = []string{"could not read " + path}
			bundle := BuildLiveBundle(snapshot, LiveBundleOptions{ScopeID: "live:x", CreatedAt: fixedLiveCreatedAt})
			if err := Validate(bundle); err == nil {
				t.Fatalf("Validate accepted local path %q", path)
			}
		})
	}
}

// TestValidateKeepsApiRouteTargetsUsable is the counterweight to the widened
// path rule: the bundle's own reproduce calls carry bare API routes, so a
// general "any absolute path" rule would reject every live bundle.
func TestValidateKeepsApiRouteTargetsUsable(t *testing.T) {
	bundle := BuildLiveBundle(realisticLiveSnapshot(), LiveBundleOptions{ScopeID: "live:x", CreatedAt: fixedLiveCreatedAt})
	var sawRoute bool
	for _, call := range bundle.Reproduce {
		if call.Kind == "api" {
			sawRoute = true
		}
	}
	if !sawRoute {
		t.Fatal("expected the live bundle to carry api reproduce routes")
	}
	if err := Validate(bundle); err != nil {
		t.Fatalf("Validate rejected the bundle's own api reproduce routes: %v", err)
	}
}

// TestValidateRejectsLocalPathsAfterAnyDelimiter covers the delimiters real
// diagnostics put in front of a path. The rule only accepted a quote or
// whitespace, so a path exported clean the moment it followed "=", ":", "(" or
// "[" -- which is how structured reasons and Go's own url/file errors read.
func TestValidateRejectsLocalPathsAfterAnyDelimiter(t *testing.T) {
	for _, reason := range []string{
		"config_path=/etc/eshu/config",
		"(/root/.config/eshu)",
		"[/var/lib/eshu]",
		"cwd:/Users/alice/repo",
		"loading:/home/alice/repo",
		"file:///etc/eshu/config",
	} {
		t.Run(reason, func(t *testing.T) {
			snapshot := realisticLiveSnapshot()
			snapshot.HealthReasons = []string{reason}
			bundle := BuildLiveBundle(snapshot, LiveBundleOptions{ScopeID: "live:x", CreatedAt: fixedLiveCreatedAt})
			if err := Validate(bundle); err == nil {
				t.Fatalf("Validate accepted local path in %q", reason)
			}
		})
	}
}

// TestValidateRejectsClusterLocalAndPortlessPrivateAddresses covers the shapes
// a live Kubernetes stack actually emits. The previous rules matched a
// hostname only if it contained the literal "internal" and matched an IP only
// when a port followed it, so the two most common real forms -- a service DNS
// name and a collector reason naming a bare instance IP -- exported clean.
func TestValidateRejectsClusterLocalAndPortlessPrivateAddresses(t *testing.T) {
	for _, reason := range []string{
		"eshu-api.eshu.svc.cluster.local:8080 refused the connection",
		"nornicdb.eshu.svc.cluster.local:7687 refused the connection",
		"dial eshu-api.eshu.svc.cluster.local failed",
		"collector instance 10.0.5.3 is unreachable",
		"collector instance 192.168.1.9 is unreachable",
		"collector instance 172.20.0.4 is unreachable",
		"metadata lookup via 169.254.169.254 failed",
		"peer fd00::5 did not respond",
	} {
		t.Run(reason, func(t *testing.T) {
			snapshot := realisticLiveSnapshot()
			snapshot.HealthReasons = []string{reason}
			bundle := BuildLiveBundle(snapshot, LiveBundleOptions{ScopeID: "live:x", CreatedAt: fixedLiveCreatedAt})
			if err := Validate(bundle); err == nil {
				t.Fatalf("Validate accepted private address in %q", reason)
			}
		})
	}
}
