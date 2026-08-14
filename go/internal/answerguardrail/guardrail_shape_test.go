// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package answerguardrail

import (
	"strings"
	"testing"
)

// UnsafeString is the publish-safety screen for everything Ask Eshu is about to
// print. Three shapes it claimed to cover walked straight through it.
//
// Reproduced against a binary built from origin/main at fc462effc before this
// branch was touched, through the answer-quality scorecard, which runs the same
// screen:
//
//	eshu answer-quality-scorecard --from <evidence with the value in run_id>
//	eshu answer-quality-scorecard --from <evidence with the value in a summary>
//
//	bolt://neo4j:S3NT1NEL@graph.example.com:7687   rc=0  [ok] publish_safety
//	postgres://eshu:S3NT1NEL@db.example.net:5432   rc=0  [ok] publish_safety
//	svc:S3NT1NEL@host/tool                         rc=0  [ok] publish_safety
//	[fd00::1]:7687                                 rc=0  [ok] publish_safety
//	fd00::1                                        rc=0  [ok] publish_safety
//	password: S3NT1NEL                             rc=0  [ok] publish_safety
//
// and, as the positive control that proves the run was measuring anything at
// all, two values the screen ALREADY caught, in the same two carriers:
//
//	10.42.7.9                                      rc=1  [!!] publish_safety
//	http://graph.example.com:7687                  rc=1  [!!] publish_safety
//
// The cause is the carrier SHAPE, not the delimiter around it: the screen
// matched the literal substrings "http://" and "https://", so any other scheme
// — or none — carried userinfo past it; rawAddressPattern was IPv4-only while
// its doc comment promised "raw addresses"; and the credential fragment list
// spelled "password=" but not the YAML/log form "password:".

// alreadyCaughtValues are values origin/main already rejected. Every table
// below runs them too, so a table that reports its new rows clean has first
// shown it can report anything at all. A clean canary nobody has watched fail
// is not evidence.
var alreadyCaughtValues = []string{
	"10.42.7.9",
	"http://graph.example.com:7687",
	"https://graph.example.com/x",
	"/Users/ops/eshu.yaml",
	"bearer S3NT1NEL",
	"password=S3NT1NEL",
	"db.internal",
}

// TestUnsafeStringPositiveControl is the control for every table in this file.
// If it fails, no "clean" result elsewhere in the package means anything.
func TestUnsafeStringPositiveControl(t *testing.T) {
	t.Parallel()

	for _, value := range alreadyCaughtValues {
		t.Run(value, func(t *testing.T) {
			if !UnsafeString(value) {
				t.Fatalf("UnsafeString(%q) = false; the screen no longer catches a value it always caught, "+
					"so every clean result in this package is meaningless until this is fixed", value)
			}
		})
	}
}

// credentialCarriers are the DSN spellings this product actually handles. Only
// the http and https spellings were screened.
var credentialCarriers = []string{
	"bolt://neo4j:S3NT1NEL@graph.example.com:7687",
	"neo4j+s://neo4j:S3NT1NEL@graph.example.com:7687",
	"postgres://eshu:S3NT1NEL@db.example.net:5432/eshu",
	"postgresql://eshu:S3NT1NEL@db.example.net:5432/eshu",
	"redis://default:S3NT1NEL@cache.example.net:6379",
	"amqp://svc:S3NT1NEL@broker.example.net:5672/eshu",
	// No scheme at all: the shape a connection string takes in a config dump.
	"svc:S3NT1NEL@host/tool",
	"eshu:S3NT1NEL@db.example.net:5432",
}

// TestUnsafeStringRejectsCredentialUserinfoOnAnyScheme pins the first hole. The
// scheme is not what makes a URL unsafe; the userinfo is.
func TestUnsafeStringRejectsCredentialUserinfoOnAnyScheme(t *testing.T) {
	t.Parallel()

	for _, value := range credentialCarriers {
		t.Run(value, func(t *testing.T) {
			if !UnsafeString(value) {
				t.Fatalf("UnsafeString(%q) = false, want true", value)
			}
		})
	}
}

