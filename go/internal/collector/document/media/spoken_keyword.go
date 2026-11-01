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
//     credential, api key, private key, and the *_marker test keywords. None is
//     a fragment of an ordinary English word, and a hit inside a longer word
//     ("credentials", "passwords") is the same secret vocabulary.
//   - Start-bounded: the split spellings "pass word", "pass phrase", and
//     "pass code". Unbounded they would fire inside "compass word" or "bypass
//     wording". The joined spellings need no bound.
//   - Whole word: token, tokens, pin, and the spelled-out p i n. A substring
//     "token" would redact "tokenize" and "tokenizer", and "pin" would redact
//     "spinning", "pinned", "mapping", and "shipping". The cost is that
//     "tokenized" and "pins" stay in clear text, which is the intended trade:
//     whisper writes a spoken secret label as its own word.
var spokenKeywordRules = []phraseRule{
	{phrase: "password"},
	{phrase: "passphrase"},
	{phrase: "passcode"},
	{phrase: "passwd"},
	{phrase: "pwd"},
	{phrase: "credential"},
	{phrase: "api key"},
	{phrase: "apikey"},
	{phrase: "private key"},
	{phrase: "secret marker"},
	{phrase: "token marker"},
	{phrase: "pass word", before: true},
	{phrase: "pass phrase", before: true},
	{phrase: "pass code", before: true},
	{phrase: "token", before: true, after: true},
	{phrase: "tokens", before: true, after: true},
	{phrase: "pin", before: true, after: true},
	{phrase: "p i n", before: true, after: true},
}

// homoglyphs folds the lower-case Cyrillic, Greek, and Latin look-alikes of
// the Latin letters in the keywords above. It is a bounded set, not the full
// Unicode confusables table: a look-alike for a letter no keyword uses is not
// worth a rule, and a letter outside this set is a documented limit.
var homoglyphs = map[rune]rune{
	// Cyrillic.
	'а': 'a', 'е': 'e', 'о': 'o', 'р': 'p', 'с': 'c',
	'х': 'x', 'у': 'y', 'і': 'i', 'ј': 'j', 'ѕ': 's',
	'һ': 'h', 'ԁ': 'd', 'ԛ': 'q', 'ԝ': 'w', 'к': 'k',
	'ѵ': 'v', 'т': 't', 'н': 'h', 'м': 'm',
	// Greek.
	'ο': 'o', 'ρ': 'p', 'ν': 'v', 'α': 'a', 'ε': 'e',
	'ι': 'i', 'κ': 'k', 'τ': 't', 'υ': 'u', 'χ': 'x',
	// Latin.
	'ı': 'i', 'ɡ': 'g',
}

// containsSpokenKeyword reports whether the folded form of text holds a spoken
// secret keyword. Folding turns "_" into a space, so the rules also cover every
// legacy keyword ("api_key" folds to "api key"); sanitize_keyword_test.go pins
// that.
func containsSpokenKeyword(text string) bool {
	folded := foldSpoken(text)
	for _, rule := range spokenKeywordRules {
		if containsPhrase(folded, rule) {
			return true
		}
	}
	return false
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
//   - turns every run of white space, dash punctuation, underscore, and dot
//     into one space, so "pass-word", "pass_word", and "pass.word" fold like
//     "pass word" and "P.I.N." folds to "p i n".
func foldSpoken(text string) string {
	var out strings.Builder
	out.Grow(len(text))
	pendingSpace := false
	for _, r := range norm.NFKD.String(text) {
		switch {
		case unicode.Is(unicode.Mn, r), unicode.Is(unicode.Cf, r):
			continue
		case unicode.IsSpace(r), r == '_', r == '.', unicode.Is(unicode.Pd, r):
			pendingSpace = out.Len() > 0
			continue
		}
		r = unicode.ToLower(r)
		if latin, ok := homoglyphs[r]; ok {
			r = latin
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
