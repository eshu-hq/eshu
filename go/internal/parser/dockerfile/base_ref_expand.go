// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package dockerfile

import "strings"

// The fixed markers base_ref_unresolved_args carries when an unresolved FROM
// reference has no variable name to list. Each holds a character a variable
// name cannot, so a marker never collides with a name, and none repeats text
// from the Dockerfile: a reference can be 4096 bytes of repository-controlled
// text, and the list is documented as names.
const (
	// unresolvedCommand marks a "$(...)" command substitution.
	unresolvedCommand = "$("
	// unresolvedMalformed marks "${" with no identifier or no closing brace.
	unresolvedMalformed = "${"
	// unresolvedBare marks a "$" that starts neither a name nor a brace.
	unresolvedBare = "$"
	// unresolvedOverBound marks a reference rejected for length or nesting
	// depth before any of it was read.
	unresolvedOverBound = "<over-bound>"
)

type expansion struct {
	out string
	// truncated is set when the expansion would have produced more than
	// maxExpandedLen bytes. It stops at the first write that does not fit, so
	// out then holds only the part that did.
	truncated bool
	// unresolved lists the variable names whose value is not known, plus the
	// fixed markers above for constructs that have no name.
	unresolved []string
	referenced []string
}

type varState int

const (
	varKnown   varState = iota // value is known from a global ARG default
	varUnset                   // not declared: Dockerfile treats it as empty
	varUnknown                 // declared or predefined but supplied at build time
)

func (r *baseRefResolver) lookup(name string) (string, varState) {
	if global, ok := r.globals[name]; ok {
		if global.known {
			return global.value, varKnown
		}
		return "", varUnknown
	}
	if predefinedBuildArgs[name] {
		return "", varUnknown
	}
	return "", varUnset
}

// expand substitutes ${VAR}, $VAR, ${VAR:-word}, ${VAR-word}, ${VAR:+word} and
// ${VAR+word}. Every other form (pattern, replace or required expressions,
// command substitution, an unterminated brace) is reported as unresolved.
func (r *baseRefResolver) expand(s string) expansion {
	if exceedsExpansionBounds(s) {
		return expansion{out: s, truncated: true}
	}
	return r.expandWithin(s)
}

// exceedsExpansionBounds reports whether s is too long, or nests "${" too
// deeply, to expand in bounded time. It is one linear pass, so a hostile line
// is rejected for the price of reading it once.
func exceedsExpansionBounds(s string) bool {
	if len(s) > maxReferenceLen {
		return true
	}
	depth := 0
	for i := 0; i+1 < len(s); i++ {
		switch {
		case s[i] == '$' && s[i+1] == '{':
			depth++
			if depth > maxBraceDepth {
				return true
			}
			i++
		case s[i] == '}' && depth > 0:
			depth--
		}
	}
	return false
}

// expander holds the state of one expansion. Every nesting level writes into
// the one output buffer, so no level copies the result of an inner level and
// the bytes written never exceed maxExpandedLen however deep the references
// nest. A write that does not fit sets truncated and ends the expansion.
type expander struct {
	r      *baseRefResolver
	out    strings.Builder
	result expansion
}

// expandWithin expands text that already passed exceedsExpansionBounds.
func (r *baseRefResolver) expandWithin(s string) expansion {
	e := expander{r: r}
	e.scan(s)
	e.result.out = e.out.String()
	return e.result
}

// write appends s unless that would pass maxExpandedLen. The length is checked
// before the write, so an over-cap value is never copied.
func (e *expander) write(s string) {
	if e.result.truncated {
		return
	}
	if e.out.Len()+len(s) > maxExpandedLen {
		e.result.truncated = true
		return
	}
	e.out.WriteString(s)
}

// scan expands s into the shared output. Nested words come back through scan,
// not expandWithin, so they reuse the buffer.
func (e *expander) scan(s string) {
	for i := 0; i < len(s) && !e.result.truncated; i++ {
		if s[i] != '$' {
			e.write(s[i : i+1])
			continue
		}
		i += e.variable(s[i:]) - 1
	}
}

// unresolve records a name or marker as unresolved.
func (e *expander) unresolve(nameOrMarker string) {
	e.result.unresolved = append(e.result.unresolved, nameOrMarker)
}

// variable handles the "$..." construct at the start of s and returns the
// number of bytes it consumed.
func (e *expander) variable(s string) int {
	if len(s) > 1 && s[1] == '{' {
		end := matchingBrace(s)
		if end < 0 {
			e.unresolve(unresolvedMalformed)
			e.write(s)
			return len(s)
		}
		e.braced(s[:end+1], s[2:end])
		return end + 1
	}
	name := leadingName(s[1:])
	if name != "" {
		e.named(name, "", "", s[:1+len(name)])
		return 1 + len(name)
	}
	text, marker := "$", unresolvedBare
	if len(s) > 1 && s[1] == '(' {
		marker = unresolvedCommand
		if close := strings.IndexByte(s, ')'); close >= 0 {
			text = s[:close+1]
		} else {
			text = s
		}
	}
	e.unresolve(marker)
	e.write(text)
	return len(text)
}

func (e *expander) braced(whole, body string) {
	name := leadingName(body)
	rest := body[len(name):]
	if name == "" {
		e.unresolve(unresolvedMalformed)
		e.write(whole)
		return
	}
	if rest == "" {
		e.named(name, "", "", whole)
		return
	}
	for _, op := range []string{":-", ":+", "-", "+"} {
		if strings.HasPrefix(rest, op) {
			e.named(name, op, rest[len(op):], whole)
			return
		}
	}
	// An operator this parser does not evaluate (pattern, replace, required).
	// The variable is still read, so list its name, never the expression text,
	// and let its declared default surface as evidence.
	e.result.referenced = append(e.result.referenced, name)
	e.unresolve(name)
	e.write(whole)
}

func (e *expander) named(name, op, word, whole string) {
	e.result.referenced = append(e.result.referenced, name)
	value, state := e.r.lookup(name)
	if state == varUnknown {
		e.unresolve(name)
		e.write(whole)
		return
	}
	set := state == varKnown
	nonEmpty := set && value != ""
	switch op {
	case "":
		if !set {
			e.unresolve(name)
			e.write(whole)
			return
		}
		e.write(value)
	case ":-":
		if nonEmpty {
			e.write(value)
		} else {
			e.scan(word)
		}
	case "-":
		if set {
			e.write(value)
		} else {
			e.scan(word)
		}
	case ":+":
		if nonEmpty {
			e.scan(word)
		}
	case "+":
		if set {
			e.scan(word)
		}
	}
}

// matchingBrace returns the index of the "}" closing the "${" at s[0:2], or -1.
func matchingBrace(s string) int {
	depth := 0
	for i := 0; i < len(s); i++ {
		switch {
		case s[i] == '$' && i+1 < len(s) && s[i+1] == '{':
			depth++
			i++
		case s[i] == '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// leadingName returns the identifier at the start of s, or "".
func leadingName(s string) string {
	end := 0
	for end < len(s) {
		c := s[end]
		isLetter := c == '_' || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')
		if !isLetter && (end == 0 || c < '0' || c > '9') {
			break
		}
		end++
	}
	return s[:end]
}
