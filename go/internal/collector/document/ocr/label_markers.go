// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ocr

import (
	"bytes"
	"unicode"
	"unicode/utf8"
)

// labelFamilies is the marker list for text read from a picture. It is wider
// than identityMarkers because OCR text is a label to match, not a name to
// keep, and it is matched word by word rather than as a raw substring. Every
// entry is stored folded (see foldRune), so a misread digit in the text and the
// letter in the list meet.
//
// A family picks its boundary by how safe the word is inside longer words:
//
//   - contains: the word is rare inside ordinary words, so a run of letters and
//     digits that holds it anywhere is a marker (`MyPassword`, `passwords`).
//   - endsWith: the word also starts ordinary words (`secretary`, `tokenize`),
//     so it must be a whole word, its plural, or the end of a compound
//     (`client_secret`, `accessToken`).
//   - exact: the word is a few letters long and sits inside many ordinary words
//     (`spin`, `pinned`), so it must be a whole word or its plural.
//   - phrases: neither word alone says much (`api`, `key`), so two adjacent
//     words together do (`API Key`, `private-key`, `Pass phrase`).
//
// Matching is case-insensitive. Space, hyphen, underscore, and a lower to upper
// case change all split words alike, so `API Key`, `API-Key`, `API_KEY`, and
// `apiKey` are one label. Any other punctuation ends a phrase.
var labelFamilies = struct {
	contains [][]byte
	endsWith [][]byte
	exact    [][]byte
	phrases  []markerPhrase
}{
	contains: foldedList("password", "passwd", "passphrase", "passcode", "credential", "apikey", "privatekey"),
	endsWith: foldedList("secret", "token"),
	exact:    foldedList("pin", "pwd"),
	phrases: []markerPhrase{
		{first: folded("api"), firstEndsWith: true, second: foldedList("key", "keys")},
		{first: folded("private"), firstEndsWith: true, second: foldedList("key", "keys")},
		{first: folded("access"), firstEndsWith: true, second: foldedList("key", "keys")},
		{first: folded("pass"), second: foldedList("word", "words", "phrase", "phrases", "code", "codes")},
	},
}

// markerPhrase is two adjacent words that name a credential together. The
// first word may end a compound (`MYAPI_KEY`) when firstEndsWith is set.
type markerPhrase struct {
	first         []byte
	firstEndsWith bool
	second        [][]byte
}

// foldRune lowercases r and maps the digits and symbols that OCR reads in place
// of a letter onto that letter: 0 for o, 3 for e, 4 and @ for a, 5 and $ for s.
// The letters i and l and the digit 1 are one class, because a capital I, a
// lowercase l, and a 1 look alike in most fonts and the marker list holds no
// word that depends on telling them apart. The map is exact, so it costs one
// rune lookup and finds no word that is not a marker once folded.
func foldRune(r rune) rune {
	switch r = unicode.ToLower(r); r {
	case '0':
		return 'o'
	case '3':
		return 'e'
	case '4', '@':
		return 'a'
	case '5', '$':
		return 's'
	case '1', 'l':
		return 'i'
	}
	return r
}

func folded(word string) []byte {
	out := make([]byte, 0, len(word))
	for _, r := range word {
		out = utf8.AppendRune(out, foldRune(r))
	}
	return out
}

func foldedList(words ...string) [][]byte {
	list := make([][]byte, 0, len(words))
	for _, word := range words {
		list = append(list, folded(word))
	}
	return list
}

func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

// isPhraseGap reports whether r may sit between the two words of a phrase.
func isPhraseGap(r rune) bool {
	return r == '-' || r == '_' || unicode.IsSpace(r)
}

// markerScan holds the state of one containsMarkerWord pass. The three buffers
// start on the stack and grow only for a word longer than 64 bytes.
type markerScan struct {
	piece []byte // folded word being read
	run   []byte // folded letters and digits since the last separator
	prev  []byte // folded last word
	// havePrev reports that prev holds a word. linked reports that nothing but
	// space, hyphen, or underscore sits between prev and the word being read.
	havePrev, linked, gapOK bool
}

