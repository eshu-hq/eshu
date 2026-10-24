// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import (
	"regexp"
	"strconv"
	"strings"
)

var (
	// placeholderPattern matches an unexpanded shell, CI or template
	// placeholder: ${X}, $X, ${{ inputs.x }} and {{ x }}.
	placeholderPattern = regexp.MustCompile(`\$\{|\$[A-Za-z_]|\{\{`)
	// operatorSpacePattern joins a comparator operator to the version that
	// follows it so ">= 18" tokenises as ">=18".
	operatorSpacePattern = regexp.MustCompile(`(<=|>=|~=|~>|==|<|>|=|\^|~)\s+`)
	// hyphenRangePattern matches the npm-style "18 - 20" range.
	hyphenRangePattern = regexp.MustCompile(`^(\S+)\s+-\s+(\S+)$`)
)

// Classify classifies one declared version string. The result never guesses:
// a named reference is an alias, an unexpanded placeholder is unresolved, and
// only a numeric version or comparator expression yields parsed numbers.
// raw is expected to be a trimmed, non-empty token; an empty string classifies
// as an alias because it names nothing.
func Classify(raw string) Spec {
	s := strings.TrimSpace(raw)
	if placeholderPattern.MatchString(s) {
		return Spec{Kind: SpecKindUnresolved}
	}
	if isRangeLike(s) {
		if spec, ok := parseRange(s); ok {
			return spec
		}
		return Spec{Kind: SpecKindAlias}
	}
	bare := strings.TrimLeft(s, "=")
	if v, ok := parseVersion(bare); ok {
		return versionSpec(v)
	}
	return Spec{Kind: SpecKindAlias}
}

// version is a parsed numeric version. components counts the numeric parts
// before any wildcard or tail.
type version struct {
	major, minor, patch *int
	components          int
	wildcard            bool
}

func versionSpec(v version) Spec {
	kind := SpecKindPartial
	// More than three numeric components keeps only the first three in
	// Major/Minor/Patch; RawSpec carries the full text.
	if v.components >= 3 && !v.wildcard {
		kind = SpecKindExact
	}
	return Spec{Kind: kind, Major: v.major, Minor: v.minor, Patch: v.patch}
}

// parseVersion reads a leading numeric dotted version with an optional "v"
// prefix, wildcard components (x, X, *) and a pre-release or build tail such
// as "rc1", "-alpine" or "+8".
func parseVersion(s string) (version, bool) {
	s = strings.TrimLeft(s, "vV")
	var v version
	rest := s
	slots := []**int{&v.major, &v.minor, &v.patch}
	for i := 0; ; i++ {
		if rest == "" {
			break
		}
		if i > 0 {
			if rest[0] != '.' {
				break
			}
			rest = rest[1:]
		}
		if rest != "" && (rest[0] == 'x' || rest[0] == 'X' || rest[0] == '*') {
			if i == 0 {
				return version{}, false
			}
			v.wildcard = true
			rest = rest[1:]
			continue
		}
		end := 0
		for end < len(rest) && rest[end] >= '0' && rest[end] <= '9' {
			end++
		}
		if end == 0 {
			if i == 0 {
				return version{}, false
			}
			// A dot not followed by a number starts a tail such as ".rc1".
			rest = "." + rest
			break
		}
		n, err := strconv.Atoi(rest[:end])
		if err != nil {
			return version{}, false
		}
		if i < len(slots) {
			value := n
			*slots[i] = &value
		}
		v.components++
		rest = rest[end:]
	}
	if v.major == nil {
		return version{}, false
	}
	if rest != "" && !validTail(rest) {
		return version{}, false
	}
	return v, true
}

// validTail reports whether the text after the numeric components looks like a
// pre-release, build or image-variant suffix rather than a stray word.
func validTail(tail string) bool {
	c := tail[0]
	switch {
	case c == '-' || c == '+' || c == '_' || c == '.':
		return true
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z':
		return true
	}
	return false
}

// isRangeLike reports whether s uses comparator syntax rather than naming one
// version. A leading "=" or "==" before a single version is an exact pin.
func isRangeLike(s string) bool {
	if s == "" {
		return false
	}
	if strings.Contains(s, "||") || hyphenRangePattern.MatchString(s) {
		return true
	}
	switch s[0] {
	case '<', '>', '~', '^':
		return true
	}
	if strings.ContainsAny(s, " ,") {
		return strings.ContainsAny(s, "<>~^=")
	}
	return false
}

