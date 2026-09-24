// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package statement

import (
	"strings"
	"testing"
)

// byteSource turns fuzzer bytes into choices, so the fuzzer mutates the shape
// of a generated literal and not only the digits inside a fixed skeleton.
type byteSource struct {
	data []byte
	pos  int
}

func (s *byteSource) next() byte {
	if s.pos >= len(s.data) {
		return 0
	}
	b := s.data[s.pos]
	s.pos++
	return b
}

func (s *byteSource) pick(chars string) string { return string(chars[int(s.next())%len(chars)]) }

func (s *byteSource) count(limit int) int { return int(s.next()) % (limit + 1) }

const (
	digitChars    = "0123456789"
	nonZeroDigits = "123456789"
	// partLetters are the ASCII members of Neo4j 5's PART_LETTER (Cypher5Lexer.g4):
	// letters, digits, underscore and dollar, which trail a number token.
	partLetters   = "0123456789abcdefABCDEFxXoOzZ_$"
	identStarters = "abcdefABCDEFxXoOzZ_"
)

// separatedDigits emits digits, each optionally preceded by the Neo4j 5
// INTEGER_PART underscore.
func (s *byteSource) separatedDigits(first string, extra int) string {
	var b strings.Builder
	b.WriteString(first)
	for range s.count(extra) {
		if s.next()&1 == 1 {
			b.WriteByte('_')
		}
		b.WriteString(s.pick(digitChars))
	}
	return b.String()
}

func (s *byteSource) tail() string {
	var b strings.Builder
	for range s.count(5) {
		b.WriteString(s.pick(partLetters))
	}
	return b.String()
}

func (s *byteSource) identifier() string { return s.pick(identStarters) + s.tail() }

// exponent follows DECIMAL_EXPONENT: [eE] ([+-])? (INTEGER_PART)+ (PART_LETTER)*.
func (s *byteSource) exponent() string {
	var b strings.Builder
	b.WriteString(s.pick("eE"))
	switch s.next() % 3 {
	case 1:
		b.WriteByte('+')
	case 2:
		b.WriteByte('-')
	}
	for range 1 + s.count(3) {
		if s.next()&1 == 1 {
			b.WriteByte('_')
		}
		b.WriteString(s.pick(digitChars))
	}
	b.WriteString(s.tail())
	return b.String()
}

func (s *byteSource) optionalExponentAndIdentifier() string {
	var b strings.Builder
	if s.next()&1 == 1 {
		b.WriteString(s.exponent())
	}
	if s.next()&1 == 1 {
		b.WriteString(s.identifier())
	}
	return b.String()
}

// neo4jNumber builds one numeric literal that Neo4j 5's lexer accepts as a
// single token, following Cypher5Lexer.g4: DECIMAL_DOUBLE (three shapes),
// UNSIGNED_DECIMAL_INTEGER, UNSIGNED_HEX_INTEGER and UNSIGNED_OCTAL_INTEGER.
func (s *byteSource) neo4jNumber() string {
	var literal string
	switch s.next() % 6 {
	case 0:
		literal = s.separatedDigits(s.pick(nonZeroDigits), 6) + s.tail()
	case 1:
		literal = "0" + s.pick("xX") + s.tail()
	case 2:
		literal = "0" + s.pick("o") + s.tail()
	case 3:
		literal = s.separatedDigits(s.pick(digitChars), 6) + "." + s.separatedDigits(s.pick(digitChars), 6) +
			s.optionalExponentAndIdentifier()
	case 4:
		literal = "." + s.separatedDigits(s.pick(digitChars), 6) + s.optionalExponentAndIdentifier()
	default:
		literal = s.separatedDigits(s.pick(digitChars), 6) + s.exponent()
		if s.next()&1 == 1 {
			literal += s.identifier()
		}
	}
	if s.next()&7 == 0 {
		literal = "-" + literal
	}
	return literal
}

// FuzzRedactNumberStructure searches for numeric leaks by shape. It generates a
// literal from the Neo4j 5 lexer's number grammar, places it in value position
// (after `= `, behind ASCII or Unicode whitespace), and requires the whole
// literal to collapse to one placeholder. Any input the lexer reads as one
// number token that survives even partly, digits after a sign, a separator or a
// letter, fails. FuzzRedactLeaksNoPlantedSecret fixes the skeletons; this one
// mutates them.
func FuzzRedactNumberStructure(f *testing.F) {
	f.Add([]byte{0, 0, 3, 1, 2, 3, 4, 5, 6})
	f.Add([]byte{5, 1, 2, 3, 0, 1, 2, 3, 4})
	f.Add([]byte{3, 4, 1, 2, 3, 5, 4, 3, 2, 1, 1, 1, 1, 2, 3, 2, 0, 1, 2, 3, 1, 0, 1, 0})
	f.Add([]byte{7, 1, 1, 2, 3, 1, 0, 1, 1, 2, 2, 5, 4, 3})
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) == 0 {
			t.Skip()
		}
		spaces := []string{" ", " ", "　", "\t", " ", "\x1f", " ", " ", " "}
		src := &byteSource{data: data[1:]}
		gap := spaces[int(data[0])%len(spaces)]
		literal := src.neo4jNumber()
		statement := "MATCH (p) WHERE p.q =" + gap + literal + gap + "RETURN p"
		const want = "MATCH (p) WHERE p.q = <REDACTED> RETURN p"
		if got := Redact(statement); got != want {
			t.Fatalf("Redact(%q) = %q, want %q", statement, got, want)
		}
	})
}
