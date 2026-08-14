// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package answerguardrail

import (
	"regexp"
	"strings"
)

// Criterion identifies one runtime answer guardrail.
type Criterion string

const (
	// CriterionCitationCoverage requires supported published answers to carry
	// at least one citation or evidence handle.
	CriterionCitationCoverage Criterion = "citation_coverage"
	// CriterionPublishSafety rejects publishable strings that look like private
	// paths, hosts, credentials, or raw addresses.
	CriterionPublishSafety Criterion = "publish_safety"
	// CriterionAnswerSubstance rejects a tautological, identity-only answer that
	// only restates the question's entity and carries no operational fact, even
	// when citation and publish-safety pass.
	CriterionAnswerSubstance Criterion = "answer_substance"
)

// Result is the bounded answer shape evaluated by guardrails.
type Result struct {
	// Question is the original question the answer addresses. It is used only by
	// the answer-substance check to detect a circular, identity-only answer; an
	// empty Question disables that check.
	Question      string
	AnswerSummary string
	Supported     bool
	// CitationHandles are inlined evidence handles that cover the prose.
	CitationHandles []string
	// TruthProvenance reports whether the prose is backed by a classified
	// packet's truth provenance (a non-empty truth_class). It is a recognized
	// citation_coverage class on par with citation handles, mirroring the
	// narration validator, which allows ProvenanceTruth whenever the packet's
	// truth_class is non-empty. It does not weaken coverage for genuinely uncited
	// prose: prose with no handles and no truth provenance is still blocked
	// (issue #3609).
	TruthProvenance bool
	Limitations     []string
	NextCalls       []string
	Metadata        []string
}

// Finding describes one failed guardrail without echoing the unsafe value.
type Finding struct {
	Criterion Criterion
	Detail    string
}

// Verdict is the aggregate guardrail result.
type Verdict struct {
	Valid    bool
	Findings []Finding
}

