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
// names and tag values, are exact-only: they are rewritten when they are a
// whole classified field value or a whole "/"- or ":"-delimited component
// of a composite, never as a substring (a tag value "1" or "us" would
// otherwise rewrite every "us-east-1" in the run) and never in a Keep
// field.
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
// record id): a whole-value match, then ARN spans component by component
// and the remaining text as a composite (each "/"- or ":"-delimited
// component looked up whole, then free text). Exact-only tokens apply as
// whole values and whole components.
func (d *dictionary) substitute(s string) string { return d.rewrite(s, true) }

// rewrite is substitute with the exact-only rule made explicit: with exact
// set (a classified field or a composite) an exact-only token is rewritten
// where it is the entire value or a whole component; without it (a Keep
// field) only substitutable tokens apply, so a Keep value equal to a short
// name or tag value is kept.
func (d *dictionary) rewrite(s string, exact bool) string {
	if learned, ok := d.entries[s]; ok && (exact || !exactOnly(s, learned.class)) {
		return learned.pseudonym
	}
	var b strings.Builder
	i := 0
	for _, loc := range arnSpanRe.FindAllStringIndex(s, -1) {
		start, end := loc[0], loc[1]
		if start < i || !boundedLeft(s, start) {
			continue
		}
		b.WriteString(d.substituteComposite(s[i:start], exact))
		b.WriteString(d.substituteARN(s[start:end], exact))
		i = end
	}
	b.WriteString(d.substituteComposite(s[i:], exact))
	return b.String()
}

// substituteComposite rewrites non-ARN text that may be a composite (a
// stable key, a source uri, a scope id). Tokens that themselves carry a
// delimiter (a CIDR, an IPv6 address, a repository path, a path:tag
// composite) are substituted first so the split below cannot break them;
// then every "/"- or ":"-delimited component is looked up whole, so an
// exact-only short or numeric name that is a whole component is rewritten,
// and what remains is free text.
func (d *dictionary) substituteComposite(s string, exact bool) string {
	if s == "" {
		return s
	}
	s = d.substituteTokens(s, func(raw string, _ Class) bool { return strings.ContainsAny(raw, "/:") })
	return d.forEachComponent(s, func(component string, _ byte) string {
		return d.substituteComponent(component, exact, false)
	})
}

// forEachComponent applies fn to every "/"- or ":"-delimited component of
// s, keeping the delimiters in place; prev is the delimiter before the
// component (0 for the first).
func (d *dictionary) forEachComponent(s string, fn func(component string, prev byte) string) string {
	var b strings.Builder
	start := 0
	var prev byte
	for i := 0; i <= len(s); i++ {
		if i < len(s) && s[i] != '/' && s[i] != ':' {
			continue
		}
		b.WriteString(fn(s[start:i], prev))
		if i < len(s) {
			b.WriteByte(s[i])
			prev = s[i]
		}
		start = i + 1
	}
	return b.String()
}

// substituteARN rewrites one ARN by position: partition, service and region
// are never touched, the account is looked up whole, and every resource
// component (split on "/" and ":") is looked up whole first, then as free
// text. A numeric component is a qualifier (revision, version) unless it
// was learned as an account or a name; a tag value never rewrites it.
func (d *dictionary) substituteARN(arn string, exact bool) string {
	parts := strings.SplitN(arn, ":", 6)
	if len(parts) < 6 {
		return d.substituteFree(arn)
	}
	parts[4] = d.pseudonym(parts[4])
	// The component at index skip is the resource name even when a ":"
	// joins it to its type token (lambda function:NAME, rds db:NAME, logs
	// log-group:NAME); a qualifier is a ":"-joined component after it.
	skip := arnTypeSkip(parts[2], arnComponents(parts[5]))
	index := -1
	parts[5] = d.forEachComponent(parts[5], func(component string, prev byte) string {
		if component != "" {
			index++
		}
		return d.substituteComponent(component, exact, prev == ':' && index > skip)
	})
	return strings.Join(parts, ":")
}

// substituteComponent looks a component up whole, else rewrites it as free
// text. Without exact (a Keep field) an exact-only token is not applied. A
// numeric component is a qualifier unless the token was learned as an
// account or a name; a numeric tag value never rewrites it, and a numeric
// name never rewrites the ":"-qualifier position of an ARN (a Lambda
// version, a task-definition revision), which qualifier reports.
func (d *dictionary) substituteComponent(component string, exact, qualifier bool) string {
	if learned, ok := d.entries[component]; ok && (exact || !exactOnly(component, learned.class)) {
		if numericRe.MatchString(component) && learned.class != ClassAccount {
			if learned.class == ClassTagValue || qualifier {
				return component
			}
		}
		return learned.pseudonym
	}
	return d.substituteFree(component)
}

// substituteFree rewrites every substitutable dictionary token in s on
// alphanumeric boundaries, longest token first, skipping any match that
// lies inside a region or availability-zone span without covering it. A
// token adjacent to another letter or digit is left alone so "app" never
// rewrites "application".
func (d *dictionary) substituteFree(s string) string {
	return d.substituteTokens(s, func(string, Class) bool { return true })
}

// substituteTokens is substituteFree restricted to the tokens keep admits;
// exact-only tokens are never substituted as free text.
func (d *dictionary) substituteTokens(s string, keep func(raw string, class Class) bool) string {
	if s == "" {
		return s
	}
	protected := regionSpans(s)
	for _, raw := range d.tokens() {
		class := d.entries[raw].class
		if !strings.Contains(s, raw) || exactOnly(raw, class) || !keep(raw, class) {
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
