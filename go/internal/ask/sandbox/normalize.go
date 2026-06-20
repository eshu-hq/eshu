package sandbox

// normalize.go — adversary-resistant query normalizer for the ask/sandbox package.
//
// Design: single left-to-right byte scanner implemented as a state machine.
// No regex. No recursion. No look-ahead beyond what is already buffered in the
// current scan position.
//
// The scanner replaces the CONTENT (not the delimiters themselves) of every
// comment and every string/quoted-literal with space characters so that a
// downstream keyword scan over the returned masked string only sees CODE tokens.
// Delimiter characters themselves are also replaced with spaces so that e.g.
// `--`, `/*`, `*/`, `'`, `$$` do not appear in the masked output as stray
// punctuation that could confuse a subsequent token scanner.
//
// Statement counting: every `;` encountered while in CODE state separates
// statements. We count non-empty statements: a segment is non-empty if it
// contains at least one non-whitespace byte outside comments/strings.
// A trailing `;` followed only by whitespace does not add a second statement.
//
// Supported per dialect:
//
//   SQL (DialectSQL):
//     - `--` line comment to end-of-line
//     - `/* … */` block comment (not nested; unterminated → err)
//     - `'…'` single-quoted string with `''` doubling escape
//     - `$tag$…$tag$` dollar-quoted string (tag may be empty); unterminated → err
//     - `"…"` double-quoted identifier (masked; cannot contain code keywords)
//
//   Cypher (DialectCypher):
//     - `//` line comment to end-of-line
//     - `--` line comment (also stripped for defense-in-depth)
//     - `/* … */` block comment (not nested; unterminated → err)
//     - `'…'` single-quoted string with `''` doubling escape; unterminated → err
//     - `` `…` `` backtick-quoted identifier (masked; unterminated → err)
//
// Dollar-quoting is NOT applied in Cypher because `$name` is a Cypher parameter
// prefix and `$$` has no string-delimiter meaning in Cypher.

import "strings"

// normalized is the output of the normalize scanner.
type normalized struct {
	// masked is the input query with every comment content and every
	// string/quoted-literal content (and their delimiters) replaced by space
	// characters. Code tokens outside comments and literals are preserved
	// verbatim. Downstream keyword scans over masked never see keywords that
	// were smuggled inside comments or string literals.
	masked string

	// statementCount is the number of non-empty top-level statements separated
	// by `;` that occur outside string/comment context. A trailing `;` followed
	// only by whitespace does not produce an additional empty statement.
	statementCount int

	// err is non-empty when the scanner encountered an unterminated string
	// literal or unterminated block comment. Possible values:
	//   "unterminated string literal"
	//   "unterminated comment"
	err string
}

// scannerState represents the scanner's lexing state.
type scannerState int

const (
	stateCode         scannerState = iota // normal code
	stateLineComment                      // inside a -- or // comment
	stateBlockComment                     // inside /* … */
	stateSingleQuote                      // inside '…'
	stateDollarQuote                      // inside $tag$…$tag$
	stateDoubleQuote                      // inside "…"  (SQL identifier)
	stateBacktick                         // inside `…` (Cypher identifier)
)