// rawIPv6Carriers are the address spellings rawAddressPattern's doc comment
// promised and its IPv4-only regex never delivered.
var rawIPv6Carriers = []string{
	"[fd00::1]",
	"[fd00::1]:7687",
	"[::1]:5432",
	"[2001:db8::1]:7687",
	"fd00::1",
	"fc00::42",
	"fe80::1",
	"::1",
	"2001:db8:0000:0000:0000:ff00:0042:8329",
	// Public and unbracketed. Unlike evidencebundle's private-endpoint rules,
	// this screen rejects every raw address -- rawAddressPattern already
	// rejects any dotted quad, public or private -- so a public IPv6 is a
	// catch here, not a false positive.
	"2001:db8::1",
}

// TestUnsafeStringRejectsRawIPv6 pins the second hole.
func TestUnsafeStringRejectsRawIPv6(t *testing.T) {
	t.Parallel()

	for _, value := range rawIPv6Carriers {
		t.Run(value, func(t *testing.T) {
			if !UnsafeString(value) {
				t.Fatalf("UnsafeString(%q) = false, want true", value)
			}
		})
	}
}

// TestUnsafeStringRejectsCompressedIPv6WithoutMatchingAnIdentifier is the pair
// this rule needs stated together: the same table has to show the rule firing
// on an address AND not firing on a namespace whose first segment happens to be
// hex. Split across two tests, a boundary that rejects everything and a
// boundary that rejects nothing each look fine in one of them.
func TestUnsafeStringRejectsCompressedIPv6WithoutMatchingAnIdentifier(t *testing.T) {
	t.Parallel()

	for value, want := range map[string]bool{
		"fd00::1":        true,
		"::1":            true,
		"2001:db8::1":    true,
		"[fd00::1]:7687": true,
		// The IPv4-mapped form, which the right boundary must not break: the
		// tail continues past the last hextet into a dotted quad.
		"::ffff:10.0.5.3": true,
		// Documented gap: both sides are valid hextets, so no boundary can
		// tell this from an address. If this ever flips, the README's
		// known-limits list is wrong and has to change with it.
		"abc::def": true,
		// A namespace whose first segment is 1-4 hex characters. Every one of
		// these was withheld while the right side was spelled "[0-9a-f:]*".
		"db::connect":   false,
		"abc::default":  false,
		"ca::cert":      false,
		"de::btree":     false,
		"ff::Field":     false,
		"fade::unwrap":  false,
		"beef::marbled": false,
	} {
		t.Run(value, func(t *testing.T) {
			if got := UnsafeString(value); got != want {
				t.Fatalf("UnsafeString(%q) = %v, want %v", value, got, want)
			}
		})
	}
}

// boundaryPrefixes and boundarySuffixes are the delimiter axis. For this screen
// they are expected to make no difference — A's hole is the carrier shape, not
// the boundary — which is exactly why it is worth pinning: a future narrowing
// that accidentally anchors one of these rules to a space would show up here
// rather than in production.
var (
	boundaryPrefixes = []string{"", " ", ":", "/", `"`, "'", "@", "?", "&", "(", "=", "\n"}
	boundarySuffixes = []string{"", " ", ":", "/", `"`, "'", "?", "&", ")", ",", "\n"}
)