// containsMarkerWord reports whether text holds a label from labelFamilies. It
// reads text once, allocates nothing for ordinary text, and stops at the first
// match.
func containsMarkerWord(text string) bool {
	var pieceBuf, runBuf, prevBuf [64]byte
	scan := markerScan{piece: pieceBuf[:0], run: runBuf[:0], prev: prevBuf[:0], gapOK: true}
	var prevRune rune // the last word rune read, or 0 after a separator
	for i := 0; i < len(text); {
		r, size := utf8.DecodeRuneInString(text[i:])
		if symbols := inWordSymbols(text[i:], prevRune); symbols > 0 {
			for _, symbol := range text[i : i+symbols] {
				scan.piece = utf8.AppendRune(scan.piece, foldRune(symbol))
				scan.run = utf8.AppendRune(scan.run, foldRune(symbol))
				prevRune = foldRune(symbol)
			}
			i += symbols
			continue
		}
		i += size
		if !isWordRune(r) {
			if scan.endPiece() || scan.endRun() {
				return true
			}
			if !isPhraseGap(r) {
				scan.gapOK = false
			}
			prevRune = 0
			continue
		}
		next, nextSize := utf8.DecodeRuneInString(text[i:])
		after, _ := utf8.DecodeRuneInString(text[i+nextSize:])
		if startsCamelWord(prevRune, r, next, after) && scan.endPiece() {
			return true
		}
		if len(scan.piece) == 0 {
			scan.linked = scan.havePrev && scan.gapOK
		}
		scan.piece = utf8.AppendRune(scan.piece, foldRune(r))
		scan.run = utf8.AppendRune(scan.run, foldRune(r))
		prevRune = r
	}
	return scan.endPiece() || scan.endRun()
}

// inWordSymbols returns how many bytes of text start a run of `@` or `$` that
// sits inside a word, as in `P@ssword` or `pa$$w0rd`, or 0. A symbol at the edge
// of a word (`$PIN`, `@token`) is a separator, not a letter.
func inWordSymbols(text string, prevRune rune) int {
	n := 0
	for n < len(text) && (text[n] == '@' || text[n] == '$') {
		n++
	}
	if n == 0 || prevRune == 0 || n == len(text) {
		return 0
	}
	if next, _ := utf8.DecodeRuneInString(text[n:]); !isWordRune(next) {
		return 0
	}
	return n
}

// startsCamelWord reports whether r begins a new word in a camel case name:
// `apiKey` before K, and `APIKey` before K as well. A capital followed by one
// lowercase letter and nothing more is a plural, as in `PINs` and `APIs`, not a
// new word, so the split needs two lowercase letters.
func startsCamelWord(prev rune, r rune, next rune, after rune) bool {
	if prev == 0 || !unicode.IsUpper(r) {
		return false
	}
	return unicode.IsLower(prev) || (unicode.IsUpper(prev) && unicode.IsLower(next) && unicode.IsLower(after))
}

// endPiece checks the word just read against the word-level families and the
// phrase before it, then keeps it as the previous word.
func (s *markerScan) endPiece() bool {
	if len(s.piece) == 0 {
		return false
	}
	matched := s.pieceIsMarker() || (s.linked && s.phraseEndsHere())
	s.prev = append(s.prev[:0], s.piece...)
	s.piece = s.piece[:0]
	s.havePrev = true
	s.gapOK = true
	return matched
}

func (s *markerScan) pieceIsMarker() bool {
	for _, word := range labelFamilies.endsWith {
		if endsWithWord(s.piece, word) {
			return true
		}
	}
	for _, word := range labelFamilies.exact {
		if isWord(s.piece, word) {
			return true
		}
	}
	return false
}

func (s *markerScan) phraseEndsHere() bool {
	for _, phrase := range labelFamilies.phrases {
		firstMatches := bytes.Equal(s.prev, phrase.first) || (phrase.firstEndsWith && bytes.HasSuffix(s.prev, phrase.first))
		if !firstMatches {
			continue
		}
		for _, second := range phrase.second {
			if bytes.Equal(s.piece, second) {
				return true
			}
		}
	}
	return false
}

// endRun checks the letters and digits since the last separator for the
// families that match anywhere inside a word.
func (s *markerScan) endRun() bool {
	run := s.run
	s.run = s.run[:0]
	for _, word := range labelFamilies.contains {
		if bytes.Contains(run, word) {
			return true
		}
	}
	return false
}

// isWord reports whether piece is word or its plural.
func isWord(piece []byte, word []byte) bool {
	return bytes.Equal(piece, word) ||
		(len(piece) == len(word)+1 && piece[len(piece)-1] == 's' && bytes.HasPrefix(piece, word))
}

// endsWithWord reports whether piece is word, its plural, or ends with either.
func endsWithWord(piece []byte, word []byte) bool {
	if bytes.HasSuffix(piece, word) {
		return true
	}
	return len(piece) > 0 && piece[len(piece)-1] == 's' && bytes.HasSuffix(piece[:len(piece)-1], word)
}