// parseRange classifies a comparator expression. ok is false when no bound
// carries a number, so the caller reports the text as an alias.
func parseRange(s string) (Spec, bool) {
	spec := Spec{Kind: SpecKindRange}
	if m := hyphenRangePattern.FindStringSubmatch(s); m != nil {
		lo, loOK := parseVersion(m[1])
		hi, hiOK := parseVersion(m[2])
		if !loOK || !hiOK {
			return Spec{}, false
		}
		setLower(&spec, m[1], lo, true)
		setUpper(&spec, m[2], hi, true)
		return spec, true
	}
	clauses := strings.Split(s, "||")
	if len(clauses) > 1 {
		first := Spec{Kind: SpecKindRange}
		applyClause(&first, clauses[0])
		if first.RangeLower == nil && first.RangeUpper == nil {
			return Spec{}, false
		}
		whole := s
		out := Spec{Kind: SpecKindRange, RangeLower: &whole}
		// A disjunction keeps only the first clause's bound majors; the
		// whole spec stays in RangeLower, so readers must not treat the
		// majors as the union's bounds.
		if first.RangeLowerMajor != nil {
			out.RangeLowerMajor = first.RangeLowerMajor
			out.RangeLowerInclusive = first.RangeLowerInclusive
		}
		return out, true
	}
	applyClause(&spec, s)
	if spec.RangeLower == nil && spec.RangeUpper == nil {
		return Spec{}, false
	}
	return spec, true
}

// applyClause fills the first lower and first upper bound found in one
// conjunctive clause such as ">=18 <21" or ">=3.9,<4".
func applyClause(spec *Spec, clause string) {
	joined := operatorSpacePattern.ReplaceAllString(strings.TrimSpace(clause), "$1")
	tokens := strings.FieldsFunc(joined, func(r rune) bool { return r == ' ' || r == ',' || r == '\t' })
	for _, tok := range tokens {
		op, rest := splitOperator(tok)
		v, ok := parseVersion(rest)
		if !ok {
			continue
		}
		switch op {
		case ">=", "":
			setLower(spec, tok, v, true)
		case ">":
			setLower(spec, tok, v, false)
		case "<":
			setUpper(spec, tok, v, false)
		case "<=":
			setUpper(spec, tok, v, true)
		case "=", "==":
			setLower(spec, tok, v, true)
			setUpper(spec, tok, v, true)
		case "^":
			setLower(spec, tok, v, true)
			major := *v.major
			if major > 0 {
				setUpperMajor(spec, tok, major+1, false)
			} else {
				setUpperMajor(spec, tok, major, true)
			}
		case "~", "~>", "~=":
			setLower(spec, tok, v, true)
			major := *v.major
			// npm "~" bumps the major only for a bare major; "~>" and "~="
			// bump it for one or two components.
			bumpsMajor := v.components <= 1 || (op != "~" && v.components == 2)
			if bumpsMajor {
				setUpperMajor(spec, tok, major+1, false)
			} else {
				setUpperMajor(spec, tok, major, true)
			}
		}
	}
}

func splitOperator(tok string) (string, string) {
	for _, op := range []string{"<=", ">=", "~=", "~>", "==", "<", ">", "=", "^", "~"} {
		if strings.HasPrefix(tok, op) {
			return op, tok[len(op):]
		}
	}
	return "", tok
}

func setLower(spec *Spec, raw string, v version, inclusive bool) {
	if spec.RangeLower != nil {
		return
	}
	text := raw
	major := *v.major
	inc := inclusive
	spec.RangeLower, spec.RangeLowerMajor, spec.RangeLowerInclusive = &text, &major, &inc
}

func setUpper(spec *Spec, raw string, v version, inclusive bool) {
	setUpperMajor(spec, raw, *v.major, inclusive)
}

func setUpperMajor(spec *Spec, raw string, major int, inclusive bool) {
	if spec.RangeUpper != nil {
		return
	}
	text := raw
	inc := inclusive
	spec.RangeUpper, spec.RangeUpperMajor, spec.RangeUpperInclusive = &text, &major, &inc
}
