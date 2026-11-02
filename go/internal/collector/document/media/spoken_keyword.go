// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package media

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// phraseRule is one folded keyword and where it must sit in a word. A phrase
// with neither flag matches as a substring, the way the original keyword list
// did, so "password" still finds "passwords" and "mypassword". Over-redaction
// there is accepted policy. before and after require a non-letter (or the text
// edge) on that side of the phrase, which keeps short or ambiguous words from
// firing inside longer ones.
type phraseRule struct {
	phrase string
	before bool
	after  bool
}

// spokenKeywordRules are matched against the folded text of a segment, where
// every separator run is one space and every letter is lower case.
//
// The boundary choice for each rule is deliberate:
//
//   - Substring (no flags): password, passphrase, passcode, passwd, pwd,
//     pincode, credential, api key, private key, and the *_marker test
//     keywords. None is a fragment of an ordinary English word, and a hit
//     inside a longer word ("credentials", "passwords") is the same secret
//     vocabulary.
//   - Start-bounded: the split spellings "pass word", "pass phrase", and
//     "pass code". Unbounded they would fire inside "compass word" or "bypass
//     wording". The joined spellings need no bound.
//   - Whole word: secret, secrets, token, tokens, pin, and the spelled-out
//     p i n and p w d. A substring "token" would redact "tokenize" and
//     "tokenizer", and "pin" would redact "spinning", "pinned", "mapping", and
//     "shipping". The cost is that "tokenized" and "pins" stay in clear text,
//     which is the intended trade: whisper writes a spoken secret label as its
//     own word. "secret" fails closed on purpose: "a secret garden" redacts
//     two following segments, which is visible in section metadata, while a
//     persisted client secret is not recoverable.
var spokenKeywordRules = []phraseRule{
	{phrase: "password"},
	{phrase: "passphrase"},
	{phrase: "passcode"},
	{phrase: "passwd"},
	{phrase: "pwd"},
	{phrase: "pincode"},
	{phrase: "credential"},
	{phrase: "api key"},
	{phrase: "apikey"},
	{phrase: "private key"},
	{phrase: "secret marker"},
	{phrase: "token marker"},
	{phrase: "pass word", before: true},
	{phrase: "pass phrase", before: true},
	{phrase: "pass code", before: true},
	{phrase: "a p i key", before: true},
	{phrase: "secret", before: true, after: true},
	{phrase: "secrets", before: true, after: true},
	{phrase: "token", before: true, after: true},
	{phrase: "tokens", before: true, after: true},
	{phrase: "pin", before: true, after: true},
	{phrase: "p i n", before: true, after: true},
	{phrase: "p w d", before: true, after: true},
}

// homoglyphs folds the lower-case Cyrillic, Greek, and Latin look-alikes of
// the Latin letters that the keywords above use (a c d e h i k l m n o p r s t
// v w y). It is a bounded set, not the full Unicode confusables table: a
// look-alike for a letter no keyword uses is not worth a rule, and a letter
// outside this set is a documented limit. foldSpoken lower-cases a rune before
// it looks here, so a capital look-alike folds through its lower-case form,
// except the two in capitalHomoglyphs.
var homoglyphs = map[rune]rune{
	// Cyrillic.
	'\u0430': 'a', '\u0435': 'e', '\u043e': 'o', '\u0440': 'p', '\u0441': 'c',
	'\u0443': 'y', '\u0456': 'i', '\u0455': 's', '\u04bb': 'h', '\u0501': 'd',
	'\u051d': 'w', '\u043a': 'k', '\u0475': 'v', '\u0442': 't', '\u043c': 'm',
	'\u043f': 'n', '\u04cf': 'l',
	// Greek.
	'\u03bf': 'o', '\u03c1': 'p', '\u03bd': 'v', '\u03b1': 'a', '\u03b5': 'e',
	'\u03b9': 'i', '\u03ba': 'k', '\u03c4': 't', '\u03b7': 'n',
	// Latin.
	'\u0131': 'i',
}