// The publish-safety screen's address and userinfo rules. Each one runs on a
// publish path, so each is written to the same standard: catch the shape the
// product actually emits, and name in a comment what it deliberately does not
// catch. A screen that withholds an honest answer is its own outage, so "reject
// more" is not automatically the safer direction here.
//
// The "password:" assignment rule is held to the same standard and lives in
// guardrail_password.go, because it carries a value classifier rather than a
// pattern alone.
var (
	// rawAddressPattern is the IPv4 half of the raw-address rule. Blanket by
	// design: any dotted quad, private or public.
	rawAddressPattern = regexp.MustCompile(`\b[0-9]{1,3}(?:\.[0-9]{1,3}){3}\b`)

	// compressedIPv6Pattern and fullIPv6Pattern are the other half of the same
	// rule, which the doc comment promised ("raw addresses") and the IPv4-only
	// regex never delivered: "[fd00::1]" published clean.
	//
	// They are two patterns rather than one because each is gated in
	// UnsafeString on a substring it cannot match without -- "::" for the
	// compressed forms, seven colons for the uncompressed one -- and one
	// combined pattern would have to run whenever either gate opened. See
	// UnsafeString for why the gates exist.
	//
	// Within each, two groups of alternatives, deliberately not one blanket
	// rule, because their false-positive risk is not the same.
	//
	// Bracketed, which is how an address appears in a host:port string. Square
	// brackets around a run of hex digits and colons are an IPv6 literal and
	// nothing else, so this half needs no left boundary. It does require
	// either the "::" compression marker or the full eight hextets: without
	// that, "buf[0:4]" and Go's three-index "buf[0:4:8]" are both all-hex
	// bracketed colon runs and would be rejected as addresses.
	//
	// Unbracketed, which is how one appears in free text. The left boundary
	// here excludes letters and digits but NOT the colon, and that choice is
	// the opposite of the one evidencebundle's privateULAv6Pattern makes.
	// The two rules are asking different questions, so they land differently:
	// evidencebundle screens PRIVATE endpoints, so matching a middle hextet
	// would wrongly reject the public address 2001:db8:fd12::1; this rule
	// screens raw addresses of any kind -- rawAddressPattern above already
	// rejects every dotted quad, public or private -- so a middle-hextet match
	// on a public address is the intended answer, not a false positive.
	// Allowing the colon is what catches ":fd00::1" after a label and bare
	// "2001:db8::1", where the character before the matching hextet is a colon.
	//
	// Excluding letters and digits is what keeps a scope-resolution operator
	// out. "std::vec", "tokio::spawn", and "value::text" all fail because the
	// hex run ending at the "::" is preceded by a letter that is not a hex
	// digit.
	//
	// Both sides carry a boundary, and they exclude the same characters. The
	// right one is why "db::connect", "ca::cert", and "ff::Field" publish: the
	// first version of this rule spelled the right side as "[0-9a-f:]*", which
	// matches the empty string, so a 1-4 hex-character namespace followed by
	// "::" matched on its prefix alone no matter what came after it and every
	// such identifier was withheld. Spelling both sides as whole hextet groups
	// and requiring a boundary after the last one is what makes the rule mean
	// "this token IS an address" rather than "this token starts like one".
	//
	// Known gap, accepted: an all-hex identifier pair such as "abc::def" is
	// indistinguishable from a compressed address by shape alone and IS
	// rejected -- both sides are valid hextets, so no boundary can separate
	// them. Nothing else in the honest corpus is -- timestamps ("12:30:45"),
	// durations, and version strings carry no "::" at all, which is why the
	// compression marker is required rather than a general run of
	// colon-separated hextets.
	compressedIPv6Pattern = regexp.MustCompile(`(?i)(\[[0-9a-f:]*::[0-9a-f:]*\]|(^|[^0-9a-z])(?:[0-9a-f]{1,4}(?::[0-9a-f]{1,4})*)?::(?:[0-9a-f]{1,4}(?::[0-9a-f]{1,4})*)?([^0-9a-z]|$))`)

	// fullIPv6Pattern is the uncompressed eight-hextet form, bracketed or not.
	// It carries no "::" so it needs its own gate, and its own pattern.
	fullIPv6Pattern = regexp.MustCompile(`(?i)(\[[0-9a-f]{1,4}(:[0-9a-f]{1,4}){7}\]|(^|[^0-9a-z])[0-9a-f]{1,4}(:[0-9a-f]{1,4}){7})`)

	// credentialUserinfoPattern rejects a connection string that carries a
	// password in its userinfo, on any scheme or none. The rule this replaces
	// matched the literal substrings "http://" and "https://", so every DSN
	// shape this product actually handles -- bolt, neo4j+s, postgres, redis,
	// amqp -- published clean, as did the schemeless "svc:PW@host/tool" form.
	//
	// Two alternatives:
	//
	//   - Scheme-bearing. This is evidencebundle's credentialURLPattern
	//     verbatim, which already ships with its own tests: it requires
	//     "user:pass@" after the "://", so an ordinary URL -- including one
	//     with a port, "https://host:443/x" -- does not match.
	//   - Schemeless. The same userinfo pair, but since there is no "://" to
	//     anchor on, the host must be followed by a port or a path. That
	//     trailing requirement is what separates "svc:PW@host/tool" from an
	//     ordinary email address written after a label, "contact:alice@
	//     example.com", which carries neither.
	//
	// The literal http/https substring check is kept below as well: it rejects
	// any URL on those two schemes, userinfo or not, which is broader than
	// this rule and is existing behaviour worth keeping.
	credentialUserinfoPattern = regexp.MustCompile(`(?i)([a-z][a-z0-9+.-]*://[^/\s:@"?#]+:[^/\s@"?#]+@|[^\s:@/"'?#]+:[^\s:@/"'?#]+@[a-z0-9.-]+(:[0-9]{1,5}|/))`)
)

// ValidateResult evaluates result against runtime-safe citation and publish
// safety rules. It performs no I/O and never calls providers.
func ValidateResult(result Result) Verdict {
	var findings []Finding
	if result.Supported && strings.TrimSpace(result.AnswerSummary) != "" && !hasCoverage(result) {
		findings = append(findings, Finding{
			Criterion: CriterionCitationCoverage,
			Detail:    "supported published answer has no citation handles or truth provenance",
		})
	}
	if FirstUnsafeString(result.Strings()) != "" {
		findings = append(findings, Finding{
			Criterion: CriterionPublishSafety,
			Detail:    "publishable answer contains a restricted private or credential-like value",
		})
	}
	if result.Supported && strings.TrimSpace(result.Question) != "" &&
		IsCircularAnswer(result.Question, result.AnswerSummary) {
		findings = append(findings, Finding{
			Criterion: CriterionAnswerSubstance,
			Detail:    "supported answer is circular or identity-only and names no operational fact",
		})
	}
	return Verdict{
		Valid:    len(findings) == 0,
		Findings: findings,
	}
}

