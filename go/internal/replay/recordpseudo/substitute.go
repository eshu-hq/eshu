// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package recordpseudo

import (
	"regexp"
	"sort"
	"strings"
)

// minSubstituteLen is the shortest free-text token (a name or a tag value)
// that is rewritten inside longer text. Shorter tokens, and purely numeric
// tag values, are exact-only: they are rewritten when they are a whole
// field value or a whole ARN component, never as a substring, because a
// tag value "1" or "us" otherwise rewrites every "us-east-1" in the run.
const minSubstituteLen = 4

var (
	// arnSpanRe finds an ARN inside free text: five colon-separated header
	// fields and a resource part that runs to whitespace, a quote or a
	// delimiter that never appears in an ARN.
	arnSpanRe = regexp.MustCompile(`arn:[a-z-]*:[a-z0-9-]*:[a-z0-9-]*:[^:"'\s,|\\<>()\[\]{}]*:[^"'\s,|\\<>()\[\]{}]*`)
	// awsRegionRe is the AWS region and availability-zone grammar built from
	// the finite AWS vocabulary (never a customer-chosen word): a region is
	// structural, is never learned, and is a protected span in free text.
	awsRegionRe      = regexp.MustCompile(`(?:us|eu|ap|ca|sa|me|af|il|mx|cn)(?:-gov|-iso[a-z]?)?-(?:east|west|north|south|central|northeast|southeast|northwest|southwest)-[0-9]{1,2}[a-z]?`)
	awsRegionExactRe = regexp.MustCompile(`^` + awsRegionRe.String() + `$`)
)

// tokens returns the learned raw tokens longest first (ties lexicographic),
// so a composite is replaced before any component it contains.
func (d *dictionary) tokens() []string {
	if d.sorted != nil {
		return d.sorted
	}
	out := make([]string, 0, len(d.entries))
	for raw := range d.entries {
		out = append(out, raw)
	}
	// Longest first, then lexicographic: a total order, so the substitution
	// sequence never depends on map iteration.
	sort.Slice(out, func(a, b int) bool {
		if len(out[a]) != len(out[b]) {
			return len(out[a]) > len(out[b])
		}
		return out[a] < out[b]
	})
	d.sorted = out
	return out
}

// exactOnly reports a learned token that is only ever rewritten as a whole
// value or a whole ARN component: a free-text class (name, tag value) below
// the length floor or purely numeric. Structured classes (account, AWS id,
// address, host, email) keep their shape and are always substitutable.
func exactOnly(raw string, class Class) bool {
	if class != ClassIdent && class != ClassTagValue {
		return false
	}
	return len(raw) < minSubstituteLen || numericRe.MatchString(raw)
}

// substitute rewrites a composite value (scope id, stable key, source uri,
// Keep field): a whole-value match of a substitutable token, then ARN spans
// component by component and the remaining text on alphanumeric boundaries.
func (d *dictionary) substitute(s string) string { return d.rewrite(s, false) }

// rewrite is substitute with the whole-value rule made explicit: with whole
// set (a classified, non-Keep field) an exact-only token that is the entire
// value is rewritten too.
func (d *dictionary) rewrite(s string, whole bool) string {
	if learned, ok := d.entries[s]; ok && (whole || !exactOnly(s, learned.class)) {
		return learned.pseudonym
	}
	var b strings.Builder
	i := 0
	for _, loc := range arnSpanRe.FindAllStringIndex(s, -1) {
		start, end := loc[0], loc[1]
		if start < i || !boundedLeft(s, start) {
			continue
		}
		b.WriteString(d.substituteFree(s[i:start]))
		b.WriteString(d.substituteARN(s[start:end]))
		i = end
	}
	b.WriteString(d.substituteFree(s[i:]))
	return b.String()
}

// substituteARN rewrites one ARN by position: partition, service and region
// are never touched, the account is looked up whole, and every resource
// component (split on "/" and ":") is looked up whole first, then as free
// text. A numeric component is a qualifier (revision, version) and only ever
// carries an account pseudonym, never a tag value's.
func (d *dictionary) substituteARN(arn string) string {
	parts := strings.SplitN(arn, ":", 6)
	if len(parts) < 6 {
		return d.substituteFree(arn)
	}
	parts[4] = d.pseudonym(parts[4])
	var b strings.Builder
	resource := parts[5]
	start := 0
	for i := 0; i <= len(resource); i++ {
		if i < len(resource) && resource[i] != '/' && resource[i] != ':' {
			continue
		}
		b.WriteString(d.substituteComponent(resource[start:i]))
		if i < len(resource) {
			b.WriteByte(resource[i])
		}
		start = i + 1
	}
	parts[5] = b.String()
	return strings.Join(parts, ":")
}

func (d *dictionary) substituteComponent(component string) string {
	if learned, ok := d.entries[component]; ok {
		if !numericRe.MatchString(component) || learned.class == ClassAccount {
			return learned.pseudonym
		}
		return component
	}
	return d.substituteFree(component)
}

// substituteFree rewrites every substitutable dictionary token in s on
// alphanumeric boundaries, longest token first, skipping any match that
// lies inside a region or availability-zone span without covering it. A
// token adjacent to another letter or digit is left alone so "app" never
// rewrites "application".
func (d *dictionary) substituteFree(s string) string {
	if s == "" {
		return s
	}
	protected := regionSpans(s)
	for _, raw := range d.tokens() {
		if !strings.Contains(s, raw) || exactOnly(raw, d.entries[raw].class) {
			continue
		}
		s = replaceBounded(s, raw, d.pseudonym(raw), protected)
		protected = regionSpans(s)
	}
	return s
}

// regionSpans returns the [start, end) offsets of every AWS region or
// availability zone in s that sits on alphanumeric boundaries.
func regionSpans(s string) [][2]int {
	var out [][2]int
	for _, loc := range awsRegionRe.FindAllStringIndex(s, -1) {
		if boundedLeft(s, loc[0]) && boundedRight(s, loc[1]) {
			out = append(out, [2]int{loc[0], loc[1]})
		}
	}
	return out
}

// insideProtected reports a match that overlaps a protected span without
// containing it whole: rewriting it would change the span's grammar.
func insideProtected(start, end int, protected [][2]int) bool {
	for _, span := range protected {
		overlaps := start < span[1] && end > span[0]
		contains := start <= span[0] && end >= span[1]
		if overlaps && !contains {
			return true
		}
	}
	return false
}

func replaceBounded(s, raw, pseudonym string, protected [][2]int) string {
	var b strings.Builder
	i := 0
	for {
		j := strings.Index(s[i:], raw)
		if j < 0 {
			b.WriteString(s[i:])
			return b.String()
		}
		start := i + j
		end := start + len(raw)
		b.WriteString(s[i:start])
		if boundedLeft(s, start) && boundedRight(s, end) && !insideProtected(start, end, protected) {
			b.WriteString(pseudonym)
		} else {
			b.WriteString(raw)
		}
		i = end
	}
}

func boundedLeft(s string, start int) bool { return start == 0 || !isAlnum(s[start-1]) }
func boundedRight(s string, end int) bool  { return end == len(s) || !isAlnum(s[end]) }
func isAlnum(c byte) bool                  { return isDigit(c) || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') }
func isDigit(c byte) bool                  { return c >= '0' && c <= '9' }
func isHex(c byte) bool                    { return isDigit(c) || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F') }