// TestUnsafeStringIsIndifferentToTheSurroundingDelimiter runs every newly
// covered carrier against every boundary character, in both positions.
func TestUnsafeStringIsIndifferentToTheSurroundingDelimiter(t *testing.T) {
	t.Parallel()

	carriers := make([]string, 0,
		len(credentialCarriers)+len(rawIPv6Carriers)+len(passwordAssignmentCarriers)+len(alreadyCaughtValues))
	carriers = append(carriers, credentialCarriers...)
	carriers = append(carriers, rawIPv6Carriers...)
	// The password carriers ride this sweep because the rule now reads the
	// value after the colon, and a delimiter the corpus wraps values in --
	// a quote, a comma, a closing bracket -- lands inside that value.
	carriers = append(carriers, passwordAssignmentCarriers...)
	// The already-caught values ride along so this table cannot report a clean
	// sweep without also having exercised something it is known to reject.
	carriers = append(carriers, alreadyCaughtValues...)

	checked := 0
	for _, carrier := range carriers {
		for _, prefix := range boundaryPrefixes {
			for _, suffix := range boundarySuffixes {
				value := prefix + carrier + suffix
				checked++
				if !UnsafeString(value) {
					t.Errorf("UnsafeString(%q) = false, want true (carrier %q, prefix %q, suffix %q)",
						value, carrier, prefix, suffix)
				}
			}
		}
	}
	// Test filters and empty tables fail silently. Count what ran.
	if want := len(carriers) * len(boundaryPrefixes) * len(boundarySuffixes); checked != want {
		t.Fatalf("checked %d combinations, want %d", checked, want)
	}
}

// TestUnsafeStringKeepsOrdinaryAnswerTextPublishable is the false-positive half
// of the widening, and the reason the new rules are shaped the way they are.
// This screen runs on a publish path: an answer it wrongly withholds is an
// outage of its own, so every value here is text a real Eshu answer, citation
// handle, limitation, or next call carries.
func TestUnsafeStringKeepsOrdinaryAnswerTextPublishable(t *testing.T) {
	t.Parallel()

	for _, value := range []string{
		// Citation handles: colon-joined, which is what askCitationHandleStrings
		// produces.
		"source:file:service/main.go",
		"advisory:GHSA-demo",
		"package:pkg:golang/example.com/demo",
		"capability:platform.impact.pre_change",
		"answer:ask-eshu:demo",
		// A module handle carrying an @version.
		"module:github.com/eshu-hq/eshu/go@v1.2.3",
		"pkg:golang/example.com/demo@v1.2.3",
		// Next calls and routes.
		"POST /api/v0/impact/pre-change",
		"GET /api/v0/status/index",
		"eshu change impact",
		"mcp:analyze_pre_change_impact",
		// Timestamps, durations, counts, versions. A loose IPv6 rule eats these.
		"observed at 12:30:45 UTC",
		"2026-06-20T00:00:00Z",
		"drained in 1:30:00",
		"backend nornicdb version 1.1.11",
		"stage:parse pending:1024",
		// Scope-resolution operators. A blanket "hex::hex" rule eats these, so
		// the IPv6 rule requires every group to be a valid hextet.
		"std::collections::HashMap",
		"tokio::spawn",
		"serde::json::Value",
		"k8s::api::core",
		"cast the column with value::text",
		// The same operator where the FIRST segment is all hex, which is the
		// case a left boundary alone does not cover.
		"db::connect has 4 callers",
		"abc::default is generated",
		"ca::cert loads the bundle",
		"ff::Field is a marker type",
		"de::btree is the storage engine",
		// Bracketed all-hex colon runs that are slice expressions, not
		// addresses. This is why the bracketed IPv6 rule requires either "::"
		// or the full eight hextets.
		"buf[0:4] holds the header",
		"buf[0:4:8] is a three-index slice",
		// Real identifiers that end in a credential keyword. evidencebundle
		// learned this the hard way: matching the bare keyword rejects honest
		// content, which is why the password rule needs a word boundary.
		"function checkPassword: 3 callers",
		"aws_appsync_api_key: 2 resources",
		"aws_secretsmanager_secret: 5 resources",
		"domain secrets_iam_trust_chain blocked",
		"field reset_password is unused",
		"passwords: 12 rotated",
		// A password key whose value is a type, a count, or a placeholder. The
		// keyword is not what makes a line a credential; the value is.
		"password: string",
		"password: String!",
		"password: Option<String>",
		"password: SecretStr",
		"random_password: 3 resources",
		"password: <redacted>",
		// Ordinary email and mailto, which are not userinfo.
		"owner alice@example.com",
		"mailto:alice@example.com",
		"contact:alice@example.com",
		// An ordinary port-bearing URL with no userinfo. The http rule already
		// rejects this one on its scheme, so use a non-http scheme to isolate
		// the userinfo rule.
		"bolt://graph.example.com:7687",
		"grpc://api.example.com:443/x",
	} {
		t.Run(value, func(t *testing.T) {
			if UnsafeString(value) {
				t.Fatalf("UnsafeString(%q) = true; this screen runs on a publish path "+
					"and withholding an honest answer is its own outage", value)
			}
		})
	}
}