// HasFinding reports whether the verdict contains criterion.
func (v Verdict) HasFinding(criterion Criterion) bool {
	for _, finding := range v.Findings {
		if finding.Criterion == criterion {
			return true
		}
	}
	return false
}

// Strings returns every publishable string carried by result.
func (r Result) Strings() []string {
	values := []string{r.AnswerSummary}
	values = append(values, r.CitationHandles...)
	values = append(values, r.Limitations...)
	values = append(values, r.NextCalls...)
	values = append(values, r.Metadata...)
	return values
}

// FirstUnsafeString returns the first value rejected by the publish-safety
// scanner, or the empty string when all values are publish-safe.
func FirstUnsafeString(values []string) string {
	for _, value := range values {
		if UnsafeString(value) {
			return value
		}
	}
	return ""
}

// UnsafeString reports whether value looks unsafe for publishable answer
// output. It is intentionally conservative and deterministic.
//
// Each regex below is gated on a substring the regex cannot possibly match
// without. The gates are not incidental. This runs once per publishable string
// on the Ask response path, and on BenchmarkUnsafeStringHonestCorpus (8 honest
// strings, best of 5 at -benchtime=2s):
//
//	before this change              6015 ns/op
//	new rules, no gates            38906 ns/op   6.5x
//	new rules, gated                7480 ns/op   1.24x
//
// The residual 1.24x is not spread evenly, and the split says where it sits:
// the seven corpus strings carrying no "::" cost 5251 -> 5502 ns/op (+4.8%,
// about 36ns per string, which is the gate scans themselves), while the one
// string that does carry "::" -- "std::collections::HashMap has 12 callers" --
// costs 678 -> 1968 ns/op, because for that shape the gate opens and the regex
// genuinely runs. Ordinary answer prose pays the 36ns; text carrying a scope
// resolution operator pays about 1.3us.
//
// Deleting a gate preserves behaviour and costs 6.5x, so if one goes, re-run
// BenchmarkUnsafeStringHonestCorpus.
func UnsafeString(value string) bool {
	lower := strings.ToLower(value)
	if rawAddressPattern.MatchString(value) {
		return true
	}
	// Userinfo needs an "@". Citation handles, routes, and prose rarely carry
	// one, so this gate closes on nearly every honest string.
	if strings.IndexByte(value, '@') >= 0 && credentialUserinfoPattern.MatchString(value) {
		return true
	}
	// A compressed address needs the "::" marker. A single colon is everywhere
	// in this corpus (handles are colon-joined); a double colon is not.
	if strings.Contains(value, "::") && compressedIPv6Pattern.MatchString(value) {
		return true
	}
	// An uncompressed address is eight hextets, so seven colons at minimum.
	if strings.Count(value, ":") >= 7 && fullIPv6Pattern.MatchString(value) {
		return true
	}
	if strings.Contains(lower, "password") && passwordAssignmentIsUnsafe(lower) {
		return true
	}
	for _, fragment := range []string{
		"/users/",
		"/home/",
		"\\users\\",
		"bearer ",
		"password=",
		"token=",
		"secret=",
		"api_key=",
		"api-key=",
		".internal",
		".corp",
		".local",
	} {
		if strings.Contains(lower, fragment) {
			return true
		}
	}
	return strings.Contains(lower, "http://") || strings.Contains(lower, "https://")
}

func hasCitation(handles []string) bool {
	for _, handle := range handles {
		if strings.TrimSpace(handle) != "" {
			return true
		}
	}
	return false
}

// hasCoverage reports whether a supported published answer carries an accepted
// citation_coverage class: inlined evidence handles OR truth provenance (a
// classified packet's non-empty truth_class). Truth provenance is recognized on
// par with citation handles, mirroring the narration validator's ProvenanceTruth
// allowance. Prose with neither is uncovered and still fails the criterion
// (issue #3609).
func hasCoverage(result Result) bool {
	return hasCitation(result.CitationHandles) || result.TruthProvenance
}
