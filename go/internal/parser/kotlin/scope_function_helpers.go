package kotlin

import "strings"

// kotlinStripReceiverPreservingScopeFunctions removes trailing receiver-
// preserving scope-function calls (`.also { ... }` and `.apply { ... }`) from a
// receiver expression so type inference sees the underlying receiver value.
// `also`/`apply` return their receiver, so dropping them does not change the
// inferred type. The scan repeats until no further such call is found.
func kotlinStripReceiverPreservingScopeFunctions(expression string) string {
	expression = strings.TrimSpace(expression)
	for {
		next := stripOneScopeFunction(expression)
		if next == expression {
			return expression
		}
		expression = strings.TrimSpace(next)
	}
}

// stripOneScopeFunction removes the first `.also { ... }` or `.apply { ... }`
// segment found in expression, returning the original string when none match.
func stripOneScopeFunction(expression string) string {
	for _, keyword := range []string{".also", ".apply"} {
		idx := strings.Index(expression, keyword)
		if idx < 0 {
			continue
		}
		rest := expression[idx+len(keyword):]
		braceStart := indexOfNonSpaceBrace(rest)
		if braceStart < 0 {
			continue
		}
		braceEnd := matchingBrace(rest, braceStart)
		if braceEnd < 0 {
			continue
		}
		return expression[:idx] + rest[braceEnd+1:]
	}
	return expression
}

// indexOfNonSpaceBrace returns the index of the first `{` in s when only
// whitespace precedes it, or -1 otherwise.
func indexOfNonSpaceBrace(s string) int {
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case ' ', '\t', '\n', '\r':
			continue
		case '{':
			return i
		default:
			return -1
		}
	}
	return -1
}

// matchingBrace returns the index of the `}` that closes the `{` at openIndex,
// or -1 when unbalanced.
func matchingBrace(s string, openIndex int) int {
	depth := 0
	for i := openIndex; i < len(s); i++ {
		switch s[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}