// TestFirstUnsafeStringStillReportsTheFirstRejection keeps the exported helper
// honest across the widening: it must return the offending value itself, since
// callers use it as a predicate and one caller needs the value to locate it.
func TestFirstUnsafeStringStillReportsTheFirstRejection(t *testing.T) {
	t.Parallel()

	values := []string{"all fine", "still fine", "bolt://neo4j:S3NT1NEL@graph.example.com:7687", "10.42.7.9"}
	got := FirstUnsafeString(values)
	if got != values[2] {
		t.Fatalf("FirstUnsafeString = %q, want %q", got, values[2])
	}
	if FirstUnsafeString(values[:2]) != "" {
		t.Fatalf("FirstUnsafeString(clean) = %q, want empty", FirstUnsafeString(values[:2]))
	}
}

// TestValidateResultFindingDoesNotEchoTheUnsafeValue pins the contract Finding
// documents for itself and the README states as an invariant: runtime callers
// put finding text straight into user-visible limitations, so a finding that
// quoted the value would publish exactly what it just refused to publish.
func TestValidateResultFindingDoesNotEchoTheUnsafeValue(t *testing.T) {
	t.Parallel()

	const secret = "bolt://neo4j:S3NT1NEL@graph.example.com:7687"
	verdict := ValidateResult(Result{
		AnswerSummary:   "the graph backend is reachable at " + secret,
		Supported:       true,
		CitationHandles: []string{"capability:ask.eshu"},
	})
	if !verdict.HasFinding(CriterionPublishSafety) {
		t.Fatalf("verdict = %#v, want a publish-safety finding", verdict)
	}
	for _, finding := range verdict.Findings {
		if strings.Contains(finding.Detail, "S3NT1NEL") || strings.Contains(finding.Detail, secret) {
			t.Fatalf("finding %q echoes the unsafe value", finding.Detail)
		}
	}
}

// benchmarkCorpus is the mix UnsafeString actually sees on the Ask publish
// path: mostly honest prose and colon-joined citation handles, which is the
// worst case for the widened screen because every rule has to run to
// completion before the value can be called safe.
var benchmarkCorpus = []string{
	"checkout-api is deployed to production via Helm from repo:demo/service.",
	"source:file:service/main.go",
	"capability:platform.impact.pre_change",
	"POST /api/v0/impact/pre-change",
	"results limited to 100 rows",
	"answer_prose citation coverage is the packet truth provenance (truth_class: exact)",
	"std::collections::HashMap has 12 callers",
	"observed at 12:30:45 UTC on 2026-06-20T00:00:00Z",
}

// BenchmarkUnsafeStringHonestCorpus measures the safe path, which is the one
// that runs on every published answer.
func BenchmarkUnsafeStringHonestCorpus(b *testing.B) {
	for i := 0; i < b.N; i++ {
		for _, value := range benchmarkCorpus {
			if UnsafeString(value) {
				b.Fatalf("corpus value %q is not publish-safe; the benchmark is measuring the wrong path", value)
			}
		}
	}
}

// BenchmarkUnsafeStringRejection measures the rejection path.
func BenchmarkUnsafeStringRejection(b *testing.B) {
	const value = "graph backend at bolt://neo4j:S3NT1NEL@graph.example.com:7687 is down"
	for i := 0; i < b.N; i++ {
		if !UnsafeString(value) {
			b.Fatal("benchmark value is not rejected; the benchmark is measuring the wrong path")
		}
	}
}