// capitalHomoglyphs holds the capitals whose lower-case form looks like a
// different letter: Greek capital nu looks like N, but small nu looks like v;
// Greek capital upsilon looks like Y.
var capitalHomoglyphs = map[rune]rune{
	'\u039d': 'n', '\u03a5': 'y',
}

// containsSpokenKeyword reports whether the folded form of text holds a spoken
// secret keyword. Folding turns "_" into a space, so the rules also cover every
// legacy keyword ("api_key" folds to "api key"); sanitize_keyword_test.go pins
// that.
func containsSpokenKeyword(text string) bool {
	return foldedHasKeyword(foldSpoken(text))
}

// foldedHasKeyword reports whether already-folded text matches a keyword rule.
func foldedHasKeyword(folded string) bool {
	for _, rule := range spokenKeywordRules {
		if containsPhrase(folded, rule) {
			return true
		}
	}
	return false
}

// joinedHasKeyword reports whether a keyword straddles two consecutive
// segments, such as "The API" then "key is". Both arguments are folded. A
// caller passes only a pair where neither segment holds a keyword alone, so a
// hit is a keyword that needed both halves. The join is one space, which is
// what the fold makes of any separator, so "pass" then "word" reads as
// "pass word". The work is linear in the two lengths.
func joinedHasKeyword(prevFolded, curFolded string) bool {
	if prevFolded == "" || curFolded == "" {
		return false
	}
	return foldedHasKeyword(prevFolded + " " + curFolded)
}

// foldSpoken returns text in the form the spoken keyword rules match on. It
// is deterministic and linear in the length of text. It
//
//   - decomposes compatibility forms (fullwidth letters, ligatures, no-break
//     and ideographic spaces) and drops the combining marks that remain, so
//     "pássword" folds to "password";
//   - drops format characters (soft hyphen, zero-width space and joiners, word
//     joiner, byte order mark, bidirectional marks), which are invisible in a
//     transcript but split a word for a byte-wise match;
//   - lower-cases and maps the homoglyph set above to Latin;
//   - turns every run of white space, dash punctuation, underscore, dot, and
//     comma into one space, so "pass-word", "pass_word", and "pass.word" fold
//     like "pass word", "P.I.N." and "P, I, N" fold to "p i n", and "A.P.I.
//     key" folds to "a p i key".
func foldSpoken(text string) string {
	var out strings.Builder
	out.Grow(len(text))
	pendingSpace := false
	for _, r := range norm.NFKD.String(text) {
		switch {
		case unicode.Is(unicode.Mn, r), unicode.Is(unicode.Cf, r):
			continue
		case unicode.IsSpace(r), r == '_', r == '.', r == ',', unicode.Is(unicode.Pd, r):
			pendingSpace = out.Len() > 0
			continue
		}
		if latin, ok := capitalHomoglyphs[r]; ok {
			r = latin
		} else {
			r = unicode.ToLower(r)
			if latin, ok := homoglyphs[r]; ok {
				r = latin
			}
		}
		if pendingSpace {
			out.WriteByte(' ')
			pendingSpace = false
		}
		out.WriteRune(r)
	}
	return out.String()
}

// containsPhrase reports whether folded holds rule.phrase with the boundaries
// the rule asks for. Every occurrence is tried, so a rejected one does not hide
// a later accepted one.
func containsPhrase(folded string, rule phraseRule) bool {
	for offset := 0; offset <= len(folded)-len(rule.phrase); {
		index := strings.Index(folded[offset:], rule.phrase)
		if index < 0 {
			return false
		}
		start := offset + index
		end := start + len(rule.phrase)
		if (!rule.before || !letterBefore(folded, start)) && (!rule.after || !letterAt(folded, end)) {
			return true
		}
		offset = start + 1
	}
	return false
}

func letterBefore(s string, index int) bool {
	r, size := utf8.DecodeLastRuneInString(s[:index])
	return size > 0 && unicode.IsLetter(r)
}

func letterAt(s string, index int) bool {
	r, size := utf8.DecodeRuneInString(s[index:])
	return size > 0 && unicode.IsLetter(r)
}