// normalize scans query left-to-right and returns a normalized result with all
// comment and string-literal content replaced by spaces. It never panics.
//
// Control-character precondition: normalize rejects any query that contains a
// byte < 0x20 other than TAB (0x09), LF (0x0A), or CR (0x0D), and also rejects
// DEL (0x7F). Such bytes can split identifier tokens (e.g. `D\x00ELETE`
// becomes the tokens `D` and `ELETE`, evading the whole-word denylist) without
// being part of any valid SQL or Cypher literal. Both the Cypher and SQL
// validators inherit this rejection because they both call normalize first.
func normalize(dialect Dialect, query string) normalized {
	if query == "" {
		return normalized{}
	}

	// Reject hostile control bytes before any scanning. A byte below 0x20 that
	// is not TAB/LF/CR, or the DEL byte (0x7F), can split keyword tokens
	// (e.g. D\x00ELETE → tokens "D" and "ELETE") and evade the whole-word
	// denylist. There is no valid SQL or Cypher literal that requires these bytes.
	for i := 0; i < len(query); i++ {
		b := query[i]
		if b == 0x7F || (b < 0x20 && b != 0x09 && b != 0x0A && b != 0x0D) {
			return normalized{err: "control character not permitted"}
		}
	}

	buf := []byte(query)
	masked := make([]byte, len(buf))
	copy(masked, buf) // start as copy; we will overwrite comment/literal regions

	state := stateCode
	i := 0
	n := len(buf)

	// dollarTag accumulates the current $tag$ sequence while scanning it.
	var dollarTag string

	// Statement counting helpers.
	// segmentHasCode tracks whether the current statement segment contains
	// at least one non-whitespace, non-comment, non-literal byte.
	stmtCount := 0
	segmentHasCode := false

	// maskRange fills masked[start:end] with spaces.
	maskRange := func(start, end int) {
		for k := start; k < end && k < len(masked); k++ {
			masked[k] = ' '
		}
	}

	// peek returns the byte at position j, or 0 if out of bounds.
	peek := func(j int) byte {
		if j >= 0 && j < n {
			return buf[j]
		}
		return 0
	}

	for i < n {
		ch := buf[i]

		switch state {
		// ── CODE state ───────────────────────────────────────────────────────
		case stateCode:
			switch {
			// Block comment /* … */
			case ch == '/' && peek(i+1) == '*':
				maskRange(i, i+2)
				i += 2
				state = stateBlockComment

			// SQL line comment --
			case ch == '-' && peek(i+1) == '-':
				maskRange(i, i+2)
				i += 2
				state = stateLineComment

			// Cypher line comment //
			case ch == '/' && peek(i+1) == '/' && dialect == DialectCypher:
				maskRange(i, i+2)
				i += 2
				state = stateLineComment

			// SQL double-quoted identifier
			case ch == '"' && dialect == DialectSQL:
				masked[i] = ' '
				i++
				state = stateDoubleQuote

			// Cypher backtick-quoted identifier
			case ch == '`' && dialect == DialectCypher:
				masked[i] = ' '
				i++
				state = stateBacktick

			// SQL dollar-quoted string  $tag$ … $tag$
			// Only in SQL; in Cypher $ is a parameter prefix.
			//
			// Postgres dollar-quote tag rules (§4.1.2.4):
			//   - empty tag:      $$  (char immediately after first $ is another $)
			//   - identifier tag: first tag char is letter or '_'; subsequent chars
			//     are letters, digits, or '_'.
			//
			// A '$' followed by a digit is a positional PARAMETER ($1, $2, …), not
			// a dollar-quote opener.  Treating it as a quote delimiter would mask
			// everything up to the next $N$ sequence, hiding real stacked statements
			// and dangerous keywords.  Such a '$' must be treated as ordinary code.
			case ch == '$' && dialect == DialectSQL:
				// Scan forward to find the closing $. The tag is [A-Za-z0-9_]*.
				j := i + 1
				for j < n && isDollarTagChar(buf[j]) {
					j++
				}
				if j < n && buf[j] == '$' {
					tag := string(buf[i+1 : j]) // may be empty for $$
					// Reject digit-leading tags: they are positional parameters ($1,
					// $2, …), not dollar-quote delimiters.
					if len(tag) > 0 && !isDollarTagIdentStart(tag[0]) {
						// Treat as ordinary code.
						segmentHasCode = true
						i++
					} else {
						// Valid dollar-quote opener (empty tag or identifier-led tag).
						dollarTag = tag
						maskRange(i, j+1)
						i = j + 1
						state = stateDollarQuote
					}
				} else {
					// Not a dollar-quote: keep as code (parameter like $1 or $name).
					segmentHasCode = true
					i++
				}

			// Single-quoted string
			case ch == '\'':
				masked[i] = ' '
				i++
				state = stateSingleQuote

			// Statement separator
			case ch == ';':
				masked[i] = ';' // keep ; visible in masked for debug; it is code
				i++
				if segmentHasCode {
					stmtCount++
				}
				segmentHasCode = false

			// Ordinary code byte
			default:
				if !isWhitespace(ch) {
					segmentHasCode = true
				}
				i++
			}

		// ── LINE COMMENT state ────────────────────────────────────────────────
		case stateLineComment:
			if ch == '\n' || ch == '\r' {
				// End of line comment: LF or CR terminates the comment.
				// PostgreSQL treats CR as a newline, so "--\r FROM pg_sleep(10)"
				// would expose "FROM pg_sleep(10)" as code unless CR also ends
				// the comment state. The newline byte itself stays (it's whitespace).
				state = stateCode
				i++
			} else {
				masked[i] = ' '
				i++
			}

		// ── BLOCK COMMENT state ───────────────────────────────────────────────
		case stateBlockComment:
			if ch == '*' && peek(i+1) == '/' {
				maskRange(i, i+2)
				i += 2
				state = stateCode
			} else {
				masked[i] = ' '
				i++
			}

		// ── SINGLE-QUOTE string state ─────────────────────────────────────────
		case stateSingleQuote:
			if ch == '\'' {
				// Check for '' doubling escape.
				if peek(i+1) == '\'' {
					// Two consecutive quotes inside a string = escaped quote.
					masked[i] = ' '
					masked[i+1] = ' '
					i += 2
				} else {
					// Closing quote.
					masked[i] = ' '
					i++
					state = stateCode
				}
			} else {
				masked[i] = ' '
				i++
			}

		// ── DOLLAR-QUOTE string state ─────────────────────────────────────────
		case stateDollarQuote:
			// Look for the matching closing $tag$.
			if ch == '$' {
				closeTag := "$" + dollarTag + "$"
				if strings.HasPrefix(string(buf[i:]), closeTag) {
					maskRange(i, i+len(closeTag))
					i += len(closeTag)
					dollarTag = ""
					state = stateCode
				} else {
					masked[i] = ' '
					i++
				}
			} else {
				masked[i] = ' '
				i++
			}

		// ── DOUBLE-QUOTE identifier state (SQL) ───────────────────────────────
		case stateDoubleQuote:
			if ch == '"' {
				// SQL double-quoted identifiers use "" to embed a literal quote.
				if peek(i+1) == '"' {
					masked[i] = ' '
					masked[i+1] = ' '
					i += 2
				} else {
					masked[i] = ' '
					i++
					state = stateCode
				}
			} else {
				masked[i] = ' '
				i++
			}

		// ── BACKTICK identifier state (Cypher) ───────────────────────────────
		case stateBacktick:
			if ch == '`' {
				masked[i] = ' '
				i++
				state = stateCode
			} else {
				masked[i] = ' '
				i++
			}
		}
	}

	// Check for unterminated constructs.
	var errStr string
	switch state {
	case stateBlockComment:
		errStr = "unterminated comment"
	case stateSingleQuote, stateDollarQuote:
		errStr = "unterminated string literal"
	case stateBacktick:
		errStr = "unterminated string literal"
	case stateDoubleQuote:
		errStr = "unterminated string literal"
	}

	// Count the final (possibly only) statement if it has code and no error.
	// On error we still return what we have for diagnostics; statementCount is
	// best-effort.
	if segmentHasCode {
		stmtCount++
	}

	return normalized{
		masked:         string(masked),
		statementCount: stmtCount,
		err:            errStr,
	}
}

// isDollarTagChar reports whether b is a valid dollar-quote tag character.
// PostgreSQL dollar-quote tags consist of letters (any Unicode letter), digits,
// and underscores. We conservatively restrict to ASCII here to avoid
// complexity with multi-byte runes in a byte-oriented scanner.
func isDollarTagChar(b byte) bool {
	return (b >= 'a' && b <= 'z') ||
		(b >= 'A' && b <= 'Z') ||
		(b >= '0' && b <= '9') ||
		b == '_'
}

// isDollarTagIdentStart reports whether b is a valid first character for a
// dollar-quote tag. Postgres requires the tag to be a SQL identifier: it must
// begin with a letter (a–z, A–Z) or underscore, not a digit. A digit-leading
// sequence such as $1$ is a positional parameter, not a dollar-quote.
func isDollarTagIdentStart(b byte) bool {
	return (b >= 'a' && b <= 'z') ||
		(b >= 'A' && b <= 'Z') ||
		b == '_'
}

// isWhitespace reports whether b is an ASCII whitespace byte.
func isWhitespace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r' || b == '\f' || b == '\v'
}
