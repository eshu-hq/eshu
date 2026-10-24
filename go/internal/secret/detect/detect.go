// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package detect

import (
	"regexp"
	"strings"
)

// The detector pattern is written once, as three parts, so the SQL form and
// the RE2 forms below cannot drift. The parts keep the POSIX bracket classes
// that Postgres ARE and Go RE2 both accept.
const (
	// keyPrefixPattern matches a credential-named key, its separator, and an
	// optional opening quote.
	keyPrefixPattern = `(password|passwd|pwd|api[_-]?key|apikey|token|secret|client[_-]?secret|private[_-]?key|authorization)[[:space:]]*[:=][[:space:]]*['"]?`
	// valuePattern matches the literal a credential key assigns; a shorter
	// value is not a finding.
	valuePattern = `[A-Za-z0-9_./+=:@!#$%^-]{6,}`
	// shapePattern matches credentials that need no key: an AWS access key id,
	// a live Stripe key, a Slack token, and a PEM private-key header.
	shapePattern = `AKIA[0-9A-Z]{16}|sk_live_[A-Za-z0-9]{8,}|xox[baprs]-[A-Za-z0-9-]{10,}|-----BEGIN [A-Z ]*PRIVATE KEY-----`
)

// SQLPattern is the case-insensitive Postgres ARE pattern migration 138 applies
// twice in eshu_secret_line_findings (file prefilter and line filter). The
// query package's hardcodedSecretSQLPattern is this constant.
const SQLPattern = keyPrefixPattern + valuePattern + `|` + shapePattern

// kindArm is one WHEN arm of the migration 138 CASE.
type kindArm struct {
	regex *regexp.Regexp
	kind  string
}

// kindArmSources lists the CASE arms in migration 138 order. The value-shape
// arms come first, which is why a marker that replaces a shape removes the
// finding under migration 138.
var kindArmSources = []struct{ Regex, Kind string }{
	{`AKIA[0-9A-Z]{16}`, "aws_access_key"},
	{`-----BEGIN [A-Z ]*PRIVATE KEY-----`, "private_key"},
	{`xox[baprs]-[A-Za-z0-9-]{10,}`, "slack_token"},
	{`(api[_-]?key|apikey|token)[[:space:]]*[:=]`, "api_token"},
	{`(password|passwd|pwd)[[:space:]]*[:=]`, "password_literal"},
	{`(secret|client[_-]?secret|private[_-]?key|authorization)[[:space:]]*[:=]`, "secret_literal"},
}

var (
	kindArms = func() []kindArm {
		arms := make([]kindArm, 0, len(kindArmSources))
		for _, source := range kindArmSources {
			arms = append(arms, kindArm{regex: regexp.MustCompile("(?i)" + source.Regex), kind: source.Kind})
		}
		return arms
	}()
	// spanPattern captures the value of a key match in group 2 so the redactor
	// can replace the value alone; a shape match has no group.
	spanPattern = regexp.MustCompile("(?i)" + keyPrefixPattern + "(" + valuePattern + ")|" + shapePattern)
)

// ArmSources returns the migration 138 CASE arms as (regex, kind) pairs in
// order, so a test can compare them with the migration text.
func ArmSources() []struct{ Regex, Kind string } {
	out := make([]struct{ Regex, Kind string }, len(kindArmSources))
	copy(out, kindArmSources)
	return out
}

// Kinds returns the finding kinds in CASE order.
func Kinds() []string {
	kinds := make([]string, 0, len(kindArms))
	for _, arm := range kindArms {
		kinds = append(kinds, arm.kind)
	}
	return kinds
}

// Classify returns the finding kind migration 138 gives one line, or "" when no
// arm matches. It is the CASE only: the line gate (SQLPattern) is Spans.
func Classify(line string) string {
	for _, arm := range kindArms {
		if arm.regex.MatchString(line) {
			return arm.kind
		}
	}
	return ""
}

// Span is a byte range of one line that holds a credential value.
type Span struct {
	Start, End int
}

// Spans returns the credential spans SQLPattern matches in one line: only the
// value for a key match, the whole shape for a value-shape match.
// Go's case folding is wider than Postgres ARE's for a few non-ASCII runes, so
// Spans can only find more than the migration, never fewer; the engine-parity
// corpus test in #8833 measures that gap.
func Spans(line string) []Span {
	if !mayMatch(line) {
		return nil
	}
	matches := spanPattern.FindAllStringSubmatchIndex(line, -1)
	spans := make([]Span, 0, len(matches))
	for _, match := range matches {
		// Group 1 is the key word inside keyPrefixPattern; group 2 is the value.
		if match[4] >= 0 {
			spans = append(spans, Span{Start: match[4], End: match[5]})
			continue
		}
		spans = append(spans, Span{Start: match[0], End: match[1]})
	}
	return spans
}

// matchWords are lowercase substrings every SQLPattern match contains: the
// credential key words and the value-shape anchors. A line holding none of
// them cannot match, so Spans skips the regular expression, which costs about
// 80 ns per byte, for the common clean line.
var matchWords = []string{
	"pass", "pwd", "key", "token", "secret", "authorization", "akia", "sk_live_", "xox", "-----begin",
}

func mayMatch(line string) bool {
	lower := strings.ToLower(line)
	for _, word := range matchWords {
		if strings.Contains(lower, word) {
			return true
		}
	}
	return false
}
